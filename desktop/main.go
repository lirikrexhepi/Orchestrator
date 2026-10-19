package main

import (
	"embed"
	"os"
	"path/filepath"
	"time"

	"composer/internal/devserver"
	"composer/internal/logger"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if devserver.IsShim(os.Args[0]) {
		os.Exit(devserver.RunShim(os.Args[1:]))
	}

	// Clean up any legacy shim executables so agents never encounter or invoke them.
	binDir := filepath.Join(configRoot(), "bin")
	_ = os.Remove(filepath.Join(binDir, "composer-serve.exe"))
	_ = os.Remove(filepath.Join(binDir, "composer-task.exe"))
	_ = os.Remove(filepath.Join(binDir, "composer-serve"))
	_ = os.Remove(filepath.Join(binDir, "composer-task"))

	logger.Init(
		filepath.Join(configRoot(), "orchestrator_debug.log"),
	)
	logger.Infof("Main", "Starting Orchestrator, PID=%d", os.Getpid())
	captureCrashes(filepath.Join(configRoot(), "crash.log"))

	if wantsHeadless(os.Args) {
		if pid := waitPIDArg(os.Args); pid > 0 {
			waitForExit(pid, time.Minute)
		}
		// Checked before NewApp so a duplicate never opens the database.
		if headlessRunning() {
			logger.Infof("Main", "A headless instance is already running; exiting")
			return
		}
		os.Exit(runHeadless(NewApp()))
	}

	// A PC powered on remotely may already be running headless. It owns the
	// gateway port, history and database, so it hands over before NewApp
	// opens any of them.
	if !bindingsBuild {
		if !claimWindow() {
			logger.Infof("Main", "The desktop window is already open; focusing it")
			return
		}
		takeOverFromHeadless()
	}

	// Create an instance of the app structure
	traceStartup("new app")
	app := NewApp()
	traceStartup("app built")

	// Check if user disabled GPU acceleration in settings.
	// Defaults to false (GPU enabled), allowing users experiencing display driver crashes to toggle software rendering.
	gpuPref, _ := app.GetUserPreference("disable_gpu_acceleration")
	disableGpu := gpuPref == "true"
	if disableGpu {
		logger.Infof("Main", "WebView2 GPU Hardware Acceleration: DISABLED by user preference")
	} else {
		logger.Infof("Main", "WebView2 GPU Hardware Acceleration: ENABLED (default)")
	}

	startState := options.Normal
	if pref, _ := app.GetUserPreference("launch_fullscreen"); pref == "true" {
		startState = options.Maximised
	}

	// Create application with options
	err := wails.Run(&options.App{
		Title:            "Orchestrator",
		WindowStartState: startState,
		Width:            1440,
		Height:           900,
		MinWidth:         900,
		MinHeight:        600,
		// The wallpaper is the interface here, so the OS title bar is dropped and
		// the scene runs edge to edge. The app draws its own drag strip and window
		// controls, since a frameless window has neither.
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnShutdown:       app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "orchestrator-desktop-6f0c2b1e" + profileSuffix(),
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if app.ctx == nil {
					return
				}
				runtime.WindowUnminimise(app.ctx)
				runtime.WindowShow(app.ctx)
			},
		},
		Debug: options.Debug{
			OpenInspectorOnStartup: false,
		},
		Windows: &windows.Options{
			// WebView2 repaints the whole surface on every resize step. Debouncing keeps
			// a drag from queueing more full-window repaints than the compositor can
			// retire, which is what makes resizing feel like it drops frames.
			ResizeDebounceMS: 16,

			// WebviewGpuIsDisabled controls WebView2 GPU hardware acceleration.
			// When false, GPU hardware acceleration is active.
			// When true, software rasterization is used.
			WebviewGpuIsDisabled: disableGpu,

			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.Dark,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		logger.Errorf("Main", "wails.Run exited with error: %v", err)
	} else {
		logger.Infof("Main", "wails.Run exited cleanly")
	}
}
