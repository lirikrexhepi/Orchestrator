package browser

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ErrChromiumNotFound is returned when no supported Chromium browser is located.
var ErrChromiumNotFound = errors.New("no Chromium-based browser found (checked Chrome, Edge, Brave); install Chrome or Edge, or set ORCHESTRATOR_BROWSER_PATH")

// FindChromium locates an installed Chromium-compatible executable.
// The search order is:
//  1. ORCHESTRATOR_BROWSER_PATH environment variable (if set and points to an existing file)
//  2. Platform-specific known installation directories for Chrome, Edge, Brave, and Chromium
//  3. exec.LookPath search across system PATH
func FindChromium() (string, error) {
	// 1. Explicit override
	if envPath := os.Getenv("ORCHESTRATOR_BROWSER_PATH"); envPath != "" {
		if fileExists(envPath) {
			return envPath, nil
		}
		return "", fmt.Errorf("ORCHESTRATOR_BROWSER_PATH is set to %q but file does not exist", envPath)
	}

	// 2. Known standard paths per OS
	for _, candidate := range candidatePaths() {
		if fileExists(candidate) {
			return candidate, nil
		}
	}

	// 3. Fallback to PATH lookups
	for _, name := range candidateNames() {
		if p, err := exec.LookPath(name); err == nil && fileExists(p) {
			return p, nil
		}
	}

	return "", ErrChromiumNotFound
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func candidatePaths() []string {
	var paths []string

	switch runtime.GOOS {
	case "windows":
		roots := []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("LocalAppData"),
		}
		subPaths := []string{
			filepath.Join("Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join("BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
			filepath.Join("Chromium", "Application", "chrome.exe"),
		}
		for _, root := range roots {
			if root == "" {
				continue
			}
			for _, sub := range subPaths {
				paths = append(paths, filepath.Join(root, sub))
			}
		}

	case "darwin":
		home, _ := os.UserHomeDir()
		appDirs := []string{"/Applications"}
		if home != "" {
			appDirs = append(appDirs, filepath.Join(home, "Applications"))
		}
		macApps := []string{
			"Google Chrome.app/Contents/MacOS/Google Chrome",
			"Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"Brave Browser.app/Contents/MacOS/Brave Browser",
			"Chromium.app/Contents/MacOS/Chromium",
		}
		for _, dir := range appDirs {
			for _, app := range macApps {
				paths = append(paths, filepath.Join(dir, app))
			}
		}

	default: // Linux / BSD
		paths = append(paths,
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/bin/microsoft-edge",
			"/usr/bin/microsoft-edge-stable",
			"/usr/bin/brave-browser",
			"/snap/bin/chromium",
		)
	}

	return paths
}

func candidateNames() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"chrome.exe", "msedge.exe", "brave.exe", "chromium.exe"}
	case "darwin":
		return []string{"google-chrome", "chromium", "brave", "microsoft-edge"}
	default:
		return []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
			"microsoft-edge",
			"microsoft-edge-stable",
			"brave-browser",
		}
	}
}
