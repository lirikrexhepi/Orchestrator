package mcp

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"composer/internal/domain"
)

func TestIsBridgeInvocation(t *testing.T) {
	if !IsBridgeInvocation([]string{"orchestrator", "--mcp-stdio-bridge"}) {
		t.Errorf("expected true for flag present")
	}
	if IsBridgeInvocation([]string{"orchestrator", "--headless"}) {
		t.Errorf("expected false for flag absent")
	}
}

func TestStdioBridgeEndToEnd(t *testing.T) {
	backend := &mockBackend{
		providers: []ProviderInfo{
			{Driver: "antigravity", Models: []string{"gemini-2.5-pro"}},
		},
	}
	srv := NewServer(backend)
	endpoint, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close(context.Background())

	cred, err := srv.Registry().Issue(endpoint, CallerScope{
		ThreadID:   "th-bridge",
		Driver:     domain.DriverAntigravity,
		Cwd:        "/workspace/test",
		Permission: domain.PermissionDefault,
	})
	if err != nil {
		t.Fatalf("failed to issue credential: %v", err)
	}

	// clientWriter writes to bridgeReader; bridgeWriter writes to clientReader
	bridgeReader, clientWriter := io.Pipe()
	clientReader, bridgeWriter := io.Pipe()

	bridgeDone := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		bridgeDone <- RunStdioBridge(ctx, endpoint, cred.Token, bridgeReader, bridgeWriter, nil)
	}()

	clientScanner := bufio.NewScanner(clientReader)

	sendAndReceive := func(req string) string {
		_, err := io.WriteString(clientWriter, req+"\n")
		if err != nil {
			t.Fatalf("failed to write to bridge: %v", err)
		}
		if !clientScanner.Scan() {
			t.Fatalf("failed to read response from bridge: %v", clientScanner.Err())
		}
		return clientScanner.Text()
	}

	// 1. Send initialize
	initResp := sendAndReceive(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if !strings.Contains(initResp, `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("expected initialize response, got: %s", initResp)
	}

	// Verify server allocated session
	srv.mu.Lock()
	sessCount := len(srv.sessions)
	srv.mu.Unlock()
	if sessCount != 1 {
		t.Fatalf("expected 1 session on server, got %d", sessCount)
	}

	// 2. Send notifications/initialized (should be handled silently, no output)
	_, _ = io.WriteString(clientWriter, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")

	// 3. Send ping - should receive ping response immediately, NOT blocked by notification
	pingResp := sendAndReceive(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if !strings.Contains(pingResp, `"result":{}`) {
		t.Fatalf("expected ping result {}, got: %s", pingResp)
	}

	// 4. Send tools/list
	listResp := sendAndReceive(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	if !strings.Contains(listResp, `"tools":`) || !strings.Contains(listResp, "orchestrator_capabilities") {
		t.Fatalf("expected tools/list result, got: %s", listResp)
	}

	// 5. Send tools/call for orchestrator_capabilities
	callResp := sendAndReceive(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"orchestrator_capabilities","arguments":{}}}`)
	if !strings.Contains(callResp, "antigravity") || !strings.Contains(callResp, "th-bridge") {
		t.Fatalf("expected capabilities result, got: %s", callResp)
	}

	// 6. Send malformed JSON -> -32700 Parse error
	parseErrResp := sendAndReceive(`{not-valid-json`)
	if !strings.Contains(parseErrResp, "-32700") {
		t.Fatalf("expected parse error -32700, got: %s", parseErrResp)
	}

	// 7. Close client writer (EOF)
	_ = clientWriter.Close()

	// Wait for bridge to complete
	select {
	case err := <-bridgeDone:
		if err != nil && err != io.EOF {
			t.Fatalf("bridge returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("bridge did not terminate on EOF")
	}

	// 8. Verify DELETE /mcp cleaned up session
	srv.mu.Lock()
	sessCountAfter := len(srv.sessions)
	srv.mu.Unlock()
	if sessCountAfter != 0 {
		t.Errorf("expected session to be deleted after bridge shutdown, remaining: %d", sessCountAfter)
	}
}
