package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"composer/internal/domain"
)

type ProviderInfo struct {
	Driver string   `json:"driver"`
	Models []string `json:"models,omitempty"`
}

type ThreadSummary struct {
	ThreadID   string                `json:"threadId"`
	Title      string                `json:"title"`
	Driver     string                `json:"driver"`
	Model      string                `json:"model,omitempty"`
	Cwd        string                `json:"cwd,omitempty"`
	Status     string                `json:"status"` // "running", "idle", "closed", "failed"
	Permission domain.PermissionMode `json:"permissionMode"`
	IsSubagent bool                  `json:"isSubagent,omitempty"`
}

type DelegateRequest struct {
	CallerThreadID string
	Title          string
	Prompt         string
	Driver         domain.DriverKind
	Model          string
	Cwd            string
	UseWorktree    bool
	Permission     domain.PermissionMode
}

type DelegateResult struct {
	ThreadID string `json:"threadId"`
	Title    string `json:"title"`
	Driver   string `json:"driver"`
	Model    string `json:"model,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Status   string `json:"status"`
}

type ThreadReadItem struct {
	Index  int    `json:"i"`
	Kind   string `json:"k"` // "user", "assistant", "tool", "error"
	Tool   string `json:"tool,omitempty"`
	Target string `json:"target,omitempty"`
	Status string `json:"status,omitempty"`
	Text   string `json:"text,omitempty"`
}

type ThreadReadResult struct {
	Thread    ThreadSummary    `json:"thread"`
	Items     []ThreadReadItem `json:"items"`
	Truncated bool             `json:"truncated,omitempty"`
}

type Backend interface {
	ListProviders(ctx context.Context) ([]ProviderInfo, error) // ready providers only
	ListThreads(ctx context.Context, limit int) ([]ThreadSummary, error)
	GetThread(ctx context.Context, threadID string) (ThreadSummary, bool, error)
	DelegateTask(ctx context.Context, req DelegateRequest) (DelegateResult, error)
	ReadThread(ctx context.Context, threadID string, view string, limit int, maxChars int) (ThreadReadResult, error)
	SendToThread(ctx context.Context, callerThreadID, targetThreadID, message string) error
	StopThread(ctx context.Context, callerThreadID, targetThreadID string, interruptOnly bool) error
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema ToolInputSchema `json:"inputSchema"`
}

type ToolInputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties,omitempty"`
	Required   []string                  `json:"required,omitempty"`
}

type PropertySchema struct {
	Type string   `json:"type"`
	Enum []string `json:"enum,omitempty"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type CallToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type CallerInfo struct {
	ThreadID       string                `json:"threadId"`
	Driver         string                `json:"driver"`
	Cwd            string                `json:"cwd"`
	PermissionMode domain.PermissionMode `json:"permissionMode"`
	IsSubagent     bool                  `json:"isSubagent,omitempty"`
}

type CapabilitiesResult struct {
	Caller    CallerInfo     `json:"caller"`
	Providers []ProviderInfo `json:"providers"`
}

var allTools = []Tool{
	{
		Name:        "orchestrator_capabilities",
		Description: "Caller thread info and ready providers.",
		InputSchema: ToolInputSchema{
			Type: "object",
		},
	},
	{
		Name:        "delegate_task",
		Description: "Spawn a child thread on a provider for a self-contained task.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"title":          {Type: "string"},
				"task":           {Type: "string"},
				"driver":         {Type: "string", Enum: []string{"claude", "antigravity", "opencode", "codex"}},
				"model":          {Type: "string"},
				"cwd":            {Type: "string"},
				"useWorktree":    {Type: "boolean"},
				"permissionMode": {Type: "string", Enum: []string{"inherit", "plan", "default", "acceptEdits", "bypassPermissions"}},
			},
			Required: []string{"title", "task"},
		},
	},
	{
		Name:        "list_threads",
		Description: "List active and recent Orchestrator threads.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"limit": {Type: "integer"},
			},
		},
	},
	{
		Name:        "read_thread",
		Description: "Read recent messages or activity from a thread.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"threadId": {Type: "string"},
				"view":     {Type: "string", Enum: []string{"messages", "activity"}},
				"limit":    {Type: "integer"},
				"maxChars": {Type: "integer"},
			},
			Required: []string{"threadId"},
		},
	},
	{
		Name:        "send_to_thread",
		Description: "Send a follow-up message to another thread.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"threadId": {Type: "string"},
				"message":  {Type: "string"},
			},
			Required: []string{"threadId", "message"},
		},
	},
	{
		Name:        "stop_thread",
		Description: "Interrupt or stop another thread.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"threadId":      {Type: "string"},
				"interruptOnly": {Type: "boolean"},
			},
			Required: []string{"threadId"},
		},
	},
}

// AllTools returns a slice of registered tools.
func AllTools() []Tool {
	return allTools
}

// IsKnownTool reports whether name is a recognized tool.
func IsKnownTool(name string) bool {
	for _, t := range allTools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// ToolAcceptedKeys maps each tool to its allowed top-level argument keys.
var toolAcceptedKeys = map[string][]string{
	"orchestrator_capabilities": {},
	"delegate_task":             {"cwd", "driver", "model", "permissionMode", "task", "title", "useWorktree"},
	"list_threads":              {"limit"},
	"read_thread":               {"limit", "maxChars", "threadId", "view"},
	"send_to_thread":            {"message", "threadId"},
	"stop_thread":               {"interruptOnly", "threadId"},
}

// ValidateToolArguments checks for any unrecognized top-level arguments.
func ValidateToolArguments(name string, rawArgs json.RawMessage) error {
	if len(rawArgs) == 0 || string(rawArgs) == "null" || string(rawArgs) == "{}" {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rawArgs, &m); err != nil {
		return fmt.Errorf("invalid arguments JSON: %w", err)
	}

	accepted, ok := toolAcceptedKeys[name]
	if !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	acceptedSet := make(map[string]bool, len(accepted))
	for _, k := range accepted {
		acceptedSet[k] = true
	}

	var unknown []string
	for k := range m {
		if !acceptedSet[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown argument %q; accepted arguments: %s", strings.Join(unknown, ", "), strings.Join(accepted, ", "))
	}
	return nil
}

func toolError(msg string) CallToolResult {
	return CallToolResult{
		Content: []ContentItem{{Type: "text", Text: msg}},
		IsError: true,
	}
}

func toolSuccess(data any) CallToolResult {
	b, err := json.Marshal(data)
	if err != nil {
		return toolError(fmt.Sprintf("failed to serialize response: %v", err))
	}
	return CallToolResult{
		Content: []ContentItem{{Type: "text", Text: string(b)}},
	}
}

// TrimThreadReadResult applies the server-side safety net, ensuring total item text does not exceed maxChars.
func TrimThreadReadResult(res *ThreadReadResult, maxChars int) {
	if res == nil {
		return
	}
	if res.Items == nil {
		res.Items = []ThreadReadItem{}
	}
	remaining := maxChars
	for i := range res.Items {
		runes := []rune(res.Items[i].Text)
		if len(runes) > remaining {
			res.Items[i].Text = string(runes[:remaining])
			res.Truncated = true
			res.Items = res.Items[:i+1]
			return
		}
		remaining -= len(runes)
		if remaining == 0 && i < len(res.Items)-1 {
			res.Truncated = true
			res.Items = res.Items[:i+1]
			return
		}
	}
}

func executeTool(ctx context.Context, backend Backend, scope CallerScope, name string, rawArgs json.RawMessage) CallToolResult {
	if err := ValidateToolArguments(name, rawArgs); err != nil {
		return toolError(err.Error())
	}

	switch name {
	case "orchestrator_capabilities":
		providers, err := backend.ListProviders(ctx)
		if err != nil {
			return toolError(err.Error())
		}
		if providers == nil {
			providers = []ProviderInfo{}
		}
		return toolSuccess(CapabilitiesResult{
			Caller: CallerInfo{
				ThreadID:       scope.ThreadID,
				Driver:         string(scope.Driver),
				Cwd:            scope.Cwd,
				PermissionMode: scope.Permission,
				IsSubagent:     scope.IsSubagent,
			},
			Providers: providers,
		})

	case "delegate_task":
		var args struct {
			Title          string `json:"title"`
			Task           string `json:"task"`
			Driver         string `json:"driver"`
			Model          string `json:"model"`
			Cwd            string `json:"cwd"`
			UseWorktree    bool   `json:"useWorktree"`
			PermissionMode string `json:"permissionMode"`
		}
		if len(rawArgs) > 0 && string(rawArgs) != "null" {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return toolError(fmt.Sprintf("invalid arguments: %v", err))
			}
		}
		if strings.TrimSpace(args.Title) == "" {
			return toolError("title is required")
		}
		if strings.TrimSpace(args.Task) == "" {
			return toolError("task is required")
		}

		driver := args.Driver
		if driver == "" {
			driver = string(scope.Driver)
		}
		switch domain.DriverKind(driver) {
		case domain.DriverClaude, domain.DriverAntigravity, domain.DriverOpenCode, domain.DriverCodex:
		default:
			return toolError(fmt.Sprintf("invalid driver %q", driver))
		}

		cwd := args.Cwd
		if cwd == "" {
			cwd = scope.Cwd
		}

		childPerm, err := ResolveChildPermission(scope.Permission, domain.PermissionMode(args.PermissionMode))
		if err != nil {
			return toolError(err.Error())
		}

		res, err := backend.DelegateTask(ctx, DelegateRequest{
			CallerThreadID: scope.ThreadID,
			Title:          args.Title,
			Prompt:         args.Task,
			Driver:         domain.DriverKind(driver),
			Model:          args.Model,
			Cwd:            cwd,
			UseWorktree:    args.UseWorktree,
			Permission:     childPerm,
		})
		if err != nil {
			return toolError(err.Error())
		}
		return toolSuccess(res)

	case "list_threads":
		var args struct {
			Limit int `json:"limit"`
		}
		if len(rawArgs) > 0 && string(rawArgs) != "null" {
			_ = json.Unmarshal(rawArgs, &args)
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 15
		} else if limit > 50 {
			limit = 50
		}
		threads, err := backend.ListThreads(ctx, limit)
		if err != nil {
			return toolError(err.Error())
		}
		if threads == nil {
			threads = []ThreadSummary{}
		}
		return toolSuccess(threads)

	case "read_thread":
		var args struct {
			ThreadID string `json:"threadId"`
			View     string `json:"view"`
			Limit    int    `json:"limit"`
			MaxChars int    `json:"maxChars"`
		}
		if len(rawArgs) > 0 && string(rawArgs) != "null" {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return toolError(fmt.Sprintf("invalid arguments: %v", err))
			}
		}
		if strings.TrimSpace(args.ThreadID) == "" {
			return toolError("threadId is required")
		}
		view := args.View
		if view == "" {
			view = "messages"
		} else if view != "messages" && view != "activity" {
			return toolError(fmt.Sprintf("invalid view %q: must be 'messages' or 'activity'", view))
		}

		limit := args.Limit
		if limit <= 0 {
			limit = 8
		} else if limit > 40 {
			limit = 40
		}

		maxChars := args.MaxChars
		if maxChars <= 0 {
			maxChars = 6000
		} else if maxChars < 500 {
			maxChars = 500
		} else if maxChars > 16000 {
			maxChars = 16000
		}

		res, err := backend.ReadThread(ctx, args.ThreadID, view, limit, maxChars)
		if err != nil {
			return toolError(err.Error())
		}
		TrimThreadReadResult(&res, maxChars)
		return toolSuccess(res)

	case "send_to_thread":
		var args struct {
			ThreadID string `json:"threadId"`
			Message  string `json:"message"`
		}
		if len(rawArgs) > 0 && string(rawArgs) != "null" {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return toolError(fmt.Sprintf("invalid arguments: %v", err))
			}
		}
		if strings.TrimSpace(args.ThreadID) == "" {
			return toolError("threadId is required")
		}
		if strings.TrimSpace(args.Message) == "" {
			return toolError("message is required")
		}
		if args.ThreadID == scope.ThreadID {
			return toolError("cannot send message to caller's own thread")
		}

		target, found, err := backend.GetThread(ctx, args.ThreadID)
		if err != nil {
			return toolError(err.Error())
		}
		if !found {
			return toolError(fmt.Sprintf("thread %q not found", args.ThreadID))
		}

		if err := CheckTargetPermission(scope.Permission, target.Permission); err != nil {
			return toolError(err.Error())
		}

		if err := backend.SendToThread(ctx, scope.ThreadID, args.ThreadID, args.Message); err != nil {
			return toolError(err.Error())
		}
		return toolSuccess(map[string]bool{"ok": true})

	case "stop_thread":
		var args struct {
			ThreadID      string `json:"threadId"`
			InterruptOnly bool   `json:"interruptOnly"`
		}
		if len(rawArgs) > 0 && string(rawArgs) != "null" {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return toolError(fmt.Sprintf("invalid arguments: %v", err))
			}
		}
		if strings.TrimSpace(args.ThreadID) == "" {
			return toolError("threadId is required")
		}
		if args.ThreadID == scope.ThreadID {
			return toolError("cannot stop caller's own thread")
		}

		target, found, err := backend.GetThread(ctx, args.ThreadID)
		if err != nil {
			return toolError(err.Error())
		}
		if !found {
			return toolError(fmt.Sprintf("thread %q not found", args.ThreadID))
		}

		if err := CheckTargetPermission(scope.Permission, target.Permission); err != nil {
			return toolError(err.Error())
		}

		if err := backend.StopThread(ctx, scope.ThreadID, args.ThreadID, args.InterruptOnly); err != nil {
			return toolError(err.Error())
		}
		return toolSuccess(map[string]bool{"ok": true})

	default:
		return toolError(fmt.Sprintf("unknown tool %q", name))
	}
}
