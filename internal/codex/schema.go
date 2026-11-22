package codex

import "encoding/json"

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Title   string `json:"title,omitempty"`
}

type InitializeParams struct {
	ClientInfo Implementation `json:"clientInfo"`
}

type UserInput struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Path string `json:"path,omitempty"`
	URL  string `json:"url,omitempty"`
}

type ThreadStartParams struct {
	Cwd                   string `json:"cwd,omitempty"`
	Model                 string `json:"model,omitempty"`
	Effort                string `json:"effort,omitempty"`
	ApprovalPolicy        string `json:"approvalPolicy,omitempty"`
	Sandbox               string `json:"sandbox,omitempty"`
	DeveloperInstructions string `json:"developerInstructions,omitempty"`
}

type Thread struct {
	ID  string `json:"id"`
	Cwd string `json:"cwd,omitempty"`
}

type ThreadStartResponse struct {
	Thread Thread `json:"thread"`
	Model  string `json:"model,omitempty"`
}

type ThreadResumeParams struct {
	ThreadID              string `json:"threadId"`
	Cwd                   string `json:"cwd,omitempty"`
	Model                 string `json:"model,omitempty"`
	Effort                string `json:"effort,omitempty"`
	ApprovalPolicy        string `json:"approvalPolicy,omitempty"`
	Sandbox               string `json:"sandbox,omitempty"`
	DeveloperInstructions string `json:"developerInstructions,omitempty"`
}

type TurnStartParams struct {
	ThreadID string      `json:"threadId"`
	Input    []UserInput `json:"input"`
	Model    string      `json:"model,omitempty"`
	Effort   string      `json:"effort,omitempty"`
	Cwd      string      `json:"cwd,omitempty"`
}

type Turn struct {
	ID     string          `json:"id"`
	Status string          `json:"status,omitempty"`
	Error  *TurnError      `json:"error,omitempty"`
	Usage  json.RawMessage `json:"usage,omitempty"`
}

type TurnError struct {
	Message string `json:"message"`
}

type TurnStartResponse struct {
	Turn Turn `json:"turn"`
}

type TurnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type FileUpdateChange struct {
	Path    string `json:"path"`
	Kind    string `json:"kind,omitempty"`
	Diff    string `json:"diff,omitempty"`
	OldText string `json:"oldText,omitempty"`
	NewText string `json:"newText,omitempty"`
}

type ThreadItem struct {
	ID   string `json:"id"`
	Type string `json:"type"`

	Text    string   `json:"text,omitempty"`
	Summary []string `json:"summary,omitempty"`
	Content []string `json:"content,omitempty"`

	Command          string `json:"command,omitempty"`
	Cwd              string `json:"cwd,omitempty"`
	AggregatedOutput string `json:"aggregatedOutput,omitempty"`
	ExitCode         *int   `json:"exitCode,omitempty"`

	Changes []FileUpdateChange `json:"changes,omitempty"`
	Status  string             `json:"status,omitempty"`

	Name      string          `json:"name,omitempty"`
	Server    string          `json:"server,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

type ItemNotification struct {
	ThreadID string     `json:"threadId"`
	TurnID   string     `json:"turnId"`
	Item     ThreadItem `json:"item"`
}

type AgentMessageDelta struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type CommandOutputDelta struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Chunk    string `json:"chunk,omitempty"`
	Delta    string `json:"delta,omitempty"`
}

type TurnCompletedNotification struct {
	ThreadID string `json:"threadId"`
	Turn     Turn   `json:"turn"`
}

type TurnStartedNotification struct {
	ThreadID string `json:"threadId"`
	Turn     Turn   `json:"turn"`
}

type PlanUpdatedNotification struct {
	ThreadID string     `json:"threadId"`
	TurnID   string     `json:"turnId"`
	Plan     []PlanStep `json:"plan,omitempty"`
	Steps    []PlanStep `json:"steps,omitempty"`
}

type PlanStep struct {
	Step   string `json:"step,omitempty"`
	Text   string `json:"text,omitempty"`
	Status string `json:"status,omitempty"`
}

type TokenBreakdown struct {
	TotalTokens           int64 `json:"totalTokens"`
	InputTokens           int64 `json:"inputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
	ContextWindow         int64 `json:"contextWindow"`
}

type TokenUsageNotification struct {
	ThreadID   string `json:"threadId"`
	TurnID     string `json:"turnId,omitempty"`
	TokenUsage *struct {
		Last               *TokenBreakdown `json:"last,omitempty"`
		Total              *TokenBreakdown `json:"total,omitempty"`
		ModelContextWindow int64           `json:"modelContextWindow,omitempty"`
	} `json:"tokenUsage,omitempty"`
	Usage TokenBreakdown `json:"usage"`
}

type ApprovalRequest struct {
	ThreadID    string             `json:"threadId"`
	TurnID      string             `json:"turnId"`
	ItemID      string             `json:"itemId"`
	CallID      string             `json:"callId,omitempty"`
	Command     string             `json:"command,omitempty"`
	Cwd         string             `json:"cwd,omitempty"`
	Reason      string             `json:"reason,omitempty"`
	Changes     []FileUpdateChange `json:"changes,omitempty"`
	Permissions map[string]any     `json:"permissions,omitempty"`
	Raw         json.RawMessage    `json:"-"`
}

type ApprovalResponse struct {
	Decision string `json:"decision"`
}

type PermissionsApprovalResponse struct {
	Permissions map[string]any `json:"permissions"`
	Scope       string         `json:"scope"`
}

const (
	DecisionApproved           = "accept"
	DecisionApprovedForSession = "acceptForSession"
	DecisionDenied             = "decline"
	DecisionAbort              = "cancel"
)
