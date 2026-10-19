package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"composer/internal/devserver"
	"composer/internal/remote"
	"composer/internal/shell"
)

var errNoDevScript = remote.ErrNoDevScript

var devScriptNames = []string{"dev", "start", "serve"}

var webFrameworks = []string{
	"vite", "next", "nuxt", "astro", "@sveltejs/kit", "react-scripts", "@remix-run/dev", "gatsby",
	"webpack-dev-server", "parcel", "@angular/cli", "@vue/cli-service", "solid-start", "@solidjs/start",
}

var frontendFolders = []string{"web", "frontend", "client", "site", "app", "www", "ui"}

var nestedParents = []string{"apps", "packages", "sites"}

var skippedFolders = map[string]bool{"node_modules": true, "dist": true, "build": true, "vendor": true, "out": true, "coverage": true}

type devScript struct {
	manager string
	script  string
	dir     string
}

type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func detectDevScript(cwd string) (devScript, error) {
	pkg, err := readPackage(cwd)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return devScript{}, err
	}
	if err == nil {
		if script := pickScript(pkg); script != "" {
			return devScript{manager: packageManager(cwd, cwd), script: script, dir: cwd}, nil
		}
	}
	dir, script := nestedDevScript(cwd)
	if dir == "" {
		return devScript{}, errNoDevScript
	}
	return devScript{manager: packageManager(dir, cwd), script: script, dir: dir}, nil
}

func readPackage(dir string) (packageJSON, error) {
	var pkg packageJSON
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return pkg, err
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return pkg, fmt.Errorf("package.json could not be read: %w", err)
	}
	return pkg, nil
}

func pickScript(pkg packageJSON) string {
	for _, name := range devScriptNames {
		if strings.TrimSpace(pkg.Scripts[name]) != "" {
			return name
		}
	}
	return ""
}

func nestedDevScript(root string) (string, string) {
	bestDir, bestScript, bestScore := "", "", 0
	consider := func(dir string, bonus int) {
		pkg, err := readPackage(dir)
		if err != nil {
			return
		}
		script := pickScript(pkg)
		if script == "" {
			return
		}
		score := 1 + bonus
		if script == "dev" {
			score += 2
		}
		for _, dep := range webFrameworks {
			if pkg.Dependencies[dep] != "" || pkg.DevDependencies[dep] != "" {
				score += 4
				break
			}
		}
		name := strings.ToLower(filepath.Base(dir))
		for _, preferred := range frontendFolders {
			if name == preferred {
				score++
				break
			}
		}
		if score > bestScore {
			bestDir, bestScript, bestScore = dir, script, score
		}
	}
	for _, dir := range childFolders(root) {
		consider(dir, 0)
	}
	for _, parent := range nestedParents {
		for _, dir := range childFolders(filepath.Join(root, parent)) {
			consider(dir, 1)
		}
	}
	return bestDir, bestScript
}

func childFolders(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") || skippedFolders[strings.ToLower(name)] {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out
}

func packageManager(dir, root string) string {
	lockfiles := []struct{ file, manager string }{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"bun.lockb", "bun"},
		{"bun.lock", "bun"},
		{"package-lock.json", "npm"},
	}
	env := shell.BaseEnvironment()
	for current := dir; ; current = filepath.Dir(current) {
		for _, l := range lockfiles {
			if _, err := os.Stat(filepath.Join(current, l.file)); err != nil {
				continue
			}
			if _, ok := shell.LookPath(l.manager, env); ok {
				return l.manager
			}
		}
		if samePath(current, root) || filepath.Dir(current) == current || !underDir(current, root) {
			return "npm"
		}
	}
}

func devServerLabel(root, dir string) string {
	name := filepath.Base(root)
	if rel, err := filepath.Rel(root, dir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return name + "/" + filepath.ToSlash(rel)
	}
	return name
}

func (a *App) threadCwd(threadID string) string {
	task, ok := a.workspaces.TaskByThread(threadID)
	if !ok {
		return ""
	}
	workspaceCwd := ""
	if ws := a.workspaces.Get(task.WorkspaceID); ws != nil {
		workspaceCwd = ws.Cwd
	}
	return task.Dir(workspaceCwd)
}

func (a *App) managedSnapshot(id string) (devserver.Snapshot, bool) {
	for _, snap := range a.devservers.List() {
		if snap.ID == id {
			return snap, true
		}
	}
	return devserver.Snapshot{}, false
}

func (a *App) runningDevServerIn(cwd string) (devserver.Snapshot, bool) {
	for _, snap := range a.devservers.List() {
		if snap.Status == devserver.StatusRunning && underDir(snap.Cwd, cwd) && !inNestedWorktree(snap.Cwd, cwd) {
			return snap, true
		}
	}
	return devserver.Snapshot{}, false
}

func inNestedWorktree(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.EqualFold(part, "worktrees") || strings.EqualFold(part, ".worktrees") {
			return true
		}
	}
	return false
}

func (a *App) remoteStartDevServer(_ context.Context, req remote.DevServerRequest) (remote.DevServerInfo, error) {
	cwd := req.Cwd
	if req.ThreadID != "" && !req.Pinned {
		if found := a.threadCwd(req.ThreadID); found != "" {
			cwd = found
		}
	}
	cwd, err := a.requireCwd(cwd)
	if err != nil {
		return remote.DevServerInfo{}, err
	}

	if snap, ok := a.runningDevServerIn(cwd); ok {
		a.queueDevServerNote(req.ThreadID, snap.ID)
		return devServerInfo(snap, nil), nil
	}

	script, err := detectDevScript(cwd)
	if err != nil {
		return remote.DevServerInfo{}, err
	}
	snap, err := a.devservers.Start(devserver.Spec{
		Label:         devServerLabel(cwd, script.dir),
		Command:       script.manager,
		Args:          []string{"run", script.script},
		Cwd:           script.dir,
		Env:           map[string]string{"BROWSER": "none"},
		OwnerThreadID: req.ThreadID,
	})
	if err != nil {
		return remote.DevServerInfo{}, err
	}
	a.queueDevServerNote(req.ThreadID, snap.ID)
	go a.warmPreviewWhenListening(snap.ID)
	return devServerInfo(snap, nil), nil
}

func (a *App) remoteDevServerStatus(id string) (remote.DevServerInfo, bool) {
	snap, ok := a.managedSnapshot(id)
	if !ok {
		return remote.DevServerInfo{}, false
	}
	var log []string
	if snap.Status != devserver.StatusRunning {
		log = tail(a.devservers.Logs(id), 6)
	}
	return devServerInfo(snap, log), true
}

func (a *App) warmPreviewWhenListening(id string) {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		snap, ok := a.managedSnapshot(id)
		if !ok || snap.Status != devserver.StatusRunning {
			return
		}
		if snap.Port > 0 {
			if a.remoteServer != nil {
				a.remoteServer.WarmPreview(snap.Port)
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (a *App) queueDevServerNote(threadID, id string) {
	if threadID == "" {
		return
	}
	a.manager.QueueTurnNote(threadID, func() string {
		snap, ok := a.managedSnapshot(id)
		if !ok || snap.Status != devserver.StatusRunning {
			return ""
		}
		where := snap.URL
		if where == "" {
			where = "its usual local port"
		}
		return fmt.Sprintf("[Note from the Orchestrator app, not the user: the app already started this project's dev server with `%s` in %s and it is running at %s. Use that server. Do not start, restart or stop dev servers unless the user asks.]", snap.Command, snap.Cwd, where)
	})
}

func devServerInfo(snap devserver.Snapshot, log []string) remote.DevServerInfo {
	return remote.DevServerInfo{
		ID:      snap.ID,
		Name:    snap.Label,
		Command: snap.Command,
		Cwd:     snap.Cwd,
		Port:    snap.Port,
		Status:  string(snap.Status),
		Log:     log,
	}
}

func tail(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}
