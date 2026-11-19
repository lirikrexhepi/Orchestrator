package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var (
	// ErrCDPClosed is returned when an operation is attempted on a closed CDP connection.
	ErrCDPClosed = errors.New("cdp connection closed")
)

type cdpRequest struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId,omitempty"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

type cdpResponse struct {
	ID        int64           `json:"id,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
}

type sessionDispatcher struct {
	events chan *cdpResponse
	done   chan struct{}
}

// CDPClient provides a minimal, thread-safe CDP JSON-RPC client over gorilla/websocket
// supporting flattened session routing.
type CDPClient struct {
	ws       *websocket.Conn
	writeMu  sync.Mutex
	nextID   int64
	done     chan struct{}
	closeErr atomic.Pointer[error]

	mu       sync.RWMutex
	pending  map[int64]chan *cdpResponse
	sessions map[string]*sessionDispatcher
	browserH func(method string, params json.RawMessage)
}

// DialCDP establishes a WebSocket connection to a Chrome DevTools browser endpoint.
func DialCDP(ctx context.Context, wsURL string) (*CDPClient, error) {
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
	}

	ws, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial cdp websocket at %s: %w", wsURL, err)
	}

	client := &CDPClient{
		ws:       ws,
		done:     make(chan struct{}),
		pending:  make(map[int64]chan *cdpResponse),
		sessions: make(map[string]*sessionDispatcher),
	}

	go client.readLoop()
	return client, nil
}

// Done returns a channel that is closed when the CDP connection terminates.
func (c *CDPClient) Done() <-chan struct{} {
	return c.done
}

// Close gracefully closes the WebSocket connection.
func (c *CDPClient) Close() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	select {
	case <-c.done:
		return nil
	default:
	}

	_ = c.ws.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second),
	)
	return c.ws.Close()
}

// Call executes a CDP JSON-RPC command, optionally routed to a specific sessionID.
func (c *CDPClient) Call(ctx context.Context, sessionID, method string, params any, result any) error {
	select {
	case <-c.done:
		return ErrCDPClosed
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	id := atomic.AddInt64(&c.nextID, 1)
	req := cdpRequest{
		ID:        id,
		SessionID: sessionID,
		Method:    method,
		Params:    params,
	}

	respCh := make(chan *cdpResponse, 1)

	c.mu.Lock()
	c.pending[id] = respCh
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	c.writeMu.Lock()
	err := c.ws.WriteJSON(req)
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("failed to write cdp request: %w", err)
	}

	select {
	case <-c.done:
		return ErrCDPClosed
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-respCh:
		if resp == nil {
			return ErrCDPClosed
		}
		if resp.Error != nil {
			return fmt.Errorf("cdp error (%d): %s", resp.Error.Code, resp.Error.Message)
		}
		if result != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("failed to unmarshal cdp result: %w", err)
			}
		}
		return nil
	}
}

// RegisterSession registers an in-order event handler for a flattened CDP session.
func (c *CDPClient) RegisterSession(sessionID string, handler func(method string, params json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Terminate previous dispatcher if any
	if existing, ok := c.sessions[sessionID]; ok {
		close(existing.done)
	}

	disp := &sessionDispatcher{
		events: make(chan *cdpResponse, 256),
		done:   make(chan struct{}),
	}
	c.sessions[sessionID] = disp

	go func() {
		for {
			select {
			case <-c.done:
				return
			case <-disp.done:
				return
			case ev, ok := <-disp.events:
				if !ok {
					return
				}
				handler(ev.Method, ev.Params)
			}
		}
	}()
}

// UnregisterSession removes the event handler for a session and stops its worker.
func (c *CDPClient) UnregisterSession(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if disp, ok := c.sessions[sessionID]; ok {
		close(disp.done)
		delete(c.sessions, sessionID)
	}
}

// SetBrowserHandler sets a handler for unflattened browser-level events (sessionId == "").
func (c *CDPClient) SetBrowserHandler(handler func(method string, params json.RawMessage)) {
	c.mu.Lock()
	c.browserH = handler
	c.mu.Unlock()
}

func (c *CDPClient) readLoop() {
	var readErr error
	defer func() {
		c.closeErr.Store(&readErr)
		close(c.done)

		c.mu.Lock()
		for _, ch := range c.pending {
			select {
			case ch <- nil:
			default:
			}
		}
		for _, disp := range c.sessions {
			close(disp.done)
		}
		c.pending = make(map[int64]chan *cdpResponse)
		c.sessions = make(map[string]*sessionDispatcher)
		c.mu.Unlock()

		_ = c.ws.Close()
	}()

	for {
		var resp cdpResponse
		if err := c.ws.ReadJSON(&resp); err != nil {
			readErr = err
			return
		}

		// Handle command response
		if resp.ID != 0 {
			c.mu.RLock()
			ch, ok := c.pending[resp.ID]
			c.mu.RUnlock()
			if ok {
				select {
				case ch <- &resp:
				default:
				}
			}
			continue
		}

		// Handle event
		if resp.Method != "" {
			if resp.SessionID != "" {
				c.mu.RLock()
				disp, ok := c.sessions[resp.SessionID]
				c.mu.RUnlock()
				if ok {
					select {
					case disp.events <- &resp:
					default:
						// If session queue overflows, drop non-critical events or log
					}
				}
			} else {
				c.mu.RLock()
				bHandler := c.browserH
				c.mu.RUnlock()
				if bHandler != nil {
					go bHandler(resp.Method, resp.Params)
				}
			}
		}
	}
}
