package mcp

import (
	"testing"
	"time"

	"composer/internal/domain"
)

func TestRegistryCredentialTTLExpiryAndRefresh(t *testing.T) {
	var revoked []string
	reg := NewRegistry(func(hashes []string) {
		revoked = append(revoked, hashes...)
	})
	reg.ttl = 50 * time.Millisecond
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	reg.now = func() time.Time { return clock }

	cred, err := reg.Issue("http://127.0.0.1:0/mcp", CallerScope{
		ThreadID:   "th-1",
		Driver:     domain.DriverClaude,
		Permission: domain.PermissionDefault,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Active lookups refresh lastUsedAt and stay valid.
	clock = clock.Add(30 * time.Millisecond)
	if _, _, _, ok := reg.Resolve(cred.Token); !ok {
		t.Fatal("expected credential to still be valid after refresh")
	}
	clock = clock.Add(30 * time.Millisecond)
	if _, _, _, ok := reg.Resolve(cred.Token); !ok {
		t.Fatal("expected refreshed credential to stay valid beyond initial TTL")
	}

	// Idle past TTL expires and triggers revocation.
	clock = clock.Add(70 * time.Millisecond)
	if _, _, _, ok := reg.Resolve(cred.Token); ok {
		t.Fatal("expected idle credential past TTL to be rejected")
	}
	if len(revoked) != 1 {
		t.Fatalf("expected 1 revoked hash, got %d", len(revoked))
	}

	// Issue prunes expired entries that were never resolved again.
	cred2, err := reg.Issue("http://127.0.0.1:0/mcp", CallerScope{ThreadID: "th-2"})
	if err != nil {
		t.Fatalf("Issue cred2: %v", err)
	}
	_ = cred2
	clock = clock.Add(70 * time.Millisecond)
	_, err = reg.Issue("http://127.0.0.1:0/mcp", CallerScope{ThreadID: "th-3"})
	if err != nil {
		t.Fatalf("Issue cred3: %v", err)
	}
	if len(revoked) != 2 {
		t.Fatalf("expected expired cred2 to be pruned on Issue, revoked=%d", len(revoked))
	}
}
