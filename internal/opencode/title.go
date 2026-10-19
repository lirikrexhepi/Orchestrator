package opencode

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/logger"
)

const titleIdleTimeout = 3 * time.Minute

var preferredTitleModels = []string{
	"deepseek-v4.1-flash-free",
	"glm-5.3-flash-free",
	"mimo-v2.6-flash-free",
	"nemotron-3.5-lightning-free",
}

type titleModel struct {
	providerID string
	id         string
	variant    string
}

type titleCandidate struct {
	providerID string
	id         string
	variants   []string
}

type TitleServer struct {
	settings domain.ProviderSettings
	dataDir  string
	workDir  string
	client   *http.Client

	mu    sync.Mutex
	srv   *server
	api   *httpClient
	major int
	model *titleModel
	idle  *time.Timer
}

func NewTitleServer(settings domain.ProviderSettings, dataDir, workDir string) *TitleServer {
	return &TitleServer{settings: settings, dataDir: dataDir, workDir: workDir, client: &http.Client{}}
}

func (t *TitleServer) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopLocked()
}

func (t *TitleServer) stopLocked() {
	if t.idle != nil {
		t.idle.Stop()
		t.idle = nil
	}
	if t.srv != nil {
		go t.srv.stop()
	}
	t.srv, t.api, t.model = nil, nil, nil
}

func (t *TitleServer) isolated() domain.ProviderSettings {
	settings := t.settings
	env := make(map[string]string, len(settings.Env)+3)
	for key, value := range settings.Env {
		env[key] = value
	}
	env["XDG_DATA_HOME"] = t.dataDir
	if t.major >= 2 {
		env["XDG_STATE_HOME"] = t.dataDir
	}
	settings.Env = env
	settings.ServerURL = ""
	settings.LaunchArgs = ""
	return settings
}

func (t *TitleServer) alive() bool {
	if t.api == nil {
		return false
	}
	if t.srv != nil && t.srv.proc != nil {
		select {
		case <-t.srv.proc.Done():
			return false
		default:
		}
	}
	return true
}

func (t *TitleServer) ensureLocked(ctx context.Context) error {
	if t.alive() {
		return nil
	}
	t.stopLocked()
	if err := os.MkdirAll(t.dataDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(t.workDir, 0o755); err != nil {
		return err
	}
	t.major = MajorVersion(ctx, t.settings)
	settings := t.isolated()
	if t.major >= 2 {
		srv, api, err := startV2Server(ctx, settings, t.client)
		if err != nil {
			return err
		}
		t.srv, t.api = srv, api
		return nil
	}
	srv, err := startServer(ctx, settings, t.client, t.workDir)
	if err != nil {
		return err
	}
	t.srv, t.api = srv, &httpClient{base: srv.baseURL, client: t.client}
	return nil
}

func (t *TitleServer) armIdleLocked() {
	if t.idle != nil {
		t.idle.Stop()
	}
	t.idle = time.AfterFunc(titleIdleTimeout, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.stopLocked()
	})
}

func (t *TitleServer) Generate(ctx context.Context, prompt string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.ensureLocked(ctx); err != nil {
		return "", err
	}
	defer t.armIdleLocked()

	if t.model == nil {
		model, err := t.pickModel(ctx)
		if err != nil {
			return "", err
		}
		t.model = model
		logger.Infof("Titler", "OpenCode title model: %s/%s (%s)", model.providerID, model.id, model.variant)
	}
	if t.major >= 2 {
		return t.generateV2(ctx, prompt)
	}
	return t.generateV1(ctx, prompt)
}

func (t *TitleServer) pickModel(ctx context.Context) (*titleModel, error) {
	var candidates []titleCandidate
	if t.major >= 2 {
		models, err := fetchV2Models(ctx, t.api)
		if err != nil {
			return nil, err
		}
		for _, model := range models {
			if !model.Enabled || model.Status == "deprecated" {
				continue
			}
			candidate := titleCandidate{providerID: model.ProviderID, id: model.ID}
			for _, variant := range model.Variants {
				candidate.variants = append(candidate.variants, variant.ID)
			}
			candidates = append(candidates, candidate)
		}
	} else {
		var catalog struct {
			Providers []struct {
				ID     string `json:"id"`
				Models map[string]struct {
					Variants map[string]any `json:"variants"`
				} `json:"models"`
			} `json:"providers"`
		}
		if err := t.api.do(ctx, http.MethodGet, "/config/providers", nil, &catalog); err != nil {
			return nil, err
		}
		for _, provider := range catalog.Providers {
			for id, model := range provider.Models {
				candidate := titleCandidate{providerID: provider.ID, id: id}
				for variant := range model.Variants {
					candidate.variants = append(candidate.variants, variant)
				}
				candidates = append(candidates, candidate)
			}
		}
	}
	model, ok := pickTitleModel(candidates)
	if !ok {
		return nil, errors.New("no OpenCode model available for titles")
	}
	return model, nil
}

func titleScore(id string) int {
	lower := strings.ToLower(id)
	score := 0
	for _, cheap := range []string{"flash", "lightning", "nano", "mini", "lite", "haiku", "small"} {
		if strings.Contains(lower, cheap) {
			score += 3
			break
		}
	}
	if strings.Contains(lower, "free") {
		score += 2
	}
	for _, heavy := range []string{"ultra", "opus", "pro", "max", "thinking", "preview"} {
		if strings.Contains(lower, heavy) {
			score -= 3
		}
	}
	return score
}

func pickTitleModel(candidates []titleCandidate) (*titleModel, bool) {
	if len(candidates) == 0 {
		return nil, false
	}
	chosen := -1
	for _, preferred := range preferredTitleModels {
		for i, candidate := range candidates {
			if candidate.providerID == "opencode" && candidate.id == preferred {
				chosen = i
				break
			}
		}
		if chosen >= 0 {
			break
		}
	}
	if chosen < 0 {
		order := make([]int, len(candidates))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool {
			left, right := candidates[order[a]], candidates[order[b]]
			if titleScore(left.id) != titleScore(right.id) {
				return titleScore(left.id) > titleScore(right.id)
			}
			return left.providerID+"/"+left.id < right.providerID+"/"+right.id
		})
		chosen = order[0]
	}
	candidate := candidates[chosen]
	model := &titleModel{providerID: candidate.providerID, id: candidate.id}
	for _, wanted := range []string{"none", "minimal", "low"} {
		for _, variant := range candidate.variants {
			if variant == wanted {
				model.variant = variant
				return model, true
			}
		}
	}
	return model, true
}

func (t *TitleServer) generateV1(ctx context.Context, prompt string) (string, error) {
	var created Session
	if err := t.api.do(ctx, http.MethodPost, "/session", CreateSessionRequest{Title: "composer-title"}, &created); err != nil {
		return "", err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = t.api.do(cleanup, http.MethodDelete, "/session/"+created.ID, nil, nil)
	}()

	body := map[string]any{
		"agent": "title",
		"model": ModelRef{ProviderID: t.model.providerID, ModelID: t.model.id},
		"parts": []any{TextPart{Type: "text", Text: prompt}},
	}
	if t.model.variant != "" {
		body["variant"] = t.model.variant
	}
	var reply struct {
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
		Info struct {
			Error map[string]any `json:"error"`
		} `json:"info"`
	}
	if err := t.api.do(ctx, http.MethodPost, "/session/"+created.ID+"/message", body, &reply); err != nil {
		return "", err
	}
	var text strings.Builder
	for _, part := range reply.Parts {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	if text.Len() == 0 {
		return "", errors.New("OpenCode returned no title")
	}
	return text.String(), nil
}

func (t *TitleServer) generateV2(ctx context.Context, prompt string) (string, error) {
	request := v2SessionCreate{
		Title:       "composer-title",
		Agent:       "title",
		Model:       &v2ModelRef{ID: t.model.id, ProviderID: t.model.providerID, Variant: t.model.variant},
		Location:    &v2Location{Directory: t.workDir},
		Permissions: []v2Rule{{Action: "*", Resource: "*", Effect: "deny"}},
	}
	var created v2Envelope[v2Session]
	if err := t.api.do(ctx, http.MethodPost, "/api/session", request, &created); err != nil {
		return "", err
	}
	id := url.PathEscape(created.Data.ID)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = t.api.do(cleanup, http.MethodDelete, "/api/session/"+id, nil, nil)
	}()

	if err := t.api.do(ctx, http.MethodPost, "/api/session/"+id+"/prompt", v2Prompt{Text: prompt}, nil); err != nil {
		return "", err
	}
	if err := t.api.do(ctx, http.MethodPost, "/api/experimental/session/"+id+"/wait", nil, nil); err != nil {
		return "", err
	}
	var messages v2Envelope[[]struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}]
	if err := t.api.do(ctx, http.MethodGet, "/api/session/"+id+"/message?order=desc&limit=10", nil, &messages); err != nil {
		return "", err
	}
	for _, message := range messages.Data {
		if message.Type != "assistant" {
			continue
		}
		var text strings.Builder
		for _, item := range message.Content {
			if item.Type == "text" {
				text.WriteString(item.Text)
			}
		}
		if text.Len() > 0 {
			return text.String(), nil
		}
	}
	return "", errors.New("OpenCode returned no title")
}
