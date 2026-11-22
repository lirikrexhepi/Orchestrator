package main

import (
	"context"
	"errors"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"time"

	"composer/internal/activity"
	"composer/internal/logger"
)

const (
	activityBackfillKey   = "activity.backfilled.v1"
	activityBackfillV2Key = "activity.backfilled.v2"
)

var (
	greetingNameOnce sync.Once
	greetingNameVal  string
)

func resolveGreetingName() string {
	greetingNameOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "git", "config", "--global", "user.name").Output(); err == nil {
			if name := firstDisplayName(string(out)); name != "" {
				greetingNameVal = name
				return
			}
		}
		if u, err := user.Current(); err == nil {
			if name := firstDisplayName(u.Name); name != "" {
				greetingNameVal = name
				return
			}
			raw := u.Username
			if idx := strings.LastIndexAny(raw, `\/`); idx >= 0 {
				raw = raw[idx+1:]
			}
			if name := firstDisplayName(raw); name != "" {
				greetingNameVal = name
			}
		}
	})
	return greetingNameVal
}

func firstDisplayName(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return ""
	}
	first := fields[0]
	switch strings.ToLower(first) {
	case "pc", "user", "admin", "administrator", "root", "owner", "default":
		return ""
	}
	return first
}

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
		v1Done, err := a.prefRepo.Get(ctx, activityBackfillKey)
		if err != nil {
			return
		}
		if v1Done == "" {
			rows, err := activity.Backfill(a.historyStore, cutoff)
			if err != nil {
				logger.Infof("Activity", "backfill skipped: %v", err)
				return
			}
			if err := a.activityRepo.AddActivity(ctx, rows); err != nil {
				logger.Infof("Activity", "backfill write failed: %v", err)
				return
			}
			now := time.Now().Format(time.RFC3339)
			_ = a.prefRepo.Set(ctx, activityBackfillKey, now)
			_ = a.prefRepo.Set(ctx, activityBackfillV2Key, now)
			return
		}
		v2Done, err := a.prefRepo.Get(ctx, activityBackfillV2Key)
		if err != nil || v2Done != "" {
			return
		}
		rows, err := activity.Backfill(a.historyStore, cutoff)
		if err != nil {
			logger.Infof("Activity", "backfill v2 skipped: %v", err)
			return
		}
		if err := a.activityRepo.BackfillAgentsAndWorking(ctx, rows); err != nil {
			logger.Infof("Activity", "backfill v2 write failed: %v", err)
			return
		}
		_ = a.prefRepo.Set(ctx, activityBackfillV2Key, time.Now().Format(time.RFC3339))
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
	overview := activity.Build(rows, time.Now(), period, project)
	overview.UserName = resolveGreetingName()
	return overview, nil
}
