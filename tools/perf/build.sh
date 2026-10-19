#!/usr/bin/env bash
set -e
cd "$(dirname "$0")/../../desktop"
if [ "$1" = "frontend" ]; then (cd frontend && npx vite build --logLevel warn); fi
taskkill //F //IM orchestrator-perf.exe >/dev/null 2>&1 || true
GOWORK="${PERF_GOWORK:-C:\Users\PC\AppData\Local\Temp\orch-perf-work\perf.work}" wails build -s -skipbindings -o orchestrator-perf.exe 2>&1 | grep -E "Built|ERROR|error" || true
