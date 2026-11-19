package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"composer/internal/domain"
	"composer/internal/mcp"
	"composer/internal/session"
)

type appMCPBackend struct {
	app *App
}

func (b *appMCPBackend) ListProviders(ctx context.Context) ([]mcp.ProviderInfo, error) {
	if b.app == nil || b.app.registry == nil {
		return []mcp.ProviderInfo{}, nil
	}
	snapshots := b.app.registry.Snapshots()
	var out []mcp.ProviderInfo
	for _, s := range snapshots {
		if s.Availability != domain.AvailabilityReady {
			continue
		}
		var models []string
		for _, m := range s.Models {
			id := strings.TrimSpace(m.ID)
			if id != "" {
				models = append(models, id)
			}
		}
		if len(models) == 0 {
			continue
		}
		out = append(out, mcp.ProviderInfo{
			Driver: string(s.Driver),
			Models: models,
		})
	}
	if out == nil {
		out = []mcp.ProviderInfo{}
	}
	return out, nil
}

func (b *appMCPBackend) ListThreads(ctx context.Context, limit int) ([]mcp.ThreadSummary, error) {
	if b.app == nil || b.app.workspaces == nil {
		return []mcp.ThreadSummary{}, nil
	}
	tasks := b.app.workspaces.AllTasks()
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt > tasks[j].CreatedAt
	})
	if limit > 0 && len(tasks) > limit {
		tasks = tasks[:limit]
	}
	out := make([]mcp.ThreadSummary, 0, len(tasks))
	for _, t := range tasks {
		status := string(t.State)
		if b.app.manager != nil {
			if b.app.manager.TurnInProgress(t.ThreadID) {
				status = "running"
			} else if b.app.manager.HasSession(t.ThreadID) {
				status = "idle"
			}
		}
		perm := t.Permission
		if perm == "" {
			perm = domain.PermissionDefault
		}
		driver := string(t.Driver)
		model := t.Model
		cwd := t.Cwd
		if b.app.manager != nil {
			if live, ok := b.app.manager.ThreadSession(t.ThreadID); ok {
				if live.Driver != "" {
					driver = string(live.Driver)
				}
				if live.Model != "" {
					model = live.Model
				}
				if live.Cwd != "" {
					cwd = live.Cwd
				}
			}
		}
		out = append(out, mcp.ThreadSummary{
			ThreadID:   t.ThreadID,
			Title:      t.Title,
			Driver:     driver,
			Model:      model,
			Cwd:        cwd,
			Status:     status,
			Permission: perm,
		})
	}
	return out, nil
}

func (b *appMCPBackend) GetThread(ctx context.Context, threadID string) (mcp.ThreadSummary, bool, error) {
	if b.app == nil || b.app.workspaces == nil {
		return mcp.ThreadSummary{}, false, nil
	}
	task, ok := b.app.workspaces.TaskByThread(threadID)
	if !ok {
		return mcp.ThreadSummary{}, false, nil
	}
	status := string(task.State)
	if b.app.manager != nil {
		if b.app.manager.TurnInProgress(task.ThreadID) {
			status = "running"
		} else if b.app.manager.HasSession(task.ThreadID) {
			status = "idle"
		}
	}
	perm := task.Permission
	if perm == "" {
		perm = domain.PermissionDefault
	}
	driver := string(task.Driver)
	model := task.Model
	cwd := task.Cwd
	if b.app.manager != nil {
		if live, ok := b.app.manager.ThreadSession(task.ThreadID); ok {
			if live.Driver != "" {
				driver = string(live.Driver)
			}
			if live.Model != "" {
				model = live.Model
			}
			if live.Cwd != "" {
				cwd = live.Cwd
			}
		}
	}
	return mcp.ThreadSummary{
		ThreadID:   task.ThreadID,
		Title:      task.Title,
		Driver:     driver,
		Model:      model,
		Cwd:        cwd,
		Status:     status,
		Permission: perm,
	}, true, nil
}

func (b *appMCPBackend) DelegateTask(ctx context.Context, req mcp.DelegateRequest) (mcp.DelegateResult, error) {
	cwd, err := b.app.requireCwd(req.Cwd)
	if err != nil {
		return mcp.DelegateResult{}, err
	}

	model := req.Model
	if model == "" && b.app.registry != nil {
		for _, s := range b.app.registry.Snapshots() {
			if s.Driver == req.Driver && s.Availability == domain.AvailabilityReady {
				if s.Settings.Model != "" {
					model = s.Settings.Model
				} else {
					for _, m := range s.Models {
						if m.Default {
							model = m.ID
							break
						}
					}
					if model == "" && len(s.Models) > 0 {
						model = s.Models[0].ID
					}
				}
				break
			}
		}
	}

	var preamble string
	if mem := b.app.projectMemory(cwd); mem != "" {
		preamble = "Project context (long-lived, applies to all work here):\n" + mem
	}

	perm := req.Permission
	if perm == "" {
		perm = domain.PermissionDefault
	}

	request := session.SpawnRequest{
		Title:          req.Title,
		Prompt:         req.Prompt,
		Driver:         req.Driver,
		Model:          model,
		Cwd:            cwd,
		Permission:     perm,
		IsSubagent:     true,
		Subagent:       true,
		ParentThreadID: req.CallerThreadID,
		Hidden:         true,
		Preamble:       preamble,
	}

	opts := session.SpawnOptions{
		Driver:         req.Driver,
		Model:          model,
		Cwd:            cwd,
		UseWorktree:    req.UseWorktree,
		Permission:     perm,
		Subagent:       true,
		ParentThreadID: req.CallerThreadID,
		Hidden:         true,
	}

	res, err := b.app.spawner.Spawn(ctx, []session.SpawnRequest{request}, opts)
	if err != nil {
		return mcp.DelegateResult{}, err
	}
	if len(res.Tasks) == 0 {
		return mcp.DelegateResult{}, fmt.Errorf("no task was spawned")
	}

	task := res.Tasks[0]
	branch := ""
	taskCwd := cwd
	if task.Worktree != nil {
		branch = task.Worktree.Branch
		if task.Worktree.Path != "" {
			taskCwd = task.Worktree.Path
		}
	}

	if b.app.sessionService != nil {
		_ = b.app.sessionService.CreateSession(ctx, task.ThreadID, task.Title, task.Dir(taskCwd), string(task.Driver), task.Model, branch)
	}

	if req.CallerThreadID != "" && b.app.manager != nil {
		go b.bridgeChildEvents(req.CallerThreadID, task.ThreadID)
	}

	return mcp.DelegateResult{
		ThreadID: task.ThreadID,
		Title:    task.Title,
		Driver:   string(task.Driver),
		Model:    task.Model,
		Cwd:      taskCwd,
		Branch:   branch,
		Status:   string(task.State),
	}, nil
}

func (b *appMCPBackend) bridgeChildEvents(parentThreadID, childThreadID string) {
	if b.app == nil || b.app.manager == nil {
		return
	}
	events, unsubscribe := b.app.manager.Bus().Subscribe()
	defer unsubscribe()

	var lastAssistantText string
	deltaTexts := make(map[string]string)
	startTime := time.Now()

	for event := range events {
		if event.ThreadID != childThreadID {
			continue
		}

		switch event.Kind {
		case domain.EventAgentMessage:
			if event.Text != "" {
				itemID := event.ItemID
				if itemID == "" {
					itemID = event.TurnID + "-msg"
				}
				if event.Delta {
					deltaTexts[itemID] += event.Text
				} else {
					deltaTexts[itemID] = event.Text
				}
				text := deltaTexts[itemID]
				lastAssistantText = text

				b.app.manager.RecordBridgedEvent(domain.RuntimeEvent{
					Kind:         domain.EventSubagentMessage,
					ThreadID:     parentThreadID,
					ParentToolID: childThreadID,
					ItemID:       itemID,
					Text:         text,
					At:           event.At,
				})
			}

		case domain.EventToolCall:
			if event.Tool != nil {
				b.app.manager.RecordBridgedEvent(domain.RuntimeEvent{
					Kind:         domain.EventSubagentToolCall,
					ThreadID:     parentThreadID,
					ParentToolID: childThreadID,
					Tool:         event.Tool,
					At:           event.At,
				})
			}

		case domain.EventToolResult:
			if event.Tool != nil {
				b.app.manager.RecordBridgedEvent(domain.RuntimeEvent{
					Kind:         domain.EventSubagentToolDone,
					ThreadID:     parentThreadID,
					ParentToolID: childThreadID,
					Tool:         event.Tool,
					At:           event.At,
				})
			}

		case domain.EventTurnCompleted:
			dur := time.Since(startTime).Milliseconds()
			finalOutput := strings.TrimSpace(lastAssistantText)
			b.app.manager.RecordBridgedEvent(domain.RuntimeEvent{
				Kind:         domain.EventToolResult,
				ThreadID:     parentThreadID,
				ParentToolID: childThreadID,
				Tool: &domain.ToolCall{
					ID:         childThreadID,
					Status:     domain.ToolCompleted,
					Output:     finalOutput,
					DurationMs: dur,
				},
				At: time.Now().UnixMilli(),
			})
			return

		case domain.EventTurnFailed:
			dur := time.Since(startTime).Milliseconds()
			errText := event.Error
			if errText == "" {
				errText = "Turn failed"
			}
			b.app.manager.RecordBridgedEvent(domain.RuntimeEvent{
				Kind:         domain.EventToolResult,
				ThreadID:     parentThreadID,
				ParentToolID: childThreadID,
				Tool: &domain.ToolCall{
					ID:         childThreadID,
					Status:     domain.ToolFailed,
					Output:     errText,
					DurationMs: dur,
				},
				At: time.Now().UnixMilli(),
			})
			return

		case domain.EventSessionStopped:
			dur := time.Since(startTime).Milliseconds()
			b.app.manager.RecordBridgedEvent(domain.RuntimeEvent{
				Kind:         domain.EventToolResult,
				ThreadID:     parentThreadID,
				ParentToolID: childThreadID,
				Tool: &domain.ToolCall{
					ID:         childThreadID,
					Status:     domain.ToolFailed,
					Output:     "Session stopped",
					DurationMs: dur,
				},
				At: time.Now().UnixMilli(),
			})
			return
		}
	}
}

func (b *appMCPBackend) ReadThread(ctx context.Context, threadID string, view string, limit int, maxChars int) (mcp.ThreadReadResult, error) {
	summary, found, err := b.GetThread(ctx, threadID)
	if err != nil {
		return mcp.ThreadReadResult{}, err
	}
	if !found {
		summary = mcp.ThreadSummary{ThreadID: threadID}
	}

	var events []domain.RuntimeEvent
	if b.app.manager != nil {
		events = b.app.manager.History(threadID)
	}
	if len(events) == 0 && b.app.historyStore != nil && b.app.workspaces != nil {
		if task, ok := b.app.workspaces.TaskByThread(threadID); ok && task.WorkspaceID != "" {
			if sess, err := b.app.historyStore.Load(task.WorkspaceID); err == nil {
				events = sess.Transcripts[threadID]
			}
		}
	}

	items := collapseAndFilterEvents(events, view)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	for i := range items {
		items[i].Index = i
	}

	res := mcp.ThreadReadResult{
		Thread: summary,
		Items:  items,
	}
	if maxChars > 0 {
		mcp.TrimThreadReadResult(&res, maxChars)
	}
	return res, nil
}

func collapseAndFilterEvents(events []domain.RuntimeEvent, view string) []mcp.ThreadReadItem {
	var items []mcp.ThreadReadItem
	toolIndex := make(map[string]int)

	for _, ev := range events {
		switch ev.Kind {
		case domain.EventUserMessage:
			items = append(items, mcp.ThreadReadItem{
				Kind: "user",
				Text: ev.Text,
			})

		case domain.EventAgentMessage:
			n := len(items)
			if n > 0 && items[n-1].Kind == "assistant" {
				items[n-1].Text += ev.Text
			} else {
				items = append(items, mcp.ThreadReadItem{
					Kind: "assistant",
					Text: ev.Text,
				})
			}

		case domain.EventTurnFailed, domain.EventDiagnostic:
			text := ev.Error
			if text == "" {
				text = ev.Text
			}
			if text != "" {
				items = append(items, mcp.ThreadReadItem{
					Kind: "error",
					Text: text,
				})
			}

		case domain.EventToolCall, domain.EventToolResult:
			if view != "activity" || ev.Tool == nil {
				continue
			}
			snippet := ev.Tool.Output
			if snippet == "" && len(ev.Tool.Input) > 0 {
				if b, err := json.Marshal(ev.Tool.Input); err == nil {
					snippet = string(b)
				}
			}
			if snippet == "" {
				snippet = ev.Text
			}
			snippet = truncateSnippet(snippet, 300)

			status := string(ev.Tool.Status)
			if status == "" {
				if ev.Kind == domain.EventToolResult {
					status = "completed"
				} else {
					status = "pending"
				}
			}

			if ev.Tool.ID != "" {
				if idx, exists := toolIndex[ev.Tool.ID]; exists {
					if status != "" {
						items[idx].Status = status
					}
					if snippet != "" {
						items[idx].Text = snippet
					}
					if ev.Tool.Target != "" && items[idx].Target == "" {
						items[idx].Target = ev.Tool.Target
					}
					continue
				}
				toolIndex[ev.Tool.ID] = len(items)
			}

			items = append(items, mcp.ThreadReadItem{
				Kind:   "tool",
				Tool:   ev.Tool.Name,
				Target: ev.Tool.Target,
				Status: status,
				Text:   snippet,
			})
		}
	}

	if items == nil {
		items = []mcp.ThreadReadItem{}
	}
	return items
}

func truncateSnippet(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}

func (b *appMCPBackend) SendToThread(ctx context.Context, callerThreadID, targetThreadID, message string) error {
	turnID := fmt.Sprintf("%s-turn-%d", targetThreadID, time.Now().UnixMilli())
	return b.app.SendTurn(domain.SendTurnInput{
		ThreadID: targetThreadID,
		TurnID:   turnID,
		Text:     message,
	})
}

func (b *appMCPBackend) StopThread(ctx context.Context, callerThreadID, targetThreadID string, interruptOnly bool) error {
	if interruptOnly {
		return b.app.InterruptTurn(targetThreadID)
	}
	return b.app.OrchestratorStopAgent(targetThreadID)
}

type appMCPProvider struct {
	server *mcp.Server
}

func (p *appMCPProvider) Issue(threadID string, driver domain.DriverKind, cwd string, perm domain.PermissionMode, isSubagent bool) (*domain.MCPConfig, error) {
	if p.server == nil || p.server.Registry() == nil {
		return nil, fmt.Errorf("mcp server is not running")
	}
	cred, err := p.server.Registry().Issue(p.server.Endpoint(), mcp.CallerScope{
		ThreadID:   threadID,
		Driver:     driver,
		Cwd:        cwd,
		Permission: perm,
		IsSubagent: isSubagent,
	})
	if err != nil {
		return nil, err
	}
	return &domain.MCPConfig{
		ServerName: "orchestrator",
		Endpoint:   cred.Endpoint,
		AuthHeader: "Bearer " + cred.Token,
	}, nil
}

func (p *appMCPProvider) RevokeThread(threadID string) {
	if p.server != nil && p.server.Registry() != nil {
		p.server.Registry().RevokeThread(threadID)
	}
}
