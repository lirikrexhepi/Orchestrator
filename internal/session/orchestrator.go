package session

// TaskRequest is one delegated unit parsed out of an orchestrator plan or MCP tool call.
type TaskRequest struct {
	// Action specifies whether to spawn a new agent ("spawn") or send a message
	// to an existing agent ("message"). Defaults to "spawn".
	Action string `json:"action,omitempty"`
	// TargetThreadID names the existing agent thread to receive this prompt
	// when Action is "message".
	TargetThreadID string `json:"targetThreadId,omitempty"`
	Title          string `json:"title"`
	Prompt         string `json:"prompt"`
	// Cwd is the absolute directory the task should run in, when the user named
	// one. Empty means the current project.
	Cwd string `json:"cwd,omitempty"`
}
