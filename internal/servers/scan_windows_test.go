//go:build windows

package servers

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNativeListenersMatchPowerShell(t *testing.T) {
	native, err := nativeListeners()
	if err != nil {
		t.Fatalf("nativeListeners: %v", err)
	}
	shell, err := shellListeners(context.Background())
	if err != nil {
		t.Skipf("PowerShell unavailable: %v", err)
	}

	key := func(l listener) [2]int { return [2]int{l.pid, l.port} }
	have := map[[2]int]bool{}
	for _, l := range native {
		have[key(l)] = true
	}
	missing := 0
	for _, l := range shell {
		if !have[key(l)] {
			missing++
			t.Logf("PowerShell saw pid %d port %d (%s) that the native scan did not", l.pid, l.port, l.address)
		}
	}
	if missing > 2 {
		t.Fatalf("%d of %d listeners missing from the native scan", missing, len(shell))
	}
}

func TestNativeProcessTableMatchesPowerShell(t *testing.T) {
	native, err := nativeProcessTable()
	if err != nil {
		t.Fatalf("nativeProcessTable: %v", err)
	}
	self, ok := native[os.Getpid()]
	if !ok || !strings.HasSuffix(strings.ToLower(self.name), ".exe") || self.command == "" {
		t.Fatalf("own process = %+v", self)
	}

	shell, err := shellProcessTable(context.Background())
	if err != nil {
		t.Skipf("PowerShell unavailable: %v", err)
	}
	compared, mismatched := 0, 0
	for pid, want := range shell {
		got, ok := native[pid]
		if !ok {
			continue
		}
		compared++
		if got.parent != want.parent || !strings.EqualFold(got.name, want.name) ||
			(want.command != "" && got.command != want.command) {
			mismatched++
			t.Logf("pid %d: native %+v, PowerShell %+v", pid, got, want)
		}
	}
	if compared < len(shell)/2 || mismatched > compared/50 {
		t.Fatalf("compared %d of %d processes, %d mismatched", compared, len(shell), mismatched)
	}
}
