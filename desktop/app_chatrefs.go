package main

import (
	"fmt"
	"sort"
	"strings"

	"composer/internal/domain"
	"composer/internal/session"
)

const (
	chatRefPrefix   = "chat://"
	chatRefMaxChars = 12000
)

func (a *App) expandChatRefs(files []domain.FileRef) (string, []domain.FileRef) {
	var blocks []string
	rest := make([]domain.FileRef, 0, len(files))
	for _, file := range files {
		if !strings.HasPrefix(file.Path, chatRefPrefix) {
			rest = append(rest, file)
			continue
		}
		if block := a.chatRefContext(strings.TrimPrefix(file.Path, chatRefPrefix)); block != "" {
			blocks = append(blocks, block)
		}
	}
	return strings.Join(blocks, "\n\n"), rest
}

func (a *App) chatRefContext(ref string) string {
	ref, _, _ = strings.Cut(ref, "?")
	workspaceID, threadID, _ := strings.Cut(ref, "/")
	if workspaceID == "" {
		return ""
	}

	title := ""
	cwd := ""
	if a.workspaces != nil {
		if threadID != "" {
			if task, ok := a.workspaces.TaskByThread(threadID); ok && task != nil {
				title = task.Title
				cwd = a.liveTaskCwd(task)
			}
		}
		if ws := a.workspaces.Get(workspaceID); ws != nil {
			if title == "" {
				title = ws.Title
			}
			if cwd == "" {
				cwd = ws.Cwd
			}
		}
	}

	var live []domain.RuntimeEvent
	if threadID != "" && a.manager != nil {
		live = a.manager.History(threadID)
	}

	var disk []domain.RuntimeEvent
	if a.historyStore != nil {
		if loaded, err := a.historyStore.Load(workspaceID); err == nil {
			if title == "" {
				title = loaded.Meta.Workspace.Title
				for _, task := range loaded.Meta.Tasks {
					if task.ThreadID == threadID && task.Title != "" {
						title = task.Title
						break
					}
				}
			}
			if cwd == "" {
				cwd = loaded.Meta.Workspace.Cwd
			}
			for id, transcript := range loaded.Transcripts {
				if threadID == "" || id == threadID || strings.HasPrefix(id, threadID+"-cont-") {
					disk = append(disk, transcript...)
				}
			}
		}
	}

	if len(live) == 0 && len(disk) == 0 && title == "" && cwd == "" {
		return fmt.Sprintf("<referenced_chat id=%q>\nThis chat could not be found.\n</referenced_chat>", ref)
	}

	var events []domain.RuntimeEvent
	if len(live) > 0 && len(disk) == 0 {
		events = append(events, live...)
	} else if len(disk) > 0 && len(live) == 0 {
		events = append(events, disk...)
	} else if len(live) > 0 && len(disk) > 0 {
		type seqKey struct {
			threadID string
			seq      uint64
		}
		seen := make(map[seqKey]bool)
		for _, e := range live {
			if e.Seq > 0 {
				seen[seqKey{e.ThreadID, e.Seq}] = true
			}
			events = append(events, e)
		}
		for _, e := range disk {
			if e.Seq > 0 && seen[seqKey{e.ThreadID, e.Seq}] {
				continue
			}
			events = append(events, e)
		}
	}

	sort.SliceStable(events, func(i, j int) bool {
		if events[i].At != events[j].At {
			return events[i].At < events[j].At
		}
		return events[i].Seq < events[j].Seq
	})

	text, omitted := session.ExtractConversationWithMeta(events, chatRefMaxChars)
	if text == "" {
		text = "(no messages)"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<referenced_chat threadId=%q title=%q cwd=%q>\n", threadID, title, cwd))
	b.WriteString("The user attached this earlier chat for context (newest last). Treat this as background reference only.\n")
	b.WriteString(text)
	if omitted && threadID != "" {
		b.WriteString(fmt.Sprintf("\n[If you need earlier turns or tool activity from this chat, call read_thread with threadId=%q.]", threadID))
	}
	b.WriteString("\n</referenced_chat>")
	return b.String()
}
