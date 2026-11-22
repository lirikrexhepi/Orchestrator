package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"composer/internal/antigravity"
	"composer/internal/claude"
	"composer/internal/domain"
	"composer/internal/logger"
	"composer/internal/projects"
	"composer/internal/provider"
	"composer/internal/session"
)

type AccountAdded struct {
	Account domain.Account `json:"account"`
	Copied  []string       `json:"copied,omitempty"`
	Warning string         `json:"warning,omitempty"`
}

type accountQuotas struct {
	mu        sync.Mutex
	sources   map[string]*claude.QuotaSource
	agSources map[string]*antigravity.QuotaSource
}

func (a *App) ListAccounts(driver string) []domain.Account {
	return a.registry.Accounts(domain.DriverKind(driver))
}

func (a *App) AddAccount(driver, name string, copySettings bool) (AccountAdded, error) {
	kind := domain.DriverKind(driver)
	store := a.registry.AccountStore()
	if store == nil || !a.registry.SupportsAccounts(kind) {
		return AccountAdded{}, provider.ErrAccountsUnsupport
	}
	account, err := store.Add(kind, name)
	if err != nil {
		return AccountAdded{}, err
	}
	if kind == domain.DriverAntigravity {
		account.AuthMode = "keyring"
		_ = store.SetAuthMode(kind, account.ID, "keyring")
		_ = provider.WriteAccountMeta(account.ConfigDir, provider.AccountMeta{
			ID:       account.ID,
			Name:     account.Name,
			AuthMode: "keyring",
		})
	}
	logger.Infof("Accounts", "Added %s account %q at %s", kind, account.Name, account.ConfigDir)
	result := AccountAdded{Account: account}
	if copySettings {
		copied, err := a.registry.CopyDefaultSettings(kind, account.ID)
		result.Copied = copied
		if err != nil {
			result.Warning = "Some settings could not be copied: " + err.Error()
		}
	}
	return result, nil
}

func (a *App) AddAntigravityAccount(name, authMode, apiKey string) (AccountAdded, error) {
	kind := domain.DriverAntigravity
	store := a.registry.AccountStore()
	if store == nil || !a.registry.SupportsAccounts(kind) {
		return AccountAdded{}, provider.ErrAccountsUnsupport
	}
	if authMode == "" {
		authMode = "keyring"
	}
	account, err := store.AddWithMode(kind, name, authMode)
	if err != nil {
		return AccountAdded{}, err
	}
	account.AuthMode = authMode
	_ = provider.WriteAccountMeta(account.ConfigDir, provider.AccountMeta{
		ID:       account.ID,
		Name:     account.Name,
		AuthMode: authMode,
		APIKey:   apiKey,
	})
	logger.Infof("Accounts", "Added Antigravity account %q (%s) at %s", account.Name, authMode, account.ConfigDir)
	return AccountAdded{Account: account}, nil
}

func (a *App) CaptureAntigravityProfile(id string) error {
	logger.Infof("Accounts", "Capturing active Antigravity profile for %q", id)
	return antigravity.DefaultKeyringStore.CaptureProfile(id)
}

func (a *App) CopyAccountSettings(driver, id string) ([]string, error) {
	return a.registry.CopyDefaultSettings(domain.DriverKind(driver), id)
}

func (a *App) RenameAccount(driver, id, name string) error {
	store := a.registry.AccountStore()
	if store == nil {
		return provider.ErrAccountsUnsupport
	}
	return store.Rename(domain.DriverKind(driver), id, name)
}

func (a *App) RemoveAccount(driver, id string) error {
	kind := domain.DriverKind(driver)
	store := a.registry.AccountStore()
	if store == nil {
		return provider.ErrAccountsUnsupport
	}
	if err := store.Remove(kind, id); err != nil {
		return err
	}
	if kind == domain.DriverAntigravity {
		_ = antigravity.DefaultKeyringStore.DeleteProfile(id)
	}
	if a.accountQuota != nil {
		a.accountQuota.mu.Lock()
		if src := a.accountQuota.sources[id]; src != nil {
			src.OnUpdate(nil)
		}
		delete(a.accountQuota.sources, id)
		if agSrc := a.accountQuota.agSources[id]; agSrc != nil {
			agSrc.OnUpdate(nil)
		}
		delete(a.accountQuota.agSources, id)
		a.accountQuota.mu.Unlock()
	}
	a.usage.Forget(kind, id)
	return nil
}

func (a *App) AccountStatus(driver, id string) (provider.AccountStatus, error) {
	kind := domain.DriverKind(driver)
	if kind == domain.DriverAntigravity {
		return a.antigravityAccountStatus(id)
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return a.registry.AccountStatus(ctx, kind, id)
}

func (a *App) antigravityAccountStatus(id string) (provider.AccountStatus, error) {
	account, ok := a.registry.Account(domain.DriverAntigravity, id)
	if !ok {
		return provider.AccountStatus{}, provider.ErrAccountNotFound
	}
	if account.AuthMode == "api_key" {
		meta, err := provider.ReadAccountMeta(account.ConfigDir)
		if err == nil && strings.TrimSpace(meta.APIKey) != "" {
			return provider.AccountStatus{SignedIn: true, Known: true, Detail: "API Key (configured)"}, nil
		}
		return provider.AccountStatus{SignedIn: false, Known: true, Detail: "No API key configured"}, nil
	}

	normID := domain.NormalizeAccount(id)
	if antigravity.DefaultKeyringStore.HasProfile(normID) {
		email, _ := antigravity.DefaultKeyringStore.GetProfileEmail(normID)
		if email != "" {
			return provider.AccountStatus{SignedIn: true, Known: true, Detail: email + " · Google Subscription"}, nil
		}
		return provider.AccountStatus{SignedIn: true, Known: true, Detail: "Active · Google Subscription"}, nil
	}

	// Check if user just completed sign-in in the active slot
	if captured, email, _ := antigravity.DefaultKeyringStore.CheckActiveSecret(normID); captured {
		if email != "" {
			return provider.AccountStatus{SignedIn: true, Known: true, Detail: email + " · Google Subscription"}, nil
		}
		return provider.AccountStatus{SignedIn: true, Known: true, Detail: "Active · Google Subscription"}, nil
	}

	return provider.AccountStatus{SignedIn: false, Known: true, Detail: "Not signed in"}, nil
}

func (a *App) RestoreAntigravityDefault() error {
	return antigravity.DefaultKeyringStore.RestoreDefault()
}

func (a *App) GetAccountModels(driver, id string) ([]domain.Model, error) {
	kind := domain.DriverKind(driver)
	account, ok := a.registry.Account(kind, id)
	if !ok {
		return nil, provider.ErrAccountNotFound
	}

	if kind == domain.DriverAntigravity {
		settings := a.registry.Settings(kind)
		binary := "agy"
		if settings.BinaryPath != "" {
			binary = settings.BinaryPath
		}
		var apiKey string
		if account.AuthMode == "api_key" {
			meta, err := provider.ReadAccountMeta(account.ConfigDir)
			if err == nil {
				apiKey = meta.APIKey
			}
		}
		ctx := a.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		return antigravity.FetchAccountModels(ctx, binary, account.ID, account.AuthMode, apiKey)
	}

	for _, snap := range a.registry.Snapshots() {
		if snap.Driver == kind {
			return snap.Models, nil
		}
	}
	return nil, nil
}

func (a *App) SignInAccount(driver, id string) error {
	kind := domain.DriverKind(driver)
	if kind == domain.DriverAntigravity {
		account, ok := a.registry.Account(kind, id)
		if ok && (account.AuthMode == "keyring" || account.AuthMode == "") {
			if err := antigravity.DefaultKeyringStore.PrepareSignIn(id); err != nil {
				logger.Warnf("Accounts", "PrepareSignIn failed for %s: %v", id, err)
			}
		}
	}
	command, err := a.registry.SignInCommand(kind, id)
	if err != nil {
		return err
	}
	logger.Infof("Accounts", "Opening a sign-in terminal for %s account %s", driver, id)
	return openTerminal(command)
}

func (a *App) SetProjectAccount(projectID, driver, account string) (*projects.Project, error) {
	kind := domain.DriverKind(driver)
	if account != "" {
		if _, ok := a.registry.Account(kind, account); !ok {
			return nil, provider.ErrAccountNotFound
		}
		if domain.NormalizeAccount(account) == domain.DefaultAccountID {
			account = ""
		}
	}
	project, err := a.projects.SetAccount(projectID, driver, account)
	if err != nil {
		return nil, err
	}
	a.emit(projectsChangedChannel)
	return &project, nil
}

func (a *App) accountLabel(driver domain.DriverKind, id string) string {
	label := domain.DriverLabel(driver)
	accounts := a.registry.Accounts(driver)
	if len(accounts) < 2 {
		return label
	}
	if account, ok := a.registry.Account(driver, id); ok {
		return label + " · " + account.Name
	}
	return label
}

func (a *App) refreshAccountQuotas(force bool) {
	if a.accountQuota == nil {
		return
	}
	for _, account := range a.registry.Accounts(domain.DriverClaude) {
		if account.IsDefault() || account.ConfigDir == "" {
			continue
		}
		source := a.accountQuotaSource(account)
		if source == nil {
			continue
		}
		if force {
			source.Invalidate()
		}
		limits, fetchedAt, err := source.Snapshot()
		if !a.accountStillActive(domain.DriverClaude, account.ID, source) {
			continue
		}
		a.usage.SetAccountLimits(domain.DriverClaude, account.ID, limits, fetchedAt)
		if err != nil {
			a.usage.SetAccountQuotaError(domain.DriverClaude, account.ID, err.Error())
		}
	}

	for _, account := range a.registry.Accounts(domain.DriverAntigravity) {
		if account.IsDefault() || account.AuthMode == "api_key" {
			continue
		}
		source := a.antigravityAccountQuotaSource(account)
		if source == nil {
			continue
		}
		if force {
			source.Invalidate()
		}
		limits, fetchedAt, err := source.Snapshot()
		if !a.antigravityAccountStillActive(account.ID, source) {
			continue
		}
		a.usage.SetAccountLimits(domain.DriverAntigravity, account.ID, limits, fetchedAt)
		if err != nil && !errors.Is(err, antigravity.ErrNoCredentials) {
			a.usage.SetAccountQuotaError(domain.DriverAntigravity, account.ID, err.Error())
		}
	}
}

func (a *App) antigravityAccountStillActive(id string, source *antigravity.QuotaSource) bool {
	if _, ok := a.registry.Account(domain.DriverAntigravity, id); !ok {
		return false
	}
	if a.accountQuota == nil {
		return false
	}
	a.accountQuota.mu.Lock()
	current := a.accountQuota.agSources[id]
	a.accountQuota.mu.Unlock()
	return current == source
}

func (a *App) antigravityAccountQuotaSource(account domain.Account) *antigravity.QuotaSource {
	a.accountQuota.mu.Lock()
	defer a.accountQuota.mu.Unlock()
	if a.accountQuota.agSources == nil {
		a.accountQuota.agSources = make(map[string]*antigravity.QuotaSource)
	}
	if _, ok := a.registry.Account(domain.DriverAntigravity, account.ID); !ok {
		return nil
	}
	if source, ok := a.accountQuota.agSources[account.ID]; ok {
		return source
	}
	source := antigravity.NewQuotaSource(account.ID)
	source.OnUpdate(func() {
		if !a.antigravityAccountStillActive(account.ID, source) {
			return
		}
		limits, fetchedAt, err := source.Snapshot()
		if !a.antigravityAccountStillActive(account.ID, source) {
			return
		}
		a.usage.SetAccountLimits(domain.DriverAntigravity, account.ID, limits, fetchedAt)
		if err != nil && !errors.Is(err, antigravity.ErrNoCredentials) {
			a.usage.SetAccountQuotaError(domain.DriverAntigravity, account.ID, err.Error())
		}
		a.emit(quotaChangedChannel)
	})
	a.accountQuota.agSources[account.ID] = source
	return source
}

func (a *App) accountStillActive(driver domain.DriverKind, id string, source *claude.QuotaSource) bool {
	if _, ok := a.registry.Account(driver, id); !ok {
		return false
	}
	if a.accountQuota == nil {
		return false
	}
	a.accountQuota.mu.Lock()
	current := a.accountQuota.sources[id]
	a.accountQuota.mu.Unlock()
	return current == source
}

func (a *App) accountQuotaSource(account domain.Account) *claude.QuotaSource {
	a.accountQuota.mu.Lock()
	defer a.accountQuota.mu.Unlock()
	if _, ok := a.registry.Account(domain.DriverClaude, account.ID); !ok {
		return nil
	}
	if source, ok := a.accountQuota.sources[account.ID]; ok {
		return source
	}
	source := claude.NewAccountQuotaSource(account.ConfigDir)
	source.OnUpdate(func() {
		if !a.accountStillActive(domain.DriverClaude, account.ID, source) {
			return
		}
		limits, fetchedAt, err := source.Snapshot()
		if !a.accountStillActive(domain.DriverClaude, account.ID, source) {
			return
		}
		a.usage.SetAccountLimits(domain.DriverClaude, account.ID, limits, fetchedAt)
		if err != nil {
			a.usage.SetAccountQuotaError(domain.DriverClaude, account.ID, err.Error())
		}
		a.emit(quotaChangedChannel)
	})
	a.accountQuota.sources[account.ID] = source
	return source
}

func (a *App) nameUsage(report session.UsageReport) session.UsageReport {
	for i := range report.Drivers {
		entry := &report.Drivers[i]
		accounts := a.registry.Accounts(entry.Driver)
		if len(accounts) < 2 {
			continue
		}
		if account, ok := a.registry.Account(entry.Driver, entry.Account); ok {
			entry.AccountName = account.Name
		}
	}
	return report
}
