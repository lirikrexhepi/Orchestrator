package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"composer/internal/logger"
	"composer/internal/updater"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	appUpdateAvailableChannel = "app:update-available"
	appUpdateProgressChannel  = "app:update-progress"

	appUpdateCacheTTL      = 30 * time.Minute
	appUpdateCheckInterval = 6 * time.Hour
)

type appUpdaterState struct {
	mu            sync.Mutex
	client        *updater.Client
	lastStatus    updater.Status
	checkedAt     time.Time
	installing    bool
	exitingUpdate bool
}

func newAppUpdater() *appUpdaterState {
	return &appUpdaterState{
		client: updater.NewClient(AppVersion),
	}
}

func (a *App) updatingForInstall() bool {
	if a.appUpdater == nil {
		return false
	}
	a.appUpdater.mu.Lock()
	defer a.appUpdater.mu.Unlock()
	return a.appUpdater.exitingUpdate
}

// GetAppVersion returns the build version string (e.g. "1.0.0" or "dev").
func (a *App) GetAppVersion() string {
	return AppVersion
}

// CheckAppUpdate returns the cached update status when recent, or checks GitHub Releases.
func (a *App) CheckAppUpdate() updater.Status {
	return a.checkAppUpdate(false)
}

// CheckAppUpdateNow forces an immediate check against GitHub Releases.
func (a *App) CheckAppUpdateNow() updater.Status {
	return a.checkAppUpdate(true)
}

func (a *App) checkAppUpdate(force bool) updater.Status {
	if a.appUpdater == nil {
		a.appUpdater = newAppUpdater()
	}

	a.appUpdater.mu.Lock()
	if !force && !a.appUpdater.checkedAt.IsZero() && time.Since(a.appUpdater.checkedAt) < appUpdateCacheTTL {
		cached := a.appUpdater.lastStatus
		a.appUpdater.mu.Unlock()
		return cached
	}
	client := a.appUpdater.client
	a.appUpdater.mu.Unlock()

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	status := client.Check(ctx)

	a.appUpdater.mu.Lock()
	a.appUpdater.lastStatus = status
	a.appUpdater.checkedAt = time.Now()
	a.appUpdater.mu.Unlock()

	if status.Available {
		a.emit(appUpdateAvailableChannel, status)
	}
	return status
}

// InstallAppUpdate downloads, verifies, and launches the silent Windows installer
// for the latest release, then exits the running Orchestrator instance cleanly.
func (a *App) InstallAppUpdate() error {
	if a.appUpdater == nil {
		a.appUpdater = newAppUpdater()
	}

	a.appUpdater.mu.Lock()
	if a.appUpdater.installing {
		a.appUpdater.mu.Unlock()
		return errors.New("an update is already in progress")
	}
	status := a.appUpdater.lastStatus
	client := a.appUpdater.client
	a.appUpdater.installing = true
	a.appUpdater.mu.Unlock()

	defer func() {
		a.appUpdater.mu.Lock()
		a.appUpdater.installing = false
		a.appUpdater.mu.Unlock()
	}()

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if status.DownloadURL == "" || !status.Available {
		status = client.Check(ctx)
		a.appUpdater.mu.Lock()
		a.appUpdater.lastStatus = status
		a.appUpdater.checkedAt = time.Now()
		a.appUpdater.mu.Unlock()
	}

	if status.IsDevBuild {
		return updater.ErrDevBuild
	}
	if !status.Supported {
		return updater.ErrUnsupportedOS
	}
	if !status.Available || status.DownloadURL == "" {
		return updater.ErrNoUpdate
	}

	installerPath, err := client.DownloadAndVerify(ctx, status, func(p updater.Progress) {
		a.emit(appUpdateProgressChannel, p)
	})
	if err != nil {
		a.emit(appUpdateProgressChannel, updater.Progress{Stage: "error", Error: err.Error()})
		return err
	}

	installDir := ""
	if exe, exeErr := os.Executable(); exeErr == nil && exe != "" {
		dir := filepath.Dir(exe)
		// Only pin /D= when running from an actual installed directory (contains
		// uninstall.exe); otherwise let NSIS install to its registered/default path.
		if _, statErr := os.Stat(filepath.Join(dir, "uninstall.exe")); statErr == nil {
			installDir = dir
		}
	}

	a.emit(appUpdateProgressChannel, updater.Progress{Stage: "installing", Percent: 100})
	logger.Infof("Updater", "Launching silent update installer %s (target=%s)", installerPath, installDir)

	if err := updater.LaunchInstaller(installerPath, installDir); err != nil {
		a.emit(appUpdateProgressChannel, updater.Progress{Stage: "error", Error: err.Error()})
		return err
	}

	// Mark exitingUpdate so shutdown() skips background phone-gateway handoff
	// and releases Orchestrator.exe cleanly for the installer.
	a.appUpdater.mu.Lock()
	a.appUpdater.exitingUpdate = true
	a.appUpdater.mu.Unlock()

	// Let the RPC response reach the frontend before quitting so the installer
	// can acquire the executable lock immediately and relaunch cleanly.
	if !a.headless && a.ctx != nil {
		quitCtx := a.ctx
		go func() {
			time.Sleep(250 * time.Millisecond)
			runtime.Quit(quitCtx)
		}()
	}
	return nil
}

func (a *App) watchAppUpdates(ctx context.Context) {
	updater.CleanupOldInstallers()
	if a.headless {
		return
	}
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return
	}
	a.checkAppUpdate(false)

	ticker := time.NewTicker(appUpdateCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.checkAppUpdate(true)
		case <-ctx.Done():
			return
		}
	}
}
