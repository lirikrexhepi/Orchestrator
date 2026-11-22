package drivers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"composer/internal/domain"
	"composer/internal/provider"
	"composer/internal/shell"
)

func claudeRegistry(t *testing.T) *provider.Registry {
	t.Helper()
	registry := provider.NewRegistry(All()...)
	registry.UseAccounts(provider.NewAccounts(t.TempDir()))
	return registry
}

func TestClaudeDefaultAccountSetsNoConfigDir(t *testing.T) {
	registry := claudeRegistry(t)
	settings, err := registry.LaunchSettings(domain.DriverClaude, domain.DefaultAccountID)
	if err != nil {
		t.Fatal(err)
	}
	if _, set := settings.Env["CLAUDE_CONFIG_DIR"]; set {
		t.Fatal("the default Claude account must keep using ~/.claude")
	}
	if len(settings.Unset) != 0 {
		t.Fatalf("the default account must not scrub anything, got %v", settings.Unset)
	}
}

func TestClaudeAccountEnvIsIsolated(t *testing.T) {
	registry := claudeRegistry(t)
	if err := registry.SetSettings(domain.DriverClaude, domain.ProviderSettings{
		Enabled: true,
		Env:     map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "work-token", "ANTHROPIC_BASE_URL": "https://gateway"},
	}); err != nil {
		t.Fatal(err)
	}
	account, err := registry.AccountStore().Add(domain.DriverClaude, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	shell.SetAmbient(map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": "ambient",
		"ANTHROPIC_API_KEY":       "ambient",
		"ANTHROPIC_AUTH_TOKEN":    "ambient",
		"CLAUDE_CONFIG_DIR":       `C:\somewhere\else`,
	})
	t.Cleanup(func() { shell.SetAmbient(nil) })

	settings, err := registry.LaunchSettings(domain.DriverClaude, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	env := provider.LaunchEnv(settings)
	if env["CLAUDE_CONFIG_DIR"] != account.ConfigDir {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q, want %q", env["CLAUDE_CONFIG_DIR"], account.ConfigDir)
	}
	for key := range env {
		switch strings.ToUpper(key) {
		case "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN":
			t.Fatalf("%s leaked into another account's environment", key)
		}
	}
	if env["ANTHROPIC_BASE_URL"] != "https://gateway" {
		t.Fatal("non-credential provider env should still apply")
	}

	defaults := provider.LaunchEnv(registry.Settings(domain.DriverClaude))
	if defaults["CLAUDE_CODE_OAUTH_TOKEN"] != "work-token" {
		t.Fatal("the default account's environment must stay exactly as configured")
	}
}

func TestOpenCodeAccountUsesItsOwnDataDir(t *testing.T) {
	registry := provider.NewRegistry(All()...)
	registry.UseAccounts(provider.NewAccounts(t.TempDir()))
	account, err := registry.AccountStore().Add(domain.DriverOpenCode, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	settings, err := registry.LaunchSettings(domain.DriverOpenCode, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Env["XDG_DATA_HOME"] != account.ConfigDir {
		t.Fatalf("XDG_DATA_HOME = %q", settings.Env["XDG_DATA_HOME"])
	}
}

func TestCodexStaysSingleAccount(t *testing.T) {
	registry := claudeRegistry(t)
	if registry.SupportsAccounts(domain.DriverCodex) {
		t.Fatalf("Codex should not offer accounts")
	}
	if _, err := registry.AccountStore().Add(domain.DriverCodex, "Personal"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.LaunchSettings(domain.DriverCodex, "personal"); err == nil {
		t.Fatalf("Codex must refuse a non-default account")
	}
}

func TestAntigravitySupportsAccounts(t *testing.T) {
	registry := claudeRegistry(t)
	if !registry.SupportsAccounts(domain.DriverAntigravity) {
		t.Fatalf("Antigravity should offer accounts")
	}
	accounts := registry.Accounts(domain.DriverAntigravity)
	if len(accounts) != 1 || accounts[0].ID != domain.DefaultAccountID {
		t.Fatalf("initial accounts = %+v, want [default]", accounts)
	}
	if accounts[0].AuthMode != "keyring" {
		t.Fatalf("default authMode = %q, want keyring", accounts[0].AuthMode)
	}

	personal, err := registry.AccountStore().Add(domain.DriverAntigravity, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	settings, err := registry.LaunchSettings(domain.DriverAntigravity, personal.ID)
	if err != nil {
		t.Fatalf("LaunchSettings failed: %v", err)
	}
	_ = settings

	// Test API Key account
	apiKeyAcc, err := registry.AccountStore().AddWithMode(domain.DriverAntigravity, "API Key Account", "api_key")
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.WriteAccountMeta(apiKeyAcc.ConfigDir, provider.AccountMeta{
		ID:       apiKeyAcc.ID,
		Name:     apiKeyAcc.Name,
		AuthMode: "api_key",
		APIKey:   "secret-test-key",
	}); err != nil {
		t.Fatal(err)
	}
	apiSettings, err := registry.LaunchSettings(domain.DriverAntigravity, apiKeyAcc.ID)
	if err != nil {
		t.Fatalf("LaunchSettings for api key failed: %v", err)
	}
	if apiSettings.Env["GEMINI_API_KEY"] != "secret-test-key" {
		t.Fatalf("expected GEMINI_API_KEY in env, got %+v", apiSettings.Env)
	}

	// Verify ambient key is scrubbed
	shell.SetAmbient(map[string]string{
		"GEMINI_API_KEY": "ambient-key",
	})
	t.Cleanup(func() { shell.SetAmbient(nil) })

	cleanSettings, err := registry.LaunchSettings(domain.DriverAntigravity, personal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleanSettings.Env["GEMINI_API_KEY"] != "" {
		t.Fatalf("ambient GEMINI_API_KEY should be scrubbed for keyring account, got %q", cleanSettings.Env["GEMINI_API_KEY"])
	}
}

func TestClaudeStatusParsing(t *testing.T) {
	driver := &claudeDriver{}
	signedOut := driver.ParseStatus(provider.CommandResult{
		Stdout:   `{"loggedIn": false, "authMethod": "none", "apiProvider": "firstParty"}`,
		ExitCode: 1,
	})
	if signedOut.SignedIn || !signedOut.Known {
		t.Fatalf("signed-out status = %+v", signedOut)
	}
	signedIn := driver.ParseStatus(provider.CommandResult{
		Stdout: `{"loggedIn": true, "authMethod": "claude.ai", "email": "me@example.com", "orgName": "Acme", "subscriptionType": "max"}`,
	})
	if !signedIn.SignedIn || signedIn.Detail != "me@example.com · Acme · max" {
		t.Fatalf("signed-in status = %+v", signedIn)
	}
	unreadable := driver.ParseStatus(provider.CommandResult{Stderr: "boom"})
	if unreadable.Known || unreadable.Detail != "boom" {
		t.Fatalf("unreadable status = %+v", unreadable)
	}
}

func TestOpenCodeStatusParsing(t *testing.T) {
	driver := &openCodeDriver{}
	none := driver.ParseStatus(provider.CommandResult{Stdout: "\x1b[90mCredentials ~/x/auth.json\x1b[0m\n|\n—  0 credentials\n"})
	if none.SignedIn || !none.Known {
		t.Fatalf("empty store = %+v", none)
	}
	some := driver.ParseStatus(provider.CommandResult{Stdout: "●  Anthropic oauth\n|\n—  1 credentials\n"})
	if !some.SignedIn || some.Detail != "1 provider" {
		t.Fatalf("one credential = %+v", some)
	}
}

func TestFreshClaudeAccountIsSignedOut(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the installed claude CLI")
	}
	if _, ok := shell.LookPath("claude", shell.BaseEnvironment()); !ok {
		t.Skip("claude CLI not installed")
	}
	registry := claudeRegistry(t)
	account, err := registry.AccountStore().Add(domain.DriverClaude, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	shell.SetAmbient(map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": "not-a-real-token",
		"ANTHROPIC_API_KEY":       "not-a-real-key",
	})
	t.Cleanup(func() { shell.SetAmbient(nil) })

	status, err := registry.AccountStatus(context.Background(), domain.DriverClaude, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.SignedIn || !status.Known {
		t.Fatalf("a brand new account must report signed out, got %+v", status)
	}
	if _, err := os.Stat(filepath.Join(account.ConfigDir, ".claude.json")); err != nil {
		t.Fatalf("the CLI did not use the account's folder: %v", err)
	}
}
