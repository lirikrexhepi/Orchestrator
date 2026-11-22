package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("jsonrpc %d: %s", e.Code, e.Message) }

const (
	CodeMethodNotFound = -32601
	CodeInternalError  = -32603
)

// Handler serves an inbound request from the agent. Returning a nil result for
// a notification is expected; errors are marshalled into a JSON-RPC error.
type Handler func(ctx context.Context, method string, params json.RawMessage) (any, error)

type Conn struct {
	w  io.Writer
	wm sync.Mutex

	seq     atomic.Int64
	pending sync.Map

	handler Handler

	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error
}

func NewConn(r io.Reader, w io.Writer, handler Handler) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{w: w, handler: handler, ctx: ctx, cancel: cancel, closed: make(chan struct{})}
	go c.read(r)
	return c
}

var ErrClosed = errors.New("jsonrpc: connection closed")

func (c *Conn) Done() <-chan struct{} { return c.closed }

func (c *Conn) Close(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = ErrClosed
		}
		c.closeErr = err
		if c.cancel != nil {
			c.cancel()
		}
		close(c.closed)
		c.pending.Range(func(key, value any) bool {
			if ch, ok := value.(chan *Message); ok {
				select {
				case ch <- &Message{Error: &Error{Code: CodeInternalError, Message: err.Error()}}:
				default:
				}
			}
			return true
		})
	})
}

func (c *Conn) read(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 128*1024), 32*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		payload := make([]byte, len(line))
		copy(payload, line)

		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		c.dispatch(&msg)
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	c.Close(err)
}

func (c *Conn) dispatch(msg *Message) {
	isResponse := msg.Method == "" && len(msg.ID) > 0
	if isResponse {
		if ch, ok := c.pending.LoadAndDelete(string(msg.ID)); ok {
			select {
			case ch.(chan *Message) <- msg:
			default:
			}
		}
		return
	}
	if msg.Method == "" {
		return
	}
	// Notifications (streamed deltas, item updates, turn completion) are
	// handled on the read goroutine so they reach the handler in wire order.
	// Only requests, which may block waiting on the user, get a goroutine.
	if len(msg.ID) == 0 {
		c.serve(msg)
		return
	}
	go c.serve(msg)
}

func (c *Conn) serve(msg *Message) {
	if c.handler == nil {
		if len(msg.ID) > 0 {
			c.respondError(msg.ID, CodeMethodNotFound, "no handler registered")
		}
		return
	}

	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := c.handler(ctx, msg.Method, msg.Params)
	if len(msg.ID) == 0 {
		return
	}
	if err != nil {
		c.respondError(msg.ID, CodeInternalError, err.Error())
		return
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		c.respondError(msg.ID, CodeInternalError, err.Error())
		return
	}
	_ = c.write(&Message{JSONRPC: "2.0", ID: msg.ID, Result: encoded})
}

func (c *Conn) respondError(id json.RawMessage, code int, message string) {
	_ = c.write(&Message{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message}})
}

func (c *Conn) write(msg *Message) error {
	encoded, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	c.wm.Lock()
	defer c.wm.Unlock()
	_, err = c.w.Write(encoded)
	return err
}

func (c *Conn) Notify(method string, params any) error {
	encoded, err := marshalParams(params)
	if err != nil {
		return err
	}
	return c.write(&Message{JSONRPC: "2.0", Method: method, Params: encoded})
}

// Call issues a request and blocks until the peer responds, the context is
// cancelled, or the connection drops.
func (c *Conn) Call(ctx context.Context, method string, params any, result any) error {
	select {
	case <-c.closed:
		return c.closeErr
	default:
	}

	encoded, err := marshalParams(params)
	if err != nil {
		return err
	}

	id := c.seq.Add(1)
	rawID := json.RawMessage(fmt.Sprintf("%d", id))
	ch := make(chan *Message, 1)
	c.pending.Store(string(rawID), ch)
	defer c.pending.Delete(string(rawID))

	if err := c.write(&Message{JSONRPC: "2.0", ID: rawID, Method: method, Params: encoded}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return c.closeErr
	case msg := <-ch:
		if msg.Error != nil {
			return msg.Error
		}
		if result == nil || len(msg.Result) == 0 {
			return nil
		}
		return json.Unmarshal(msg.Result, result)
	}
}

func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	return json.Marshal(params)
}
