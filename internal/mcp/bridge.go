package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

const (
	BridgeFlag  = "--mcp-stdio-bridge"
	EnvEndpoint = "ORCHESTRATOR_MCP_ENDPOINT"
	EnvAuth     = "ORCHESTRATOR_MCP_AUTH"
)

// IsBridgeInvocation returns true if args contains the --mcp-stdio-bridge flag.
func IsBridgeInvocation(args []string) bool {
	for _, a := range args {
		if a == BridgeFlag {
			return true
		}
	}
	return false
}

// RunStdioBridge bridges stdio JSON-RPC to loopback Streamable HTTP MCP server.
func RunStdioBridge(ctx context.Context, endpoint, authHeader string, r io.Reader, w io.Writer, client *http.Client) error {
	if client == nil {
		client = &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        2,
				MaxIdleConnsPerHost: 2,
				DisableCompression:  true,
			},
		}
	}

	if !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		authHeader = "Bearer " + authHeader
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4*1024), 4*1024*1024)

	var writeMu sync.Mutex
	writeLine := func(b []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := w.Write(b); err != nil {
			return err
		}
		_, err := w.Write([]byte("\n"))
		return err
	}

	var metaMu sync.Mutex
	var sessionID string
	var protocolVersion string
	var handshakeDone bool
	var wg sync.WaitGroup

	defer func() {
		wg.Wait()
		metaMu.Lock()
		sid := sessionID
		metaMu.Unlock()
		if sid != "" {
			delReq, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, endpoint, nil)
			if err == nil {
				delReq.Header.Set("Authorization", authHeader)
				delReq.Header.Set("Mcp-Session-Id", sid)
				resp, err := client.Do(delReq)
				if err == nil {
					_ = resp.Body.Close()
				}
			}
		}
	}()

	sendHTTP := func(data []byte, rawID json.RawMessage, isNotification bool) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
		if err != nil {
			if !isNotification {
				_ = writeLine([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":%q}}`, string(rawID), err.Error())))
			}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", authHeader)
		metaMu.Lock()
		sid := sessionID
		pv := protocolVersion
		metaMu.Unlock()
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
		if pv != "" {
			req.Header.Set("Mcp-Protocol-Version", pv)
		}

		resp, err := client.Do(req)
		if err != nil {
			if !isNotification {
				_ = writeLine([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":%q}}`, string(rawID), err.Error())))
			}
			return
		}
		defer resp.Body.Close()

		newSID := resp.Header.Get("Mcp-Session-Id")
		newPV := resp.Header.Get("Mcp-Protocol-Version")
		if newSID != "" || newPV != "" {
			metaMu.Lock()
			if newSID != "" {
				sessionID = newSID
			}
			if newPV != "" {
				protocolVersion = newPV
			}
			metaMu.Unlock()
		}

		if resp.StatusCode == http.StatusAccepted {
			// 202 handled silently
			return
		}

		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "text/event-stream") {
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				line := sc.Text()
				if strings.HasPrefix(line, "data: ") {
					eventData := strings.TrimPrefix(line, "data: ")
					_ = writeLine([]byte(eventData))
				}
			}
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			if !isNotification {
				_ = writeLine([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":%q}}`, string(rawID), err.Error())))
			}
			return
		}

		var compact bytes.Buffer
		if err := json.Compact(&compact, body); err == nil {
			_ = writeLine(compact.Bytes())
		} else {
			_ = writeLine(bytes.TrimSpace(body))
		}
	}

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var raw struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id,omitempty"`
			Method  string          `json:"method"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			_ = writeLine([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`))
			continue
		}

		isNotification := len(raw.ID) == 0 || string(bytes.TrimSpace(raw.ID)) == "null"

		if !handshakeDone {
			// Handshake barrier: serialize initialize and notifications/initialized
			sendHTTP(line, raw.ID, isNotification)
			if raw.Method == "notifications/initialized" || (raw.Method != "initialize" && !isNotification) {
				handshakeDone = true
			}
			continue
		}

		// Post-handshake: dispatch concurrently
		lineCopy := make([]byte, len(line))
		copy(lineCopy, line)
		wg.Add(1)
		go func(data []byte, rawID json.RawMessage, isNotif bool) {
			defer wg.Done()
			sendHTTP(data, rawID, isNotif)
		}(lineCopy, raw.ID, isNotification)
	}

	return scanner.Err()
}

// RunBridgeMain reads environment variables and executes RunStdioBridge on stdin/stdout.
func RunBridgeMain() int {
	endpoint := os.Getenv(EnvEndpoint)
	auth := os.Getenv(EnvAuth)
	if endpoint == "" || auth == "" {
		fmt.Fprintf(os.Stderr, "missing %s or %s\n", EnvEndpoint, EnvAuth)
		return 1
	}
	err := RunStdioBridge(context.Background(), endpoint, auth, os.Stdin, os.Stdout, nil)
	if err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "bridge error: %v\n", err)
		return 1
	}
	return 0
}
