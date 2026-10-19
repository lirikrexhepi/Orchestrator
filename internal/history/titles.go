package history

import (
	"errors"
	"strings"
)

const (
	TitleFromAI   = "ai"
	TitleFromUser = "user"
)

type TitleState struct {
	WorkspaceID string
	ThreadID    string
	Title       string
	Source      string
	Prompt      string
	TitleTurn   int
	UserTurns   int
	Agents      int
}

func rootTaskIndex(meta *Meta, root string) int {
	match := -1
	for i, task := range meta.Tasks {
		if task.ThreadID == root {
			return i
		}
		if match < 0 && RootThreadID(task.ThreadID) == root {
			match = i
		}
	}
	return match
}

func agentCount(meta *Meta) int {
	count := 0
	for _, task := range meta.Tasks {
		if RootThreadID(task.ThreadID) == task.ThreadID {
			count++
		}
	}
	return count
}

func titleState(meta *Meta, index int, root string) TitleState {
	task := meta.Tasks[index]
	return TitleState{
		WorkspaceID: meta.Workspace.ID,
		ThreadID:    root,
		Title:       task.Title,
		Source:      task.TitleSource,
		Prompt:      firstNonBlank(task.Prompt, meta.Workspace.Prompt),
		TitleTurn:   task.TitleTurn,
		UserTurns:   task.UserTurns,
		Agents:      agentCount(meta),
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (r *Recorder) NoteUserTurn(threadID string) (TitleState, bool) {
	root := RootThreadID(threadID)
	r.mu.Lock()
	workspaceID, ok := r.workspaceOf[threadID]
	if !ok {
		workspaceID, ok = r.workspaceOf[root]
	}
	if !ok {
		r.mu.Unlock()
		return TitleState{}, false
	}
	meta := r.metaFor(workspaceID)
	index := rootTaskIndex(meta, root)
	if index < 0 {
		r.mu.Unlock()
		return TitleState{}, false
	}
	meta.Tasks[index].UserTurns++
	r.dirty[workspaceID] = true
	state := titleState(meta, index, root)
	r.mu.Unlock()
	return state, true
}

func (r *Recorder) findWorkspace(threadID string) (string, bool) {
	root := RootThreadID(threadID)
	r.mu.RLock()
	workspaceID, ok := r.workspaceOf[threadID]
	if !ok {
		workspaceID, ok = r.workspaceOf[root]
	}
	r.mu.RUnlock()
	if ok {
		return workspaceID, true
	}
	metas, err := r.store.List()
	if err != nil {
		return "", false
	}
	for _, meta := range metas {
		for _, task := range meta.Tasks {
			if task.ThreadID == threadID || RootThreadID(task.ThreadID) == root {
				return meta.Workspace.ID, true
			}
		}
	}
	return "", false
}

func (r *Recorder) SetTitle(threadID, title, source string, turn int) (TitleState, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return TitleState{}, errors.New("title is empty")
	}
	workspaceID, ok := r.findWorkspace(threadID)
	if !ok {
		return TitleState{}, errors.New("chat not found")
	}
	root := RootThreadID(threadID)

	r.mu.Lock()
	meta := r.metaFor(workspaceID)
	index := rootTaskIndex(meta, root)
	if index < 0 {
		r.mu.Unlock()
		return TitleState{}, errors.New("chat not found")
	}
	if source == TitleFromAI && meta.Tasks[index].TitleSource == TitleFromUser {
		state := titleState(meta, index, root)
		r.mu.Unlock()
		return state, errors.New("chat was renamed by the user")
	}
	for i := range meta.Tasks {
		if meta.Tasks[i].ThreadID != root && RootThreadID(meta.Tasks[i].ThreadID) != root {
			continue
		}
		meta.Tasks[i].Title = title
		meta.Tasks[i].TitleSource = source
		if turn > 0 {
			meta.Tasks[i].TitleTurn = turn
		}
	}
	if agentCount(meta) <= 1 {
		meta.Workspace.Title = title
	}
	state := titleState(meta, index, root)
	snapshot := *meta
	r.mu.Unlock()

	if err := r.store.SaveMeta(snapshot); err != nil {
		return state, err
	}
	r.notifyChanged()
	return state, nil
}
