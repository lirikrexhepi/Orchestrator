# Perf harness

Drives a real, isolated instance of the desktop app over the Chrome DevTools Protocol.

- `ORCHESTRATOR_PROFILE=perf` makes the app use `%APPDATA%\composer-perf` (a copy of real data) and its own single-instance locks, so it runs beside the normal app.
- `build.sh [frontend]` builds `desktop/build/bin/orchestrator-perf.exe` against a patched go-webview2 (via a GOWORK file outside the repo) that appends `ORCHESTRATOR_WEBVIEW_ARGS` to the WebView2 browser arguments, which is how `--remote-debugging-port` gets in.
- `startup.mjs [runs]` cold-launch timings (Go startup trace + navigation/paint timings).
- `calls.mjs` every backend binding call during boot with latency and payload size.
- `openchat.mjs [project-label] [chat-text]` opens a chat from the island and reports LoadHistory cost, long tasks, frames and a CPU profile.
- `send.mjs` sends a real message, measures time to the user bubble and reply, then replays 500 synthetic streamed deltas and reports main-thread CPU.
