package antigravity

import (
	"encoding/json"

	"composer/internal/domain"
)

type initializeParams struct {
	ProtocolVersion    int                   `json:"protocolVersion"`
	ClientCapabilities acpClientCapabilities `json:"clientCapabilities"`
	ClientInfo         acpClientInfo         `json:"clientInfo"`
}

type acpClientCapabilities struct {
	Fs       acpClientFsCaps `json:"fs"`
	Terminal bool            `json:"terminal"`
}

type acpClientFsCaps struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type acpClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion int            `json:"protocolVersion,omitempty"`
	ServerInfo      *acpServerInfo `json:"serverInfo,omitempty"`
}

type acpServerInfo struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type sessionNewParams struct {
	Cwd        string         `json:"cwd"`
	MCPServers []any          `json:"mcpServers"`
	Meta       map[string]any `json:"_meta,omitempty"`
}

func (p sessionNewParams) MarshalJSON() ([]byte, error) {
	type Alias sessionNewParams
	servers := p.MCPServers
	if servers == nil {
		servers = []any{}
	}
	return json.Marshal(&struct {
		Alias
		MCPServers []any `json:"mcpServers"`
	}{
		Alias:      Alias(p),
		MCPServers: servers,
	})
}

type sessionLoadParams struct {
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	MCPServers []any  `json:"mcpServers"`
}

func (p sessionLoadParams) MarshalJSON() ([]byte, error) {
	type Alias sessionLoadParams
	servers := p.MCPServers
	if servers == nil {
		servers = []any{}
	}
	return json.Marshal(&struct {
		Alias
		MCPServers []any `json:"mcpServers"`
	}{
		Alias:      Alias(p),
		MCPServers: servers,
	})
}

type sessionNewResult struct {
	SessionID     string            `json:"sessionId"`
	ConfigOptions []acpConfigOption `json:"configOptions,omitempty"`
	Models        json.RawMessage   `json:"models,omitempty"`
	Modes         json.RawMessage   `json:"modes,omitempty"`
}

type acpConfigOption struct {
	ID           string                  `json:"id"`
	Name         string                  `json:"name"`
	Category     string                  `json:"category,omitempty"`
	Type         string                  `json:"type,omitempty"`
	CurrentValue string                  `json:"currentValue,omitempty"`
	Options      []acpConfigOptionChoice `json:"options,omitempty"`
}

type acpConfigOptionChoice struct {
	Value string `json:"value"`
	Name  string `json:"name,omitempty"`
}

type sessionSetConfigParams struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

type sessionPromptParams struct {
	SessionID string `json:"sessionId"`
	Prompt    []any  `json:"prompt"`
}

type sessionPromptResult struct {
	StopReason string    `json:"stopReason"`
	Usage      *acpUsage `json:"usage,omitempty"`
}

type acpUsage struct {
	InputTokens       int64 `json:"inputTokens,omitempty"`
	OutputTokens      int64 `json:"outputTokens,omitempty"`
	CachedReadTokens  int64 `json:"cachedReadTokens,omitempty"`
	CachedWriteTokens int64 `json:"cachedWriteTokens,omitempty"`
	ThoughtTokens     int64 `json:"thoughtTokens,omitempty"`
	TotalTokens       int64 `json:"totalTokens,omitempty"`
}

type sessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

type sessionUpdateParams struct {
	SessionID string           `json:"sessionId"`
	Update    acpSessionUpdate `json:"update"`
}

type acpSessionUpdate struct {
	SessionUpdate string             `json:"sessionUpdate"`
	Content       json.RawMessage    `json:"content,omitempty"`
	ToolCallID    string             `json:"toolCallId,omitempty"`
	Name          string             `json:"name,omitempty"`
	Title         string             `json:"title,omitempty"`
	Kind          string             `json:"kind,omitempty"`
	Status        string             `json:"status,omitempty"`
	Args          map[string]any     `json:"args,omitempty"`
	RawInput      map[string]any     `json:"rawInput,omitempty"`
	RawOutput     json.RawMessage    `json:"rawOutput,omitempty"`
	Locations     []acpLocation      `json:"locations,omitempty"`
	Used          int64              `json:"used,omitempty"`
	Size          int64              `json:"size,omitempty"`
	Entries       []domain.PlanEntry `json:"entries,omitempty"`
}

type acpLocation struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

type acpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (t *acpTextContent) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		t.Type = "text"
		t.Text = s
		return nil
	}
	type raw acpTextContent
	return json.Unmarshal(data, (*raw)(t))
}

type acpImagePrompt struct {
	Type     string `json:"type"`
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type acpToolContentItem struct {
	Type    string          `json:"type"`
	Path    string          `json:"path,omitempty"`
	OldText string          `json:"oldText,omitempty"`
	NewText string          `json:"newText,omitempty"`
	Content *acpTextContent `json:"content,omitempty"`
}

type requestPermissionParams struct {
	SessionID string                `json:"sessionId"`
	ToolCall  acpToolCallPermission `json:"toolCall"`
	Options   []acpPermissionOption `json:"options"`
}

type acpToolCallPermission struct {
	ToolCallID string         `json:"toolCallId"`
	Title      string         `json:"title"`
	Kind       string         `json:"kind"`
	RawInput   map[string]any `json:"rawInput,omitempty"`
}

type acpPermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type requestPermissionResult struct {
	Outcome acpPermissionOutcome `json:"outcome"`
}

type acpPermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}
