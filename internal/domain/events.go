package domain

type EventKind string

const (
	EventSessionStarted   EventKind = "session.started"
	EventSessionStopped   EventKind = "session.stopped"
	EventTurnStarted      EventKind = "turn.started"
	EventTurnCompleted    EventKind = "turn.completed"
	EventTurnFailed       EventKind = "turn.failed"
	EventAgentMessage     EventKind = "agent.message"
	EventAgentThought     EventKind = "agent.thought"
	EventToolCall         EventKind = "tool.call"
	EventToolResult       EventKind = "tool.result"
	EventSubagentMessage  EventKind = "subagent.message"
	EventSubagentToolCall EventKind = "subagent.tool.call"
	EventSubagentToolDone EventKind = "subagent.tool.result"
	EventSubagentProgress EventKind = "subagent.progress"
	EventPlan             EventKind = "plan"
	EventApprovalRequest  EventKind = "approval.request"
	EventApprovalResolved EventKind = "approval.resolved"
	EventQuestionAsked    EventKind = "question.asked"
	EventQuestionAnswered EventKind = "question.answered"
	EventUserMessage      EventKind = "user.message"
	EventNotice           EventKind = "notice"
	EventUsage            EventKind = "usage"
	EventRateLimit        EventKind = "rate.limit"
	EventDiagnostic       EventKind = "diagnostic"
	EventProviderStatus   EventKind = "provider.status"
)

type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopMaxTokens StopReason = "max_tokens"
	StopRefusal   StopReason = "refusal"
	StopCancelled StopReason = "cancelled"
	StopError     StopReason = "error"
)

type RuntimeEvent struct {
	Kind       EventKind  `json:"kind"`
	ThreadID   string     `json:"threadId"`
	TurnID     string     `json:"turnId,omitempty"`
	InstanceID string     `json:"instanceId,omitempty"`
	Driver     DriverKind `json:"driver,omitempty"`
	Model      string     `json:"model,omitempty"`
	Account    string     `json:"account,omitempty"`
	Seq        uint64     `json:"seq"`
	// ItemID identifies the content item (text block, reasoning part, tool) an
	// event belongs to, so consumers merge streamed deltas into the right block
	// and start a new one when the item changes. Empty for legacy events.
	ItemID       string `json:"itemId,omitempty"`
	ParentToolID string `json:"parentToolId,omitempty"`
	At           int64  `json:"at"`

	Text       string           `json:"text,omitempty"`
	Delta      bool             `json:"delta,omitempty"`
	Icon       string           `json:"icon,omitempty"`
	Files      []FileRef        `json:"files,omitempty"`
	Tool       *ToolCall        `json:"tool,omitempty"`
	Plan       []PlanEntry      `json:"plan,omitempty"`
	Approval   *ApprovalRequest `json:"approval,omitempty"`
	Question   *QuestionRequest `json:"question,omitempty"`
	Usage      *Usage           `json:"usage,omitempty"`
	RateLimits []RateLimit      `json:"rateLimits,omitempty"`
	StopReason StopReason       `json:"stopReason,omitempty"`
	Error      string           `json:"error,omitempty"`
}

type ToolStatus string

const (
	ToolPending    ToolStatus = "pending"
	ToolInProgress ToolStatus = "in_progress"
	ToolCompleted  ToolStatus = "completed"
	ToolFailed     ToolStatus = "failed"
)

type ToolCall struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Kind   string         `json:"kind,omitempty"`
	Status ToolStatus     `json:"status"`
	Input  map[string]any `json:"input,omitempty"`
	Output string         `json:"output,omitempty"`
	Diffs  []FileDiff     `json:"diffs,omitempty"`

	DurationMs int64 `json:"durationMs,omitempty"`
	ToolCount  int   `json:"toolCount,omitempty"`
	Tokens     int64 `json:"tokens,omitempty"`
	Async      bool  `json:"async,omitempty"`
}

type FileDiff struct {
	Path    string `json:"path"`
	OldText string `json:"oldText,omitempty"`
	NewText string `json:"newText,omitempty"`
}

type PlanEntry struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
}

type ApprovalRequest struct {
	RequestID string           `json:"requestId"`
	Title     string           `json:"title"`
	Detail    string           `json:"detail,omitempty"`
	Tool      *ToolCall        `json:"tool,omitempty"`
	Options   []ApprovalOption `json:"options,omitempty"`
}

type ApprovalOption struct {
	ID   string           `json:"id"`
	Name string           `json:"name"`
	Kind ApprovalDecision `json:"kind"`
}

type Usage struct {
	InputTokens      int64   `json:"inputTokens,omitempty"`
	OutputTokens     int64   `json:"outputTokens,omitempty"`
	CacheReadTokens  int64   `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64   `json:"cacheWriteTokens,omitempty"`
	ContextWindow    int64   `json:"contextWindow,omitempty"`
	ContextTokens    int64   `json:"contextTokens,omitempty"`
	CostUSD          float64 `json:"costUsd,omitempty"`
}

// RateLimit is one subscription quota window as the CLI reports it.
//
// UsedPercent is a pointer because "not reported" and "0% used" are different
// facts: the CLI omits the field entirely on some plans and states, and showing
// an empty bar for unknown quota would be a lie.
type RateLimit struct {
	Window      string `json:"window"`
	Status      string `json:"status,omitempty"`
	UsedPercent *int   `json:"usedPercent,omitempty"`
	ResetsAt    int64  `json:"resetsAt,omitempty"`
}

// QuestionRequest carries a structured clarification question from the agent.
type QuestionRequest struct {
	RequestID string         `json:"requestId"`
	Questions []QuestionItem `json:"questions"`
}

type QuestionItem struct {
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"`
	MultiSelect bool     `json:"isMultiSelect,omitempty"`
}
