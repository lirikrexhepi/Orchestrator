package opencode

import (
	"encoding/json"
	"strings"
)

type v2Envelope[T any] struct {
	Data T `json:"data"`
}

type v2ModelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant,omitempty"`
}

type v2Location struct {
	Directory string `json:"directory"`
}

type v2Rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

type v2SessionCreate struct {
	Title       string      `json:"title,omitempty"`
	Agent       string      `json:"agent,omitempty"`
	Model       *v2ModelRef `json:"model,omitempty"`
	Location    *v2Location `json:"location,omitempty"`
	Permissions []v2Rule    `json:"permissions,omitempty"`
}

type v2Session struct {
	ID string `json:"id"`
}

type v2PromptFile struct {
	URI  string `json:"uri"`
	Name string `json:"name,omitempty"`
}

type v2Prompt struct {
	Text  string         `json:"text"`
	Files []v2PromptFile `json:"files,omitempty"`
}

type v2Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type v2Tokens struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache"`
}

type v2Error struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (e *v2Error) String() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Type
}

type v2ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type v2StreamData struct {
	SessionID          string          `json:"sessionID"`
	AssistantMessageID string          `json:"assistantMessageID"`
	Ordinal            int             `json:"ordinal"`
	Delta              string          `json:"delta"`
	Text               string          `json:"text"`
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Input              json.RawMessage `json:"input"`
	Content            []v2ToolContent `json:"content"`
	Error              *v2Error        `json:"error"`
	Model              *v2ModelRef     `json:"model"`
	Tokens             *v2Tokens       `json:"tokens"`
	Cost               float64         `json:"cost"`
	Reason             string          `json:"reason"`
}

type v2PermissionAsked struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionID"`
	Action    string   `json:"action"`
	Resources []string `json:"resources"`
	Message   string   `json:"message"`
}

type v2FormOption struct {
	Value       any    `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type v2FormField struct {
	Key         string         `json:"key"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Type        string         `json:"type"`
	Hidden      bool           `json:"hidden"`
	Options     []v2FormOption `json:"options"`
}

type v2Form struct {
	ID        string        `json:"id"`
	SessionID string        `json:"sessionID"`
	Title     string        `json:"title"`
	Fields    []v2FormField `json:"fields"`
}

type v2FormCreated struct {
	Form v2Form `json:"form"`
}

type v2ModelInfo struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Status     string `json:"status"`
	Variants   []struct {
		ID string `json:"id"`
	} `json:"variants"`
	Limit struct {
		Context int64 `json:"context"`
	} `json:"limit"`
}

var v2ToolNames = map[string]string{
	"shell":    "bash",
	"subagent": "task",
}

func NormalizeToolName(name string) string {
	if mapped, ok := v2ToolNames[strings.ToLower(name)]; ok {
		return mapped
	}
	return name
}

func v2ModelRefFor(model, variant string) *v2ModelRef {
	ref := parseModelRef(model)
	if ref == nil {
		return nil
	}
	return &v2ModelRef{ID: ref.ModelID, ProviderID: ref.ProviderID, Variant: variant}
}
