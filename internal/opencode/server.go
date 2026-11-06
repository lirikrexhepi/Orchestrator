package opencode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/logger"
	"composer/internal/process"
	"composer/internal/provider"
	"composer/internal/shell"
)

const defaultHostname = "127.0.0.1"

var (
	listeningPattern  = regexp.MustCompile(`on\s+(https?://[^\s]+)`)
	logLevelPattern   = regexp.MustCompile(`(?:^|\s)level=(\w+)`)
	sessionIDPattern  = regexp.MustCompile(`(?:^|\s)session\.id=([^\s]+)`)
	errorFieldPattern = regexp.MustCompile(`(?:^|\s)error\.error="([^"]+)"`)
	errorBarePattern  = regexp.MustCompile(`(?:^|\s)error\.error=([^\s]+)`)
	messagePattern    = regexp.MustCompile(`(?:^|\s)message="([^"]+)"`)
	streamErrPattern  = regexp.MustCompile(`message="stream error"`)
)

func parseOpenCodeLogLine(line string) (level, sessionID, errMsg string) {
	if m := logLevelPattern.FindStringSubmatch(line); m != nil {
		level = strings.ToUpper(m[1])
	}
	if m := sessionIDPattern.FindStringSubmatch(line); m != nil {
		sessionID = m[1]
	}
	if m := errorFieldPattern.FindStringSubmatch(line); m != nil {
		errMsg = m[1]
	} else if m := errorBarePattern.FindStringSubmatch(line); m != nil {
		errMsg = m[1]
	} else if streamErrPattern.MatchString(line) {
		errMsg = "OpenCode stream error"
	} else if level == "ERROR" {
		if m := messagePattern.FindStringSubmatch(line); m != nil {
			errMsg = m[1]
		}
	}
	return level, sessionID, errMsg
}

// server owns a managed `opencode serve` child. When settings point at an
// external ServerURL the process is skipped entirely and only the base URL is
// used.
type server struct {
	baseURL string
	proc    *process.Process
	cancel  context.CancelFunc
	onError func(sessionID, errMsg string)
	onExit  func(tail string)
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", defaultHostname+":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func binary(settings domain.ProviderSettings) string {
	if settings.BinaryPath != "" {
		return settings.BinaryPath
	}
	return "opencode"
}

func startServer(
	ctx context.Context,
	settings domain.ProviderSettings,
	client *http.Client,
	cwd string,
	onError func(sessionID, errMsg string),
	onExit func(tail string),
) (*server, error) {
	if url := strings.TrimSpace(settings.ServerURL); url != "" {
		return &server{baseURL: strings.TrimSuffix(url, "/"), onError: onError, onExit: onExit}, nil
	}

	port, err := freePort()
	if err != nil {
		return nil, err
	}

	args := append(
		[]string{"serve", "--hostname=" + defaultHostname, fmt.Sprintf("--port=%d", port), "--print-logs", "--log-level=INFO"},
		shell.TokenizeArgs(settings.LaunchArgs)...,
	)
	env := provider.LaunchEnv(settings)

	procCwd := cwd
	if procCwd == "" {
		procCwd, _ = os.Getwd()
	}
	if procCwd == "" {
		procCwd, _ = os.UserHomeDir()
	}

	logger.Infof("OpenCode", "Starting server: %s %s (cwd=%s)", binary(settings), strings.Join(args, " "), procCwd)

	handleLogLine := func(source, line string) {
		level, sessionID, errMsg := parseOpenCodeLogLine(line)
		switch level {
		case "ERROR":
			logger.Errorf("OpenCode:"+source, "%s", line)
			if onError != nil && errMsg != "" {
				onError(sessionID, errMsg)
			}
		case "WARN":
			logger.Warnf("OpenCode:"+source, "%s", line)
		default:
			logger.Debugf("OpenCode:"+source, "%s", line)
		}
	}

	procCtx, cancel := context.WithCancel(context.Background())
	proc, err := process.Start(procCtx, process.Spec{
		Command: binary(settings),
		Args:    args,
		Env:     env,
		Cwd:     procCwd,

		DieWithParent: true,
		Stderr: func(line string) {
			handleLogLine("err", line)
		},
	})
	if err != nil {
		cancel()
		logger.Errorf("OpenCode", "Failed to start server: %v", err)
		return nil, fmt.Errorf("start opencode serve: %w", err)
	}

	discovered := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(proc.Stdout())
		for scanner.Scan() {
			line := scanner.Text()
			handleLogLine("out", line)
			if match := listeningPattern.FindStringSubmatch(line); match != nil {
				select {
				case discovered <- strings.TrimSuffix(match[1], "/"):
				default:
				}
			}
		}
	}()

	baseURL := fmt.Sprintf("http://%s:%d", defaultHostname, port)
	select {
	case url := <-discovered:
		baseURL = url
	case <-time.After(3 * time.Second):
	case <-proc.Done():
		cancel()
		tail := proc.StderrTail()
		logger.Errorf("OpenCode", "Server exited prematurely: %s", tail)
		return nil, fmt.Errorf("opencode serve exited: %s", tail)
	}

	go func() {
		<-proc.Done()
		err := proc.ExitError()
		tail := proc.StderrTail()
		logger.Warnf("OpenCode", "Server process exited (err=%v, baseURL=%s): %s", err, baseURL, tail)
		if onExit != nil {
			onExit(tail)
		}
	}()

	s := &server{baseURL: baseURL, proc: proc, cancel: cancel, onError: onError, onExit: onExit}
	if err := s.waitHealthy(ctx, client); err != nil {
		logger.Errorf("OpenCode", "Health check failed for %s: %v", baseURL, err)
		s.stop()
		return nil, err
	}
	logger.Infof("OpenCode", "Server healthy at %s", baseURL)
	return s, nil
}

func (s *server) waitHealthy(ctx context.Context, client *http.Client) error {
	deadline := time.Now().Add(20 * time.Second)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/global/health", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode < 400 {
				return nil
			}
		}

		if s.proc != nil {
			select {
			case <-s.proc.Done():
				return fmt.Errorf("opencode serve exited: %s", s.proc.StderrTail())
			default:
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("opencode server did not become healthy at %s", s.baseURL)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *server) stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.proc != nil {
		_ = s.proc.Shutdown(2 * time.Second)
	}
}

// subscribe streams GET /event and yields decoded frames until ctx ends.
func subscribe(ctx context.Context, client *http.Client, baseURL string, onEvent func(Event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/event", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("event stream returned %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 128*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			logger.Warnf("OpenCode SSE", "Failed to decode event: %v (payload=%s)", err, payload)
			continue
		}
		onEvent(event)
	}
	return scanner.Err()
}

type httpClient struct {
	base     string
	client   *http.Client
	password string
	mu       sync.Mutex
}

func (h *httpClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(encoded))
	}

	req, err := http.NewRequestWithContext(ctx, method, h.base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.password != "" {
		req.SetBasicAuth(v2Username, h.password)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(detail)))
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
