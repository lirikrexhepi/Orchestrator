package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"composer/internal/domain"
)

// CallerScope captures the security identity and ambient context of an MCP client caller.
type CallerScope struct {
	ThreadID   string
	Driver     domain.DriverKind
	Cwd        string
	Permission domain.PermissionMode
	IsSubagent bool
	IssuedAt   time.Time
}

// IssuedCredential represents an issued loopback credential for an MCP client.
type IssuedCredential struct {
	SessionKey string
	Endpoint   string // e.g. "http://127.0.0.1:<port>/mcp"
	Token      string // raw base64url 32-byte secret
}

const defaultCredentialTTL = 24 * time.Hour

type registryEntry struct {
	sessionKey string
	tokenHash  string
	scope      CallerScope
	lastUsedAt time.Time
}

// Registry manages ephemeral credentials for loopback MCP clients.
type Registry struct {
	mu       sync.RWMutex
	records  map[string]registryEntry // keyed by tokenHash
	onRevoke func(tokenHashes []string)
	ttl      time.Duration
	now      func() time.Time
}

// NewRegistry creates a new credential registry with an optional onRevoke callback.
func NewRegistry(onRevoke func(tokenHashes []string)) *Registry {
	return &Registry{
		records:  make(map[string]registryEntry),
		onRevoke: onRevoke,
		ttl:      defaultCredentialTTL,
		now:      time.Now,
	}
}

func (r *Registry) currentTime() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// SetOnRevoke updates the revocation callback.
func (r *Registry) SetOnRevoke(fn func(tokenHashes []string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onRevoke = fn
}

// Issue generates a 32-byte cryptographic token, stores only its SHA-256 hash mapped to
// the session key and scope, and returns the IssuedCredential.
func (r *Registry) Issue(endpoint string, scope CallerScope) (IssuedCredential, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return IssuedCredential{}, fmt.Errorf("failed to generate random token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)

	h := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(h[:])

	sessBytes := make([]byte, 16)
	if _, err := rand.Read(sessBytes); err != nil {
		return IssuedCredential{}, fmt.Errorf("failed to generate session key: %w", err)
	}
	sessionKey := hex.EncodeToString(sessBytes)

	now := r.currentTime()
	if scope.IssuedAt.IsZero() {
		scope.IssuedAt = now
	}

	r.mu.Lock()
	pruned := r.pruneExpiredLocked(now)
	onRevoke := r.onRevoke
	r.records[tokenHash] = registryEntry{
		sessionKey: sessionKey,
		tokenHash:  tokenHash,
		scope:      scope,
		lastUsedAt: now,
	}
	r.mu.Unlock()

	if onRevoke != nil && len(pruned) > 0 {
		onRevoke(pruned)
	}

	return IssuedCredential{
		SessionKey: sessionKey,
		Endpoint:   endpoint,
		Token:      token,
	}, nil
}

func (r *Registry) pruneExpiredLocked(now time.Time) []string {
	if r.ttl <= 0 {
		return nil
	}
	var expired []string
	for hash, entry := range r.records {
		if now.Sub(entry.lastUsedAt) > r.ttl {
			expired = append(expired, hash)
			delete(r.records, hash)
		}
	}
	return expired
}

// Resolve looks up a raw token by its SHA-256 hash. Returns (scope, sessionKey, tokenHash, true) if found and unexpired.
func (r *Registry) Resolve(rawToken string) (CallerScope, string, string, bool) {
	if rawToken == "" {
		return CallerScope{}, "", "", false
	}
	h := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(h[:])
	now := r.currentTime()

	r.mu.Lock()
	entry, ok := r.records[tokenHash]
	if !ok {
		r.mu.Unlock()
		return CallerScope{}, "", "", false
	}
	if r.ttl > 0 && now.Sub(entry.lastUsedAt) > r.ttl {
		delete(r.records, tokenHash)
		onRevoke := r.onRevoke
		r.mu.Unlock()
		if onRevoke != nil {
			onRevoke([]string{tokenHash})
		}
		return CallerScope{}, "", "", false
	}
	entry.lastUsedAt = now
	r.records[tokenHash] = entry
	r.mu.Unlock()
	return entry.scope, entry.sessionKey, tokenHash, true
}

// RevokeThread removes all credentials issued for the specified threadID and invokes onRevoke.
func (r *Registry) RevokeThread(threadID string) {
	var revokedHashes []string
	var onRevoke func([]string)

	r.mu.Lock()
	for hash, entry := range r.records {
		if entry.scope.ThreadID == threadID {
			revokedHashes = append(revokedHashes, hash)
			delete(r.records, hash)
		}
	}
	onRevoke = r.onRevoke
	r.mu.Unlock()

	if onRevoke != nil && len(revokedHashes) > 0 {
		onRevoke(revokedHashes)
	}
}

// RevokeSession removes the credential matching sessionKey and invokes onRevoke.
func (r *Registry) RevokeSession(sessionKey string) {
	var revokedHashes []string
	var onRevoke func([]string)

	r.mu.Lock()
	for hash, entry := range r.records {
		if entry.sessionKey == sessionKey {
			revokedHashes = append(revokedHashes, hash)
			delete(r.records, hash)
		}
	}
	onRevoke = r.onRevoke
	r.mu.Unlock()

	if onRevoke != nil && len(revokedHashes) > 0 {
		onRevoke(revokedHashes)
	}
}

// RevokeAll clears all issued credentials and invokes onRevoke.
func (r *Registry) RevokeAll() {
	var revokedHashes []string
	var onRevoke func([]string)

	r.mu.Lock()
	for hash := range r.records {
		revokedHashes = append(revokedHashes, hash)
	}
	r.records = make(map[string]registryEntry)
	onRevoke = r.onRevoke
	r.mu.Unlock()

	if onRevoke != nil && len(revokedHashes) > 0 {
		onRevoke(revokedHashes)
	}
}

// Permission rank ordering:
// PermissionPlan (0) < PermissionDefault (1) < PermissionAcceptEdits (2) < PermissionBypass (3).
func PermissionRank(mode domain.PermissionMode) (int, error) {
	switch mode {
	case domain.PermissionPlan:
		return 0, nil
	case domain.PermissionDefault, "":
		return 1, nil
	case domain.PermissionAcceptEdits:
		return 2, nil
	case domain.PermissionBypass:
		return 3, nil
	default:
		return -1, fmt.Errorf("unknown permission mode %q", mode)
	}
}

// ResolveChildPermission calculates the effective child permission mode.
// Empty or "inherit" returns parent. rank(requested) > rank(parent) fails with escalation-denied error.
func ResolveChildPermission(parent, requested domain.PermissionMode) (domain.PermissionMode, error) {
	if parent == "" {
		parent = domain.PermissionDefault
	}
	if requested == "" || requested == "inherit" {
		return parent, nil
	}

	pRank, err := PermissionRank(parent)
	if err != nil {
		return "", fmt.Errorf("invalid parent permission mode %q: %w", parent, err)
	}

	rRank, err := PermissionRank(requested)
	if err != nil {
		return "", fmt.Errorf("invalid requested permission mode %q: %w", requested, err)
	}

	if rRank > pRank {
		return "", fmt.Errorf("permission escalation denied: parent has %q (rank %d), cannot grant %q (rank %d)", parent, pRank, requested, rRank)
	}

	return requested, nil
}

// CheckTargetPermission ensures the caller has sufficient permission to mutate target thread.
// Refuses mutating a thread whose permission rank is strictly greater than caller's rank.
func CheckTargetPermission(caller, target domain.PermissionMode) error {
	cRank, err := PermissionRank(caller)
	if err != nil {
		return fmt.Errorf("invalid caller permission mode %q: %w", caller, err)
	}
	tRank, err := PermissionRank(target)
	if err != nil {
		return fmt.Errorf("invalid target permission mode %q: %w", target, err)
	}

	if tRank > cRank {
		return fmt.Errorf("permission denied: caller permission %q (rank %d) is lower than target permission %q (rank %d)", caller, cRank, target, tRank)
	}
	return nil
}
