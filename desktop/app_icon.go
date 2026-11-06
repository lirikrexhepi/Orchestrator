package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"composer/internal/logger"
)

//go:embed assets/logo/sized/*.png
var appIconFiles embed.FS

//go:embed assets/logo/ico/*.ico
var appIconIcoFiles embed.FS

//go:embed assets/logo/app-icon-*.png
var appIconFullFiles embed.FS

const (
	appIconPreference = "app_icon"
	defaultAppIcon    = "blue-base"
)

var appIconIDs = []string{
	"blue-simplistic", "blue-base", "blue-complex",
	"purple-simplistic", "purple-base", "purple-complex",
}

func (a *App) GetAppIcon() string {
	id, err := a.GetUserPreference(appIconPreference)
	if err != nil || !slices.Contains(appIconIDs, id) {
		return defaultAppIcon
	}
	return id
}

func (a *App) SetAppIcon(id string) error {
	if !slices.Contains(appIconIDs, id) {
		return fmt.Errorf("unknown app icon %q", id)
	}
	if err := a.SetUserPreference(appIconPreference, id); err != nil {
		return err
	}
	applyErr := applyAppIcon(id)
	publishAppIcon(id)
	return applyErr
}

func (a *App) syncAppIcon() {
	id := a.GetAppIcon()
	if err := applyAppIcon(id); err != nil {
		logger.Warnf("App", "Could not apply app icon %s: %v", id, err)
	}
	publishAppIcon(id)
}

func applyAppIcon(id string) error {
	small, err := appIconFiles.ReadFile("assets/logo/sized/" + id + "-32.png")
	if err != nil {
		return err
	}
	big, err := appIconFiles.ReadFile("assets/logo/sized/" + id + "-256.png")
	if err != nil {
		return err
	}
	return setWindowIcon(small, big)
}

const (
	appIconStageDir = "icons"
	stagedIcoName   = "app.ico"
	stagedFullName  = "app-512.png"
)

func publishAppIcon(id string) {
	ico, err := appIconIcoFiles.ReadFile("assets/logo/ico/" + id + ".ico")
	if err != nil {
		logger.Warnf("App", "App icon %s missing staged ico: %v", id, err)
		return
	}
	full, err := appIconFullFiles.ReadFile("assets/logo/app-icon-" + id + ".png")
	if err != nil {
		logger.Warnf("App", "App icon %s missing full-size png: %v", id, err)
		return
	}
	dir := filepath.Join(configRoot(), appIconStageDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warnf("App", "Could not create icon stage dir: %v", err)
		return
	}
	icoPath := filepath.Join(dir, stagedIcoName)
	if err := writeFileAtomicRename(icoPath, ico, 0o644); err != nil {
		logger.Warnf("App", "Could not stage app icon: %v", err)
		return
	}
	if err := writeFileAtomicRename(filepath.Join(dir, stagedFullName), full, 0o644); err != nil {
		logger.Warnf("App", "Could not stage full-size app icon: %v", err)
		return
	}
	if err := retargetAppShortcuts(icoPath); err != nil {
		logger.Warnf("App", "Could not retarget app shortcuts: %v", err)
	}
	notifyShellIconChanged()
}

func writeFileAtomicRename(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-icon-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
