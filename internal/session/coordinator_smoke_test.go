package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"composer/internal/domain"
	"composer/internal/drivers"
	"composer/internal/provider"
	"composer/internal/shell"
)

func TestProbeExposesModelOptions(t *testing.T) {
	registry := provider.NewRegistry(drivers.All()...)
	for _, snapshot := range registry.Probe(context.Background(), true) {
		if snapshot.Availability != domain.AvailabilityReady {
			t.Logf("%-12s %s", snapshot.Driver, snapshot.Availability)
			continue
		}
		t.Logf("%-12s v%s  %d models", snapshot.Driver, snapshot.Version, len(snapshot.Models))
		for _, model := range snapshot.Models {
			labels := make([]string, 0, len(model.Options))
			for _, option := range model.Options {
				labels = append(labels, string(option.Type)+":"+option.ID)
			}
			t.Logf("    %-28s %s", model.ID, strings.Join(labels, " "))
		}
	}
}

// TestCoordinatorSwitchesModel drives the coordinator the way the UI will:
// send, then send again under a different model so the session restarts.
func TestCoordinatorSwitchesModel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live CLI smoke test in short mode")
	}
	env := shell.BaseEnvironment()
	if _, ok := shell.LookPath("claude", env); !ok {
		t.Skip("claude CLI not installed")
	}

	manager := NewManager(provider.NewRegistry(drivers.All()...))
	coordinator := NewCoordinator(manager)
	events, unsubscribe := manager.Bus().Subscribe()
	defer unsubscribe()
	defer manager.StopAll(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg := Config{
		Driver:     string(domain.DriverClaude),
		Model:      "claude-sonnet-5",
		Cwd:        t.TempDir(),
		Options:    domain.ModelOptions{domain.OptionEffort: "low"},
		Permission: domain.PermissionPlan,
	}

	turnID, err := coordinator.Send(ctx, cfg, "Reply with exactly: FIRST_OK")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	awaitTurn(t, events, turnID, "FIRST_OK")

	cfg.Options = domain.ModelOptions{domain.OptionEffort: "medium"}
	turnID, err = coordinator.Send(ctx, cfg, "Reply with exactly: SECOND_OK")
	if err != nil {
		t.Fatalf("send after option change: %v", err)
	}
	awaitTurn(t, events, turnID, "SECOND_OK")

	if got := len(coordinator.History()); got == 0 {
		t.Error("expected coordinator transcript")
	}
}

func awaitTurn(t *testing.T, events <-chan domain.RuntimeEvent, turnID, want string) {
	t.Helper()

	var text strings.Builder
	deadline := time.After(2 * time.Minute)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", turnID)
		case event := <-events:
			if event.TurnID != turnID {
				continue
			}
			switch event.Kind {
			case domain.EventAgentMessage:
				text.WriteString(event.Text)
			case domain.EventTurnFailed:
				t.Fatalf("turn failed: %s", event.Error)
			case domain.EventTurnCompleted:
				if !strings.Contains(text.String(), want) {
					t.Errorf("expected %q, got %q", want, text.String())
				}
				return
			}
		}
	}
}

// TestCoordinatorThinkingOption reproduces the --settings regression: a boolean
// model option must survive Windows shell escaping and reach the CLI intact.
func TestCoordinatorThinkingOption(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live CLI smoke test in short mode")
	}
	env := shell.BaseEnvironment()
	if _, ok := shell.LookPath("claude", env); !ok {
		t.Skip("claude CLI not installed")
	}

	manager := NewManager(provider.NewRegistry(drivers.All()...))
	coordinator := NewCoordinator(manager)
	events, unsubscribe := manager.Bus().Subscribe()
	defer unsubscribe()
	defer manager.StopAll(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	turnID, err := coordinator.Send(ctx, Config{
		Driver:     string(domain.DriverClaude),
		Model:      "claude-haiku-4-5",
		Cwd:        t.TempDir(),
		Options:    domain.ModelOptions{domain.OptionThinking: false},
		Permission: domain.PermissionPlan,
	}, "Reply with exactly: HAIKU_OK")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	awaitTurn(t, events, turnID, "HAIKU_OK")
}

// TestCoordinatorAutoPermissions confirms the bypass mode both CLIs are given by
// the UI is accepted, since an unknown value makes the CLI exit immediately.
func TestCoordinatorAutoPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live CLI smoke test in short mode")
	}
	env := shell.BaseEnvironment()

	for _, tc := range []struct {
		driver  domain.DriverKind
		binary  string
		model   string
		options domain.ModelOptions
	}{
		{domain.DriverClaude, "claude", "claude-haiku-4-5", nil},
		// The picker always sends the catalog's default effort, which agy
		// requires for tiered families.
		{domain.DriverAntigravity, "agy", "gemini-3.7-flash", domain.ModelOptions{domain.OptionEffort: "low"}},
	} {
		t.Run(string(tc.driver), func(t *testing.T) {
			if _, ok := shell.LookPath(tc.binary, env); !ok {
				t.Skipf("%s not installed", tc.binary)
			}

			manager := NewManager(provider.NewRegistry(drivers.All()...))
			coordinator := NewCoordinator(manager)
			events, unsubscribe := manager.Bus().Subscribe()
			defer unsubscribe()
			defer manager.StopAll(context.Background())

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			turnID, err := coordinator.Send(ctx, Config{
				Driver:     string(tc.driver),
				Model:      tc.model,
				Cwd:        t.TempDir(),
				Options:    tc.options,
				Permission: domain.PermissionBypass,
			}, "Reply with exactly: AUTO_OK")
			if err != nil {
				t.Fatalf("send: %v", err)
			}
			awaitTurn(t, events, turnID, "AUTO_OK")
		})
	}
}

// TestAntigravityToolExecution covers the failure seen in the UI: agy gates tool
// calls behind request-review by default, which blocks forever in print mode.
func TestAntigravityToolExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live CLI smoke test in short mode")
	}
	env := shell.BaseEnvironment()
	if _, ok := shell.LookPath("agy", env); !ok {
		t.Skip("agy not installed")
	}

	manager := NewManager(provider.NewRegistry(drivers.All()...))
	coordinator := NewCoordinator(manager)
	events, unsubscribe := manager.Bus().Subscribe()
	defer unsubscribe()
	defer manager.StopAll(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	turnID, err := coordinator.Send(ctx, Config{
		Driver:     string(domain.DriverAntigravity),
		Model:      "gemini-3.7-flash",
		Options:    domain.ModelOptions{domain.OptionEffort: "medium"},
		Permission: domain.PermissionBypass,
		Cwd:        `C:\Users\PC\Projects`,
	}, `Use a tool to list the subfolders of C:\Users\PC\Projects, then state how many there are.`)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	var text strings.Builder
	toolFailures := 0
	deadline := time.After(3 * time.Minute)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out")
		case event := <-events:
			if event.TurnID != turnID && event.Kind != domain.EventToolResult {
				continue
			}
			switch event.Kind {
			case domain.EventAgentMessage:
				text.WriteString(event.Text)
			case domain.EventToolResult:
				if event.Tool != nil && event.Tool.Status == domain.ToolFailed {
					toolFailures++
					t.Logf("tool failed: %s: %s", event.Tool.Name, event.Tool.Output)
				}
			case domain.EventTurnFailed:
				t.Fatalf("turn failed: %s", event.Error)
			case domain.EventTurnCompleted:
				t.Logf("answer: %s", text.String())
				if toolFailures > 0 {
					t.Errorf("%d tool call(s) were denied", toolFailures)
				}
				if text.Len() == 0 {
					t.Error("no answer produced")
				}
				return
			}
		}
	}
}
