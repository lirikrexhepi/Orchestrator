package main

import (
	"context"
	"time"

	"composer/internal/browser"
)

// BrowserEndpointInfo holds connection details for the loopback browser WebSocket server.
type BrowserEndpointInfo struct {
	WSURL string `json:"wsUrl"`
	Token string `json:"token"`
}

// BrowserEndpoint returns the local loopback WebSocket URL and auth token.
func (a *App) BrowserEndpoint() BrowserEndpointInfo {
	if a.browserServer == nil {
		return BrowserEndpointInfo{}
	}
	wsURL, token := a.browserServer.Endpoint()
	return BrowserEndpointInfo{
		WSURL: wsURL,
		Token: token,
	}
}

// BrowserOpen opens a new browser tab in headless Chromium.
func (a *App) BrowserOpen(threadID, url string, width, height int) (browser.SessionInfo, error) {
	if a.browsers == nil {
		return browser.SessionInfo{}, browser.ErrBrowserClosed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	return a.browsers.Open(ctx, threadID, url, width, height)
}

// BrowserClose closes an active browser tab.
func (a *App) BrowserClose(id string) error {
	if a.browsers == nil {
		return nil
	}
	return a.browsers.Close(id)
}
