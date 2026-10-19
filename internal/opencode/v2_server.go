package opencode

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/logger"
	"composer/internal/process"
	"composer/internal/provider"
	"composer/internal/shell"
)

const (
	v2Username        = "opencode"
	v2PasswordEnv     = "OPENCODE_SERVER_PASSWORD"
	versionCacheTTL   = 30 * time.Second
	v2HealthTimeout   = 30 * time.Second
	v2ModelListBudget = 45 * time.Second
)

type versionEntry struct {
	major int
	at    time.Time
}

var (
	versionMu    sync.Mutex
	versionCache = map[string]versionEntry{}
)

func MajorVersion(ctx context.Context, settings domain.ProviderSettings) int {
	key := binary(settings) + "|" + strings.TrimSpace(settings.ServerURL)
	versionMu.Lock()
	if entry, ok := versionCache[key]; ok && time.Since(entry.at) < versionCacheTTL {
		versionMu.Unlock()
		return entry.major
	}
	versionMu.Unlock()

	major := detectMajor(ctx, settings)
	versionMu.Lock()
	versionCache[key] = versionEntry{major: major, at: time.Now()}
	versionMu.Unlock()
	return major
}

func detectMajor(ctx context.Context, settings domain.ProviderSettings) int {
	if url := strings.TrimSuffix(strings.TrimSpace(settings.ServerURL), "/"); url != "" {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url+"/api/info", nil)
		if err != nil {
			return 1
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 1
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return 1
		}
		return 2
	}
	snapshot := provider.ProbeVersion(ctx, binary(settings), settings)
	return MajorOf(snapshot.Version)
}

func MajorOf(version string) int {
	head, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")
	major, err := strconv.Atoi(head)
	if err != nil || major < 1 {
		return 1
	}
	return major
}

func IsV2(ctx context.Context, settings domain.ProviderSettings) bool {
	return MajorVersion(ctx, settings) >= 2
}

func newPassword() string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func startV2Server(ctx context.Context, settings domain.ProviderSettings, client *http.Client) (*server, *httpClient, error) {
	if url := strings.TrimSpace(settings.ServerURL); url != "" {
		api := &httpClient{base: strings.TrimSuffix(url, "/"), client: client, password: settings.Env[v2PasswordEnv]}
		return &server{baseURL: api.base}, api, nil
	}

	port, err := freePort()
	if err != nil {
		return nil, nil, err
	}
	password := newPassword()
	args := append(
		[]string{"serve", "--hostname=" + defaultHostname, fmt.Sprintf("--port=%d", port), "--print-logs", "--log-level=info"},
		shell.TokenizeArgs(settings.LaunchArgs)...,
	)
	env := provider.LaunchEnv(settings)
	env[v2PasswordEnv] = password

	cwd, _ := os.UserHomeDir()
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	logger.Infof("OpenCode", "Starting v2 server: %s %s", binary(settings), strings.Join(args, " "))

	procCtx, cancel := context.WithCancel(context.Background())
	proc, err := process.Start(procCtx, process.Spec{
		Command:       binary(settings),
		Args:          args,
		Env:           env,
		Cwd:           cwd,
		DieWithParent: true,
		Stderr: func(line string) {
			logger.Debugf("OpenCode:err", "%s", line)
		},
	})
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start opencode serve: %w", err)
	}

	discovered := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(proc.Stdout())
		for scanner.Scan() {
			line := scanner.Text()
			logger.Debugf("OpenCode:out", "%s", line)
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
	case <-time.After(5 * time.Second):
	case <-proc.Done():
		cancel()
		return nil, nil, fmt.Errorf("opencode serve exited: %s", proc.StderrTail())
	}

	srv := &server{baseURL: baseURL, proc: proc, cancel: cancel}
	api := &httpClient{base: baseURL, client: client, password: password}
	if err := waitV2Healthy(ctx, srv, api); err != nil {
		srv.stop()
		return nil, nil, err
	}
	logger.Infof("OpenCode", "v2 server healthy at %s", baseURL)
	return srv, api, nil
}

func waitV2Healthy(ctx context.Context, srv *server, api *httpClient) error {
	deadline := time.Now().Add(v2HealthTimeout)
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := api.do(probeCtx, http.MethodGet, "/api/info", nil, nil)
		cancel()
		if err == nil {
			return nil
		}
		if srv.proc != nil {
			select {
			case <-srv.proc.Done():
				return fmt.Errorf("opencode serve exited: %s", srv.proc.StderrTail())
			default:
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("opencode server did not become healthy at %s: %v", api.base, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func subscribeV2(ctx context.Context, api *httpClient, onEvent func(v2Event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api.base+"/api/event", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if api.password != "" {
		req.SetBasicAuth(v2Username, api.password)
	}
	resp, err := api.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("event stream returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 128*1024), 16*1024*1024)
	var data strings.Builder
	flush := func() {
		if data.Len() == 0 {
			return
		}
		var event v2Event
		if json.Unmarshal([]byte(data.String()), &event) == nil && event.Type != "" {
			onEvent(event)
		}
		data.Reset()
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if payload, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(payload, " "))
		}
	}
	flush()
	return scanner.Err()
}

func fetchV2Models(ctx context.Context, api *httpClient) ([]v2ModelInfo, error) {
	var envelope v2Envelope[[]v2ModelInfo]
	if err := api.do(ctx, http.MethodGet, "/api/model", nil, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data, nil
}

type ModelInfo struct {
	ID       string
	Name     string
	Variants []string
}

func ListModels(ctx context.Context, settings domain.ProviderSettings) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, v2ModelListBudget)
	defer cancel()
	srv, api, err := startV2Server(ctx, settings, &http.Client{})
	if err != nil {
		return nil, err
	}
	defer srv.stop()

	models, err := fetchV2Models(ctx, api)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(models))
	for _, model := range models {
		if !model.Enabled || model.Status == "deprecated" {
			continue
		}
		info := ModelInfo{ID: model.ProviderID + "/" + model.ID, Name: model.Name}
		for _, variant := range model.Variants {
			if variant.ID != "" && variant.ID != "default" {
				info.Variants = append(info.Variants, variant.ID)
			}
		}
		out = append(out, info)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
