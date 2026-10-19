package provider

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"composer/internal/domain"
	"composer/internal/shell"
)

const probeTimeout = 10 * time.Second

var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?`)

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// RunCommand executes a short-lived CLI probe with the resolved environment.
func RunCommand(ctx context.Context, binary string, args []string, env map[string]string, cwd string) CommandResult {
	return RunCommandWith(ctx, binary, args, env, cwd, "", probeTimeout)
}

func RunCommandWith(ctx context.Context, binary string, args []string, env map[string]string, cwd, stdin string, timeout time.Duration) CommandResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if env == nil {
		env = shell.BaseEnvironment()
	}
	resolved := shell.SpawnCommand(binary, args, env)

	cmd := exec.CommandContext(ctx, resolved.Command, resolved.Args...)
	cmd.Dir = cwd
	cmd.Env = shell.Slice(env)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	hideWindow(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	return result
}

func LaunchEnv(settings domain.ProviderSettings) map[string]string {
	return shell.Merge(Without(shell.BaseEnvironment(), settings.Unset...), settings.Env)
}

func ParseVersion(output string) string {
	return versionPattern.FindString(output)
}

// ProbeVersion is the standard availability check: resolve the binary, run
// `--version`, and classify the outcome into a snapshot.
func ProbeVersion(ctx context.Context, binary string, settings domain.ProviderSettings, args ...string) domain.ProviderSnapshot {
	env := LaunchEnv(settings)

	commandPath, found := shell.LookPath(binary, env)
	if !found {
		return domain.ProviderSnapshot{
			Availability: domain.AvailabilityUnavailable,
			Message:      binary + " was not found on PATH. Install the CLI or set an explicit binary path.",
		}
	}

	if len(args) == 0 {
		args = []string{"--version"}
	}
	result := RunCommand(ctx, binary, args, env, "")
	combined := result.Stdout + "\n" + result.Stderr
	version := ParseVersion(combined)

	if result.Err != nil && version == "" {
		return domain.ProviderSnapshot{
			Availability: domain.AvailabilityUnavailable,
			CommandPath:  commandPath,
			Message:      strings.TrimSpace(firstLine(combined)),
		}
	}

	return domain.ProviderSnapshot{
		Availability: domain.AvailabilityReady,
		CommandPath:  commandPath,
		Version:      version,
	}
}

func firstLine(text string) string {
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		return text[:idx]
	}
	return text
}
