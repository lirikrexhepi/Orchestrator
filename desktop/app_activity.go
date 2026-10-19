package main

import (
	"context"
	"errors"
	"time"

	"composer/internal/activity"
	"composer/internal/logger"
)

const activityBackfillKey = "activity.backfilled.v1"

func (a *App) activityThread(threadID string) activity.Thread {
	var info activity.Thread
	if a.recorder != nil {
		info.Project = a.recorder.WorkspaceCwd(threadID)
	}
	for _, s := range a.manager.Sessions() {
		if s.ThreadID != threadID {
			continue
		}
		if info.Project == "" {
			info.Project = s.Cwd
		}
		info.Driver = string(s.Driver)
		info.Model = s.Model
		break
	}
	return info
}

func (a *App) startActivity() {
	if a.activity == nil {
		return
	}
	a.activity.Run(5 * time.Second)
	if a.prefRepo == nil || a.historyStore == nil {
		return
	}
	cutoff := a.activity.Since()
	go func() {
		ctx := context.Background()
		if done, err := a.prefRepo.Get(ctx, activityBackfillKey); err != nil || done != "" {
			return
		}
		rows, err := activity.Backfill(a.historyStore, cutoff)
		if err != nil {
			logger.Infof("Activity", "backfill skipped: %v", err)
			return
		}
		if err := a.activityRepo.AddActivity(ctx, rows); err != nil {
			logger.Infof("Activity", "backfill write failed: %v", err)
			return
		}
		_ = a.prefRepo.Set(ctx, activityBackfillKey, time.Now().Format(time.RFC3339))
	}()
}

func (a *App) ActivityOverview(period, project string) (activity.Overview, error) {
	if a.activityRepo == nil {
		return activity.Overview{}, errors.New("activity storage is unavailable")
	}
	ctx := context.Background()
	_ = a.activity.Flush(ctx)
	rows, err := a.activityRepo.Rows(ctx)
	if err != nil {
		return activity.Overview{}, err
	}
	return activity.Build(rows, time.Now(), period, project), nil
}
