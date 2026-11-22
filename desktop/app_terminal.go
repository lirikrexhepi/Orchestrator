package main

import (
	"composer/internal/terminal"
)

// TerminalEndpointInfo holds connection details for the loopback terminal WebSocket server.
type TerminalEndpointInfo struct {
	WSURL string `json:"wsUrl"`
	Token string `json:"token"`
}

// TerminalEndpoint returns the local loopback WebSocket URL and auth token.
func (a *App) TerminalEndpoint() TerminalEndpointInfo {
	if a.terminalServer == nil {
		return TerminalEndpointInfo{}
	}
	wsURL, token := a.terminalServer.Endpoint()
	return TerminalEndpointInfo{
		WSURL: wsURL,
		Token: token,
	}
}

// TerminalOpen creates and starts a new server-owned ConPTY terminal session.
func (a *App) TerminalOpen(threadID, cwd, title string, cols, rows int) (terminal.SessionInfo, error) {
	if a.terminals == nil {
		return terminal.SessionInfo{}, terminal.ErrSessionClosed
	}

	targetCwd := cwd
	if targetCwd == "" {
		if res, err := a.requireCwd(""); err == nil && res != "" {
			targetCwd = res
		}
	} else {
		if res, err := a.requireCwd(targetCwd); err == nil && res != "" {
			targetCwd = res
		}
	}

	return a.terminals.Open(threadID, targetCwd, title, cols, rows)
}

// TerminalList returns all active or recent terminal sessions for the given thread.
func (a *App) TerminalList(threadID string) []terminal.SessionInfo {
	if a.terminals == nil {
		return []terminal.SessionInfo{}
	}
	return a.terminals.List(threadID)
}

// TerminalResize updates the terminal dimensions on the PTY.
func (a *App) TerminalResize(id string, cols, rows int) error {
	if a.terminals == nil {
		return terminal.ErrSessionNotFound
	}
	return a.terminals.Resize(id, cols, rows)
}

// TerminalClose kills and unregisters the terminal session.
func (a *App) TerminalClose(id string) error {
	if a.terminals == nil {
		return nil
	}
	return a.terminals.Close(id)
}
