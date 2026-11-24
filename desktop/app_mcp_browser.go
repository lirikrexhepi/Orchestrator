package main

import (
	"context"
	"fmt"
	"time"

	"composer/internal/browser"
	"composer/internal/mcp"
)

func (b *appMCPBackend) ensureBrowser() (*browser.Manager, error) {
	if b.app == nil || b.app.browsers == nil {
		return nil, browser.ErrBrowserClosed
	}
	return b.app.browsers, nil
}

func (b *appMCPBackend) emitBrowserTabOpened(threadID string, info browser.SessionInfo) {
	if b.app == nil {
		return
	}
	b.app.emit("browser:agent_tab_opened", map[string]any{
		"threadId": threadID,
		"tab":      info,
	})
}

func (b *appMCPBackend) resolveTab(threadID, tabID string) (*browser.Manager, string, error) {
	mgr, err := b.ensureBrowser()
	if err != nil {
		return nil, "", err
	}
	resolved, err := mgr.ResolveTabID(threadID, tabID)
	if err != nil {
		return nil, "", fmt.Errorf("no open browser tab for this thread — call browser_open first")
	}
	return mgr, resolved, nil
}

func (b *appMCPBackend) BrowserOpen(ctx context.Context, threadID, url string, width, height int) (browser.SessionInfo, error) {
	mgr, err := b.ensureBrowser()
	if err != nil {
		return browser.SessionInfo{}, err
	}
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 800
	}
	openCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	info, err := mgr.Open(openCtx, threadID, url, width, height)
	if err != nil {
		return browser.SessionInfo{}, err
	}
	b.emitBrowserTabOpened(threadID, info)
	// Brief settle so initial navigation begins before agent inspects
	time.Sleep(350 * time.Millisecond)
	if updated, ok := mgr.GetTabInfo(info.ID); ok {
		info = updated
	}
	return info, nil
}

func (b *appMCPBackend) BrowserListTabs(ctx context.Context, threadID string) ([]browser.SessionInfo, error) {
	mgr, err := b.ensureBrowser()
	if err != nil {
		return nil, err
	}
	return mgr.ListTabs(threadID), nil
}

func (b *appMCPBackend) BrowserNavigate(ctx context.Context, threadID, tabID, url string) (browser.SessionInfo, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return browser.SessionInfo{}, err
	}
	navCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := mgr.Navigate(navCtx, id, url); err != nil {
		return browser.SessionInfo{}, err
	}
	if info, ok := mgr.GetTabInfo(id); ok {
		b.emitBrowserTabOpened(threadID, info)
		return info, nil
	}
	return browser.SessionInfo{ID: id, ThreadID: threadID, URL: url}, nil
}

func (b *appMCPBackend) BrowserScreenshot(ctx context.Context, threadID, tabID string, grid bool) (mcp.BrowserScreenshotResult, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return mcp.BrowserScreenshotResult{}, err
	}
	shotCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	b64, w, h, err := mgr.CaptureScreenshotJPEG(shotCtx, id, 82)
	if err != nil {
		return mcp.BrowserScreenshotResult{}, err
	}
	if grid && b64 != "" {
		if gridded, gErr := browser.OverlayGrid(b64, w, h); gErr == nil {
			b64 = gridded
		}
	}
	info, _ := mgr.GetTabInfo(id)
	b.emitBrowserTabOpened(threadID, info)
	if b.app != nil && b64 != "" {
		b.app.emit("browser:agent_screenshot", map[string]any{
			"threadId": threadID,
			"tabId":    id,
			"url":      info.URL,
			"width":    w,
			"height":   h,
			"dataUrl":  "data:image/jpeg;base64," + b64,
			"at":       time.Now().UnixMilli(),
		})
	}
	return mcp.BrowserScreenshotResult{
		TabID:      id,
		URL:        info.URL,
		Width:      w,
		Height:     h,
		Base64JPEG: b64,
	}, nil
}

func (b *appMCPBackend) BrowserSnapshot(ctx context.Context, threadID, tabID string) (browser.SnapshotResult, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return browser.SnapshotResult{}, err
	}
	snapCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return mgr.Snapshot(snapCtx, id)
}

func (b *appMCPBackend) BrowserClick(ctx context.Context, threadID, tabID string, opts browser.ClickOptions) (mcp.BrowserPointResult, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return mcp.BrowserPointResult{}, err
	}
	clickCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	x, y, err := mgr.Click(clickCtx, id, opts)
	if err != nil {
		return mcp.BrowserPointResult{}, err
	}
	if info, ok := mgr.GetTabInfo(id); ok {
		b.emitBrowserTabOpened(threadID, info)
	}
	return mcp.BrowserPointResult{TabID: id, X: x, Y: y, Errors: mgr.DrainNewErrors(id)}, nil
}

func (b *appMCPBackend) BrowserDrag(ctx context.Context, threadID, tabID string, opts browser.DragOptions) ([]browser.ConsoleEntry, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return nil, err
	}
	dragCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if info, ok := mgr.GetTabInfo(id); ok {
		b.emitBrowserTabOpened(threadID, info)
	}
	if err := mgr.Drag(dragCtx, id, opts); err != nil {
		return nil, err
	}
	return mgr.DrainNewErrors(id), nil
}

func (b *appMCPBackend) BrowserWheel(ctx context.Context, threadID, tabID string, x, y, deltaX, deltaY float64, modifiers []string) error {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return err
	}
	wheelCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return mgr.Wheel(wheelCtx, id, x, y, deltaX, deltaY, modifiers)
}

func (b *appMCPBackend) BrowserType(ctx context.Context, threadID, tabID, text, ref, selector string) ([]browser.ConsoleEntry, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return nil, err
	}
	typeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := mgr.TypeText(typeCtx, id, text, ref, selector); err != nil {
		return nil, err
	}
	return mgr.DrainNewErrors(id), nil
}

func (b *appMCPBackend) BrowserPress(ctx context.Context, threadID, tabID, key string, modifiers []string) ([]browser.ConsoleEntry, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return nil, err
	}
	pressCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := mgr.PressKey(pressCtx, id, key, modifiers); err != nil {
		return nil, err
	}
	return mgr.DrainNewErrors(id), nil
}

func (b *appMCPBackend) BrowserBatch(ctx context.Context, threadID, tabID string, steps []browser.BatchStep) (browser.BatchResult, []browser.ConsoleEntry, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return browser.BatchResult{}, nil, err
	}
	batchCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	if info, ok := mgr.GetTabInfo(id); ok {
		b.emitBrowserTabOpened(threadID, info)
	}
	res := mgr.Batch(batchCtx, id, steps)
	return res, mgr.DrainNewErrors(id), nil
}

func (b *appMCPBackend) BrowserEval(ctx context.Context, threadID, tabID, expression string) (any, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return nil, err
	}
	evalCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return mgr.Evaluate(evalCtx, id, expression)
}

func (b *appMCPBackend) BrowserConsole(ctx context.Context, threadID, tabID string, clear bool) ([]browser.ConsoleEntry, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return nil, err
	}
	return mgr.ConsoleLogs(id, clear)
}

func (b *appMCPBackend) BrowserResize(ctx context.Context, threadID, tabID string, width, height int) (browser.SessionInfo, error) {
	mgr, id, err := b.resolveTab(threadID, tabID)
	if err != nil {
		return browser.SessionInfo{}, err
	}
	resizeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := mgr.Resize(resizeCtx, id, width, height, 1.0); err != nil {
		return browser.SessionInfo{}, err
	}
	info, _ := mgr.GetTabInfo(id)
	return info, nil
}
