package main

import (
	"context"
	"errors"
	"fmt"

	"composer/internal/antigravity"
	"composer/internal/domain"
	"composer/internal/opencode"
	"composer/internal/session"
)

// UsageReport returns per-CLI token spend for this app run plus current
// subscription quota.
//
// Quota comes from a source that refreshes in the background, matching how each
// CLI's own settings view behaves: opening the panel shows current figures
// without an agent having to run first, and without waiting on a request.
func (a *App) UsageReport() session.UsageReport {
	a.refreshQuota()
	a.refreshAccountQuotas(false)
	return a.nameUsage(a.usage.Report())
}

// refreshQuota pulls limits from every CLI whose quota Composer can reach.
//
// Claude's is fetched from the account with the OAuth token the CLI stores,
// falling back to that CLI's own on-disk cache. OpenCode Go's comes from the
// official usage endpoint with the key the CLI stores in auth.json. Antigravity's
// comes from the Cloud Code quota summary endpoint using the active OAuth token.
func (a *App) refreshQuota() {
	limits, fetchedAt, err := a.quota.Snapshot()
	a.usage.SetLimits(domain.DriverClaude, limits, fetchedAt)
	if err != nil {
		a.usage.SetQuotaError(domain.DriverClaude, err.Error())
	}

	goLimits, goFetchedAt, goErr := a.opencodeQuota.Snapshot()
	a.usage.SetLimits(domain.DriverOpenCode, goLimits, goFetchedAt)
	if goErr != nil && !errors.Is(goErr, opencode.ErrNoGoCredentials) {
		a.usage.SetQuotaError(domain.DriverOpenCode, goErr.Error())
	}

	if a.antigravityQuota != nil {
		agLimits, agFetchedAt, agErr := a.antigravityQuota.Snapshot()
		a.usage.SetLimits(domain.DriverAntigravity, agLimits, agFetchedAt)
		if agErr != nil && !errors.Is(agErr, antigravity.ErrNoCredentials) {
			a.usage.SetQuotaError(domain.DriverAntigravity, agErr.Error())
		}
	}
}

// RefreshUsage forces quota to be re-fetched rather than served from the
// freshness window, for when the panel is opened.
//
// The figures it returns are the ones already held: the fetch it starts runs in
// the background and announces itself over quotaChangedChannel when it lands, so
// opening the panel paints immediately instead of waiting on the network.
func (a *App) RefreshUsage() session.UsageReport {
	a.quota.Invalidate()
	a.opencodeQuota.Invalidate()
	if a.antigravityQuota != nil {
		a.antigravityQuota.Invalidate()
	}
	a.refreshAccountQuotas(true)
	return a.UsageReport()
}

func (a *App) ListProviders(force bool) []domain.ProviderSnapshot {
	if force {
		antigravity.InvalidateAccountModels("")
	}
	return a.registry.Probe(a.ctx, force)
}

func (a *App) GetProviderSettings(driver string) domain.ProviderSettings {
	return a.registry.Settings(domain.DriverKind(driver))
}

func (a *App) UpdateProviderSettings(driver string, settings domain.ProviderSettings) error {
	if a.prefService != nil {
		if settings.Model != "" {
			_ = a.prefService.SetPreferredModel(a.ctx, driver, settings.Model)
		}
		_ = a.prefService.SetPermission(a.ctx, driver, settings.Enabled)
	}
	kind := domain.DriverKind(driver)
	previous := a.registry.Settings(kind)
	if err := a.registry.SetSettings(kind, settings); err != nil {
		return err
	}
	// Adapters capture launch settings when built; rebuild it so new sessions
	// pick up a changed binary, env or launch args without an app restart.
	if launchSettingsChanged(previous, settings) {
		a.manager.ResetAdapter(kind)
	}
	return nil
}

func launchSettingsChanged(a, b domain.ProviderSettings) bool {
	if a.BinaryPath != b.BinaryPath || a.LaunchArgs != b.LaunchArgs || a.ServerURL != b.ServerURL ||
		a.APIEndpoint != b.APIEndpoint || a.PrintTimeout != b.PrintTimeout || len(a.Env) != len(b.Env) {
		return true
	}
	for key, value := range a.Env {
		if b.Env[key] != value {
			return true
		}
	}
	return false
}

// RenameSession updates the user-defined custom name of an agent session.
func (a *App) RenameSession(threadID string, customName string) error {
	if a.sessionService == nil {
		return fmt.Errorf("database session service not initialized")
	}
	return a.sessionService.RenameSession(a.ctx, threadID, customName)
}

// GetSessionMeta returns the SQLite session record for a thread ID.
func (a *App) GetSessionMeta(threadID string) (*domain.SessionRecord, error) {
	if a.sessionService == nil {
		return nil, fmt.Errorf("database session service not initialized")
	}
	return a.sessionService.GetSession(a.ctx, threadID)
}

// ListSessionsMeta lists recent sessions stored in SQLite.
func (a *App) ListSessionsMeta(limit int) ([]domain.SessionRecord, error) {
	if a.sessionService == nil {
		return nil, fmt.Errorf("database session service not initialized")
	}
	return a.sessionService.ListSessions(a.ctx, limit)
}

// GetSessionTasks returns the current task list for a session.
func (a *App) GetSessionTasks(sessionID string) ([]domain.TaskRecord, error) {
	if a.taskService == nil {
		return nil, fmt.Errorf("database task service not initialized")
	}
	return a.taskService.ListTasks(a.ctx, sessionID)
}

// SetUserPreference persists a key-value setting in SQLite.
func (a *App) SetUserPreference(key string, value string) error {
	if a.prefService == nil {
		return fmt.Errorf("preference service not initialized")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.prefService.Set(ctx, key, value)
}

// GetUserPreference retrieves a key-value setting from SQLite.
func (a *App) GetUserPreference(key string) (string, error) {
	if a.prefService == nil {
		return "", fmt.Errorf("preference service not initialized")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.prefService.Get(ctx, key)
}

// GetAllUserPreferences retrieves all key-value settings from SQLite.
func (a *App) GetAllUserPreferences() (map[string]string, error) {
	if a.prefService == nil {
		return nil, fmt.Errorf("preference service not initialized")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.prefService.All(ctx)
}

// SetProviderPermission updates permission (enabled/disabled) for an agent provider.
func (a *App) SetProviderPermission(providerID string, enabled bool) error {
	if a.prefService == nil {
		return fmt.Errorf("preference service not initialized")
	}
	return a.prefService.SetPermission(a.ctx, providerID, enabled)
}

// IsProviderEnabled checks if a given agent provider is enabled.
func (a *App) IsProviderEnabled(providerID string) (bool, error) {
	if a.prefService == nil {
		return true, nil
	}
	return a.prefService.IsProviderEnabled(a.ctx, providerID)
}
