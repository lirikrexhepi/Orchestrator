package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"composer/internal/domain"
	"composer/internal/instructions"
	"composer/internal/opencode"
	"composer/internal/provider"
	"composer/internal/reveal"
	"composer/internal/shell"
)

func (a *App) InstructionSources(cwd string) []instructions.Source {
	home, _ := os.UserHomeDir()
	cwd = strings.TrimSpace(cwd)
	if cwd != "" {
		cwd = filepath.Clean(cwd)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := instructions.Input{
		Home:           home,
		Cwd:            cwd,
		ClaudeAccounts: a.claudeAccountsFor(cwd, home),
		OpenCodeMajor:  opencode.MajorVersion(ctx, domain.ProviderSettings{}),
		AppNote:        provider.RuntimeInstructions,
	}
	if a.memory != nil && cwd != "" {
		input.ProjectMemoryPath = a.memory.ProjectMemoryPath(cwd)
	}
	return instructions.Collect(input)
}

func (a *App) claudeAccountsFor(cwd, home string) []instructions.ClaudeAccount {
	defaultDir := strings.TrimSpace(shell.BaseEnvironment()["CLAUDE_CONFIG_DIR"])
	if defaultDir == "" {
		defaultDir = filepath.Join(home, ".claude")
	}
	inUse := domain.DefaultAccountID
	if a.manager != nil {
		if resolved, err := a.manager.ResolveAccount(domain.DriverClaude, cwd, ""); err == nil {
			inUse = resolved
		}
	}

	var out []instructions.ClaudeAccount
	for _, account := range a.registry.Accounts(domain.DriverClaude) {
		dir := account.ConfigDir
		if account.IsDefault() || dir == "" {
			dir = defaultDir
		}
		out = append(out, instructions.ClaudeAccount{
			ID:      account.ID,
			Name:    account.Name,
			Dir:     dir,
			Default: account.IsDefault(),
			InUse:   domain.NormalizeAccount(account.ID) == domain.NormalizeAccount(inUse),
		})
	}
	if len(out) == 0 {
		out = append(out, instructions.ClaudeAccount{ID: domain.DefaultAccountID, Name: "Default", Dir: defaultDir, Default: true, InUse: true})
	}
	return out
}

func (a *App) OpenPath(target string) error {
	return reveal.File(target)
}
