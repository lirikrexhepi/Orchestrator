package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"composer/internal/domain"
	"composer/internal/shell"
)

const accountsFile = "accounts.json"

const accountsDir = "accounts"

const defaultAccountName = "Default"

var (
	ErrAccountNotFound   = errors.New("account not found")
	ErrAccountName       = errors.New("account name is required")
	ErrAccountNameTaken  = errors.New("an account with that name already exists")
	ErrDefaultAccount    = errors.New("the default account cannot be removed")
	ErrAccountsUnsupport = errors.New("this CLI does not support separate accounts")
)

type AccountStatus struct {
	SignedIn bool   `json:"signedIn"`
	Known    bool   `json:"known"`
	Detail   string `json:"detail,omitempty"`
}

type AccountDriver interface {
	Binary(settings domain.ProviderSettings) string
	AccountEnv(dir string) map[string]string
	AccountUnset() []string
	DefaultConfigDir(env map[string]string) string
	SharedSettings() []string
	SignInArgs() []string
	StatusArgs() []string
	ParseStatus(result CommandResult) AccountStatus
}

type driverAccounts struct {
	DefaultName string           `json:"defaultName,omitempty"`
	Accounts    []domain.Account `json:"accounts,omitempty"`
}

type Accounts struct {
	root string
	path string

	mu    sync.Mutex
	saved map[domain.DriverKind]driverAccounts
}

func NewAccounts(root string) *Accounts {
	store := &Accounts{
		root:  root,
		path:  filepath.Join(root, accountsFile),
		saved: make(map[domain.DriverKind]driverAccounts),
	}
	payload, err := os.ReadFile(store.path)
	if err != nil {
		return store
	}
	var stored map[domain.DriverKind]driverAccounts
	if json.Unmarshal(payload, &stored) == nil && stored != nil {
		store.saved = stored
	}
	return store
}

func (s *Accounts) List(kind domain.DriverKind) []domain.Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.saved[kind]
	out := make([]domain.Account, 0, len(entry.Accounts)+1)
	defaultAcc := domain.Account{ID: domain.DefaultAccountID, Name: defaultName(entry)}
	if kind == domain.DriverAntigravity {
		defaultAcc.AuthMode = "keyring"
	}
	out = append(out, defaultAcc)
	for _, acc := range entry.Accounts {
		if kind == domain.DriverAntigravity && acc.AuthMode == "" {
			if meta, err := ReadAccountMeta(acc.ConfigDir); err == nil && meta.AuthMode != "" {
				acc.AuthMode = meta.AuthMode
			} else {
				acc.AuthMode = "keyring"
			}
		}
		out = append(out, acc)
	}
	return out
}

func (s *Accounts) Get(kind domain.DriverKind, id string) (domain.Account, bool) {
	id = domain.NormalizeAccount(id)
	for _, account := range s.List(kind) {
		if account.ID == id {
			return account, true
		}
	}
	return domain.Account{}, false
}

func (s *Accounts) Add(kind domain.DriverKind, name string) (domain.Account, error) {
	return s.AddWithMode(kind, name, "")
}

func (s *Accounts) AddWithMode(kind domain.DriverKind, name, authMode string) (domain.Account, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Account{}, ErrAccountName
	}
	if kind == domain.DriverAntigravity && authMode == "" {
		authMode = "keyring"
	}

	s.mu.Lock()
	entry := s.saved[kind]
	if nameTaken(entry, name, "") {
		s.mu.Unlock()
		return domain.Account{}, ErrAccountNameTaken
	}
	id := uniqueID(entry, slug(name))
	account := domain.Account{
		ID:        id,
		Name:      name,
		ConfigDir: filepath.Join(s.root, accountsDir, string(kind)+"-"+id),
		AuthMode:  authMode,
	}
	entry.Accounts = append(entry.Accounts, account)
	s.saved[kind] = entry
	s.mu.Unlock()

	if err := os.MkdirAll(account.ConfigDir, 0o700); err != nil {
		s.forget(kind, id)
		return domain.Account{}, err
	}
	if err := s.persist(); err != nil {
		s.forget(kind, id)
		return domain.Account{}, err
	}
	return account, nil
}

func (s *Accounts) SetAuthMode(kind domain.DriverKind, id, authMode string) error {
	id = domain.NormalizeAccount(id)
	s.mu.Lock()
	entry := s.saved[kind]
	found := false
	for i := range entry.Accounts {
		if entry.Accounts[i].ID == id {
			entry.Accounts[i].AuthMode = authMode
			found = true
			break
		}
	}
	if found {
		s.saved[kind] = entry
	}
	s.mu.Unlock()
	if !found {
		return ErrAccountNotFound
	}
	return s.persist()
}

func (s *Accounts) Rename(kind domain.DriverKind, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrAccountName
	}
	id = domain.NormalizeAccount(id)

	s.mu.Lock()
	entry := s.saved[kind]
	if nameTaken(entry, name, id) {
		s.mu.Unlock()
		return ErrAccountNameTaken
	}
	found := false
	if id == domain.DefaultAccountID {
		entry.DefaultName = name
		found = true
	} else {
		for i := range entry.Accounts {
			if entry.Accounts[i].ID == id {
				entry.Accounts[i].Name = name
				found = true
				break
			}
		}
	}
	if found {
		s.saved[kind] = entry
	}
	s.mu.Unlock()

	if !found {
		return ErrAccountNotFound
	}
	return s.persist()
}

func (s *Accounts) Remove(kind domain.DriverKind, id string) error {
	id = domain.NormalizeAccount(id)
	if id == domain.DefaultAccountID {
		return ErrDefaultAccount
	}
	if !s.forget(kind, id) {
		return ErrAccountNotFound
	}
	return s.persist()
}

func (s *Accounts) forget(kind domain.DriverKind, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.saved[kind]
	kept := make([]domain.Account, 0, len(entry.Accounts))
	found := false
	for _, account := range entry.Accounts {
		if account.ID == id {
			found = true
			continue
		}
		kept = append(kept, account)
	}
	entry.Accounts = kept
	s.saved[kind] = entry
	return found
}

func (s *Accounts) persist() error {
	s.mu.Lock()
	payload, err := json.MarshalIndent(s.saved, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, payload, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temp, s.path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func defaultName(entry driverAccounts) string {
	if strings.TrimSpace(entry.DefaultName) != "" {
		return entry.DefaultName
	}
	return defaultAccountName
}

func nameTaken(entry driverAccounts, name, except string) bool {
	if except != domain.DefaultAccountID && strings.EqualFold(defaultName(entry), name) {
		return true
	}
	for _, account := range entry.Accounts {
		if account.ID != except && strings.EqualFold(account.Name, name) {
			return true
		}
	}
	return false
}

func uniqueID(entry driverAccounts, base string) string {
	taken := map[string]bool{domain.DefaultAccountID: true}
	for _, account := range entry.Accounts {
		taken[account.ID] = true
	}
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := base + "-" + strconv.Itoa(n)
		if !taken[candidate] {
			return candidate
		}
	}
}

func slug(name string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		default:
			return '-'
		}
	}, name)
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-")
	if out == "" {
		return "account"
	}
	if len(out) > 32 {
		out = strings.Trim(out[:32], "-")
	}
	return out
}

type AccountCommand struct {
	Binary    string
	Args      []string
	Env       map[string]string
	Overrides map[string]string
	Title     string
}

func (r *Registry) accountDriver(kind domain.DriverKind) (AccountDriver, error) {
	driver, ok := r.Driver(kind)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", kind)
	}
	accounts, ok := driver.(AccountDriver)
	if !ok || r.AccountStore() == nil {
		return nil, ErrAccountsUnsupport
	}
	return accounts, nil
}

func (r *Registry) AccountStatus(ctx context.Context, kind domain.DriverKind, id string) (AccountStatus, error) {
	accounts, err := r.accountDriver(kind)
	if err != nil {
		return AccountStatus{}, err
	}
	settings, err := r.LaunchSettings(kind, id)
	if err != nil {
		return AccountStatus{}, err
	}
	env := LaunchEnv(settings)
	binary := accounts.Binary(settings)
	if _, found := shell.LookPath(binary, env); !found {
		return AccountStatus{Detail: binary + " was not found on PATH"}, nil
	}
	return accounts.ParseStatus(RunCommand(ctx, binary, accounts.StatusArgs(), env, "")), nil
}

func (r *Registry) SignInCommand(kind domain.DriverKind, id string) (AccountCommand, error) {
	accounts, err := r.accountDriver(kind)
	if err != nil {
		return AccountCommand{}, err
	}
	account, ok := r.Account(kind, id)
	if !ok {
		return AccountCommand{}, ErrAccountNotFound
	}
	settings, err := r.LaunchSettings(kind, id)
	if err != nil {
		return AccountCommand{}, err
	}
	driver, _ := r.Driver(kind)
	var overrides map[string]string
	if !account.IsDefault() {
		overrides = accounts.AccountEnv(account.ConfigDir)
	}
	return AccountCommand{
		Binary:    accounts.Binary(settings),
		Args:      accounts.SignInArgs(),
		Env:       LaunchEnv(settings),
		Overrides: overrides,
		Title:     "Sign in to " + driver.DisplayName() + " (" + account.Name + ")",
	}, nil
}

func (r *Registry) CopyDefaultSettings(kind domain.DriverKind, id string) ([]string, error) {
	accounts, err := r.accountDriver(kind)
	if err != nil {
		return nil, err
	}
	account, ok := r.Account(kind, id)
	if !ok {
		return nil, ErrAccountNotFound
	}
	if account.IsDefault() {
		return nil, ErrDefaultAccount
	}
	items := accounts.SharedSettings()
	if len(items) == 0 {
		return nil, nil
	}
	from := accounts.DefaultConfigDir(LaunchEnv(r.Settings(kind)))
	if from == "" {
		return nil, errors.New("could not find the default account's folder")
	}
	if filepath.Clean(from) == filepath.Clean(account.ConfigDir) {
		return nil, errors.New("the account already uses the default folder")
	}
	return CopySharedSettings(from, account.ConfigDir, items)
}

func AccountSettings(settings domain.ProviderSettings, account domain.Account, driver Driver) (domain.ProviderSettings, error) {
	out := settings
	if def, ok := settings.AccountDefaults[domain.NormalizeAccount(account.ID)]; ok {
		if def.Model != "" {
			out.Model = def.Model
		}
		if len(def.Options) > 0 {
			out.Options = def.Options
		}
	}
	if account.IsDefault() {
		return out, nil
	}
	accounts, ok := driver.(AccountDriver)
	if !ok {
		return domain.ProviderSettings{}, ErrAccountsUnsupport
	}
	if strings.TrimSpace(account.ConfigDir) == "" {
		return domain.ProviderSettings{}, fmt.Errorf("account %q has no folder", account.Name)
	}

	overrides := accounts.AccountEnv(account.ConfigDir)
	unset := append([]string{}, accounts.AccountUnset()...)
	for key := range overrides {
		unset = append(unset, key)
	}

	out.Env = Without(settings.Env, unset...)
	for key, value := range overrides {
		out.Env[key] = value
	}
	out.Unset = unset
	return out, nil
}

func Without(env map[string]string, keys ...string) map[string]string {
	out := make(map[string]string, len(env))
	for key, value := range env {
		drop := false
		for _, unwanted := range keys {
			if strings.EqualFold(key, unwanted) {
				drop = true
				break
			}
		}
		if !drop {
			out[key] = value
		}
	}
	return out
}

func CopySharedSettings(from, to string, items []string) ([]string, error) {
	copied := make([]string, 0, len(items))
	for _, item := range items {
		if !safeItem(item) {
			continue
		}
		source := filepath.Join(from, item)
		info, err := os.Stat(source)
		if err != nil {
			continue
		}
		target := filepath.Join(to, item)
		if info.IsDir() {
			err = copyTree(source, target)
		} else {
			err = copyFile(source, target, info.Mode())
		}
		if err != nil {
			return copied, err
		}
		copied = append(copied, item)
	}
	return copied, nil
}

var neverCopied = map[string]bool{
	".credentials.json": true,
	".claude.json":      true,
	"history.jsonl":     true,
	"projects":          true,
	"sessions":          true,
	"todos":             true,
	"statsig":           true,
	"shell-snapshots":   true,
	"backups":           true,
}

func safeItem(item string) bool {
	clean := filepath.Clean(item)
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	return !neverCopied[strings.ToLower(filepath.Base(clean))] && !neverCopied[strings.ToLower(strings.Split(filepath.ToSlash(clean), "/")[0])]
}

func copyTree(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if rel != "." && neverCopied[strings.ToLower(info.Name())] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dest := filepath.Join(target, rel)
		if info.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		return copyFile(path, dest, info.Mode())
	})
}

func copyFile(source, target string, mode os.FileMode) error {
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm()|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

type AccountMeta struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	AuthMode string            `json:"authMode,omitempty"`
	APIKey   string            `json:"apiKey,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
}

func ReadAccountMeta(dir string) (AccountMeta, error) {
	data, err := os.ReadFile(filepath.Join(dir, "account.json"))
	if err != nil {
		return AccountMeta{}, err
	}
	var meta AccountMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return AccountMeta{}, err
	}
	return meta, nil
}

func WriteAccountMeta(dir string, meta AccountMeta) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "account.json"), data, 0o600)
}

