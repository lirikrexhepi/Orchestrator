package claude

import "encoding/json"

type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type Message struct {
	ID         string         `json:"id,omitempty"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model,omitempty"`
	StopReason string         `json:"stop_reason,omitempty"`
	Usage      *Usage         `json:"usage,omitempty"`
}

type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	ToolUses                 int   `json:"tool_uses,omitempty"`
	DurationMs               int64 `json:"duration_ms,omitempty"`
	TotalTokens              int64 `json:"total_tokens,omitempty"`
}

// Envelope is the union of every line the CLI emits on stdout under
// --output-format stream-json.
type Envelope struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Message   *Message        `json:"message,omitempty"`
	Model     string          `json:"model,omitempty"`
	Tools     []string        `json:"tools,omitempty"`
	Cwd       string          `json:"cwd,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`
	// ParentToolUseID is set on frames produced inside a subagent (Task tool).
	ParentToolUseID string          `json:"parent_tool_use_id,omitempty"`
	UUID            string          `json:"uuid,omitempty"`
	ToolUseID       string          `json:"tool_use_id,omitempty"`
	TaskStatus      string          `json:"status,omitempty"`
	TaskSummary     string          `json:"summary,omitempty"`
	ToolUseResult   json.RawMessage `json:"tool_use_result,omitempty"`

	IsError    bool                  `json:"is_error,omitempty"`
	StopReason string                `json:"stop_reason,omitempty"`
	Result     string                `json:"result,omitempty"`
	TotalCost  float64               `json:"total_cost_usd,omitempty"`
	NumTurns   int                   `json:"num_turns,omitempty"`
	Usage      *Usage                `json:"usage,omitempty"`
	ModelUsage map[string]ModelUsage `json:"modelUsage,omitempty"`

	RateLimitInfo *RateLimitInfo  `json:"rate_limit_info,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	Request       *ControlPayload `json:"request,omitempty"`
}

// RateLimitInfo rides on `type: "rate_limit_event"` frames and is the only place
// the CLI reports subscription quota. Fields beyond the window type and reset are
// optional: an unconstrained account reports status and reset with no percentage.
type RateLimitInfo struct {
	Status        string `json:"status,omitempty"`
	RateLimitType string `json:"rateLimitType,omitempty"`
	ResetsAt      int64  `json:"resetsAt,omitempty"`
	UsedPercent   *int   `json:"usedPercent,omitempty"`
	RemainingPct  *int   `json:"remainingPercent,omitempty"`
}

type InputMessage struct {
	Type    string       `json:"type"`
	Message InputContent `json:"message"`
}

type InputContent struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type textInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type imageInput struct {
	Type   string      `json:"type"`
	Source imageSource `json:"source"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

func UserText(text string) InputMessage {
	return InputMessage{
		Type:    "user",
		Message: InputContent{Role: "user", Content: []any{textInput{Type: "text", Text: text}}},
	}
}

// ControlPayload is the body of a control_request the CLI sends us. Only the
// can_use_tool fields are decoded; other subtypes are answered with an error.
type ControlPayload struct {
	Subtype               string          `json:"subtype"`
	ToolName              string          `json:"tool_name,omitempty"`
	Input                 json.RawMessage `json:"input,omitempty"`
	ToolUseID             string          `json:"tool_use_id,omitempty"`
	PermissionSuggestions json.RawMessage `json:"permission_suggestions,omitempty"`
	DecisionReason        string          `json:"decision_reason,omitempty"`
	BlockedPath           string          `json:"blocked_path,omitempty"`
}

// ControlResponse answers a CLI control_request. The request id lives inside
// `response`, matching what the Agent SDK writes; a top-level id is ignored.
type ControlResponse struct {
	Type     string              `json:"type"`
	Response ControlResponseBody `json:"response"`
}

type ControlResponseBody struct {
	Subtype   string `json:"subtype"`
	RequestID string `json:"request_id"`
	Response  any    `json:"response,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ControlRequestOut is a control_request we send to the CLI (interrupt,
// set_model, set_permission_mode).
type ControlRequestOut struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Request   any    `json:"request"`
}

// streamEvent is the Messages API streaming event carried by stream_event
// frames under --include-partial-messages.
type streamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		ID string `json:"id"`
	} `json:"message,omitempty"`
	Delta *struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		Thinking string `json:"thinking,omitempty"`
	} `json:"delta,omitempty"`
}
