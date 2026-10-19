package activity

import (
	"composer/internal/history"
)

type HistorySource interface {
	List() ([]history.Meta, error)
	Load(workspaceID string) (history.Session, error)
}

func Backfill(source HistorySource, before int64) ([]Row, error) {
	metas, err := source.List()
	if err != nil {
		return nil, err
	}
	acc := newAccumulator()
	for _, meta := range metas {
		if meta.Workspace.ImportedFrom != "" {
			continue
		}
		loaded, err := source.Load(meta.Workspace.ID)
		if err != nil {
			continue
		}
		for _, task := range loaded.Meta.Tasks {
			acc.seed(task.ThreadID, Thread{Project: meta.Workspace.Cwd, Driver: string(task.Driver), Model: task.Model})
		}
		for threadID, events := range loaded.Transcripts {
			acc.seed(threadID, Thread{Project: meta.Workspace.Cwd})
			for _, event := range events {
				if event.At <= 0 || event.At >= before || !counted(event.Kind) {
					continue
				}
				if event.ThreadID == "" {
					event.ThreadID = threadID
				}
				acc.observe(event)
			}
		}
	}
	return acc.drain(), nil
}
