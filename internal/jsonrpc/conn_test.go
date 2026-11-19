package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

func TestInboundRequestBlockingDoesNotDeadlockOutboundCall(t *testing.T) {
	clientR, peerW := io.Pipe()
	peerR, clientW := io.Pipe()
	defer peerW.Close()
	defer clientW.Close()

	reqStarted := make(chan struct{})
	reqUnblock := make(chan struct{})

	handler := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method == "session/request_permission" {
			close(reqStarted)
			<-reqUnblock
			return map[string]string{"decision": "allow"}, nil
		}
		return nil, nil
	}

	clientConn := NewConn(clientR, clientW, handler)
	defer clientConn.Close(nil)

	peerScanner := bufio.NewScanner(peerR)
	peerLines := make(chan string, 10)
	go func() {
		for peerScanner.Scan() {
			peerLines <- peerScanner.Text()
		}
	}()

	// 1. Client initiates an outbound Call
	type outboundResult struct {
		Greeting string `json:"greeting"`
	}
	var callRes outboundResult
	callDone := make(chan error, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		callDone <- clientConn.Call(ctx, "session/prompt", map[string]string{"text": "hello"}, &callRes)
	}()

	// 2. Peer receives the outbound Call message (with id: 1)
	var callLine string
	select {
	case callLine = <-peerLines:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for outbound Call message from client")
	}

	var callMsg Message
	if err := json.Unmarshal([]byte(callLine), &callMsg); err != nil {
		t.Fatalf("failed to unmarshal outbound call message: %v", err)
	}
	if callMsg.Method != "session/prompt" {
		t.Fatalf("expected method session/prompt, got %q", callMsg.Method)
	}

	// 3. Peer sends an inbound request to client before responding to the Call
	inboundReq := `{"jsonrpc":"2.0","id":99,"method":"session/request_permission","params":{}}` + "\n"
	if _, err := peerW.Write([]byte(inboundReq)); err != nil {
		t.Fatalf("failed to write inbound request: %v", err)
	}

	// 4. Wait for handler to start and block
	select {
	case <-reqStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to start processing inbound request")
	}

	// Handler is now blocked on reqUnblock
	// 5. While the inbound request handler is still blocked, Peer sends the response to Call (id: 1)
	callResponse := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"greeting":"world"}}`+"\n", string(callMsg.ID))
	if _, err := peerW.Write([]byte(callResponse)); err != nil {
		t.Fatalf("failed to write Call response: %v", err)
	}

	// 6. The outbound Call MUST resolve cleanly without waiting for reqUnblock
	select {
	case err := <-callDone:
		if err != nil {
			t.Fatalf("Call failed: %v", err)
		}
		if callRes.Greeting != "world" {
			t.Fatalf("expected greeting 'world', got %q", callRes.Greeting)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("outbound Call deadlocked while inbound request was blocking reader loop")
	}

	// 7. Unblock the inbound request handler
	close(reqUnblock)

	// 8. Peer receives the response for the inbound request (id: 99)
	var respLine string
	select {
	case respLine = <-peerLines:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for inbound request response from client")
	}

	var respMsg Message
	if err := json.Unmarshal([]byte(respLine), &respMsg); err != nil {
		t.Fatalf("failed to unmarshal inbound request response: %v", err)
	}
	if string(respMsg.ID) != "99" {
		t.Fatalf("expected response id 99, got %s", string(respMsg.ID))
	}

	var decision struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(respMsg.Result, &decision); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if decision.Decision != "allow" {
		t.Fatalf("expected decision 'allow', got %q", decision.Decision)
	}
}

func TestSequentialNotificationsDeliveredInFIFOOrder(t *testing.T) {
	clientR, peerW := io.Pipe()
	peerR, clientW := io.Pipe()
	defer peerW.Close()
	defer clientW.Close()

	var (
		mu       sync.Mutex
		received []int
	)

	handler := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method == "session/update" {
			var p struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
			mu.Lock()
			received = append(received, p.Index)
			mu.Unlock()
		}
		if method == "ping" {
			return "pong", nil
		}
		return nil, nil
	}

	clientConn := NewConn(clientR, clientW, handler)
	defer clientConn.Close(nil)

	peerScanner := bufio.NewScanner(peerR)
	peerLines := make(chan string, 10)
	go func() {
		for peerScanner.Scan() {
			peerLines <- peerScanner.Text()
		}
	}()

	const count = 100
	for i := 0; i < count; i++ {
		line := fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/update","params":{"index":%d}}`+"\n", i)
		if _, err := peerW.Write([]byte(line)); err != nil {
			t.Fatalf("failed to write notification %d: %v", i, err)
		}
	}

	// Send a request to ensure all preceding notifications on the wire are processed
	pingReq := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"
	if _, err := peerW.Write([]byte(pingReq)); err != nil {
		t.Fatalf("failed to write ping request: %v", err)
	}

	// Wait for ping response
	select {
	case <-peerLines:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ping response")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != count {
		t.Fatalf("expected %d notifications, got %d", count, len(received))
	}
	for i := 0; i < count; i++ {
		if received[i] != i {
			t.Fatalf("ordering mismatch at index %d: got %d, want %d", i, received[i], i)
		}
	}
}
