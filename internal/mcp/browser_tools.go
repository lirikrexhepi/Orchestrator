package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"composer/internal/browser"
)

// BrowserScreenshotResult holds a captured JPEG screenshot plus tab metadata.
type BrowserScreenshotResult struct {
	TabID      string `json:"tabId"`
	URL        string `json:"url"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Base64JPEG string `json:"-"`
}

// BrowserPointResult reports the coordinates of a click/interaction.
type BrowserPointResult struct {
	TabID  string                 `json:"tabId"`
	X      float64                `json:"x"`
	Y      float64                `json:"y"`
	Errors []browser.ConsoleEntry `json:"errors,omitempty"`
}

// BrowserActionResult reports a generic action result plus any new console/network errors.
type BrowserActionResult struct {
	OK     bool                   `json:"ok"`
	Errors []browser.ConsoleEntry `json:"errors,omitempty"`
}

// BrowserBackend is optionally implemented by Backend to expose the embedded
// Chromium CDP browser to agents across all providers.
type BrowserBackend interface {
	BrowserOpen(ctx context.Context, threadID, url string, width, height int) (browser.SessionInfo, error)
	BrowserListTabs(ctx context.Context, threadID string) ([]browser.SessionInfo, error)
	BrowserNavigate(ctx context.Context, threadID, tabID, url string) (browser.SessionInfo, error)
	BrowserScreenshot(ctx context.Context, threadID, tabID string, grid bool) (BrowserScreenshotResult, error)
	BrowserSnapshot(ctx context.Context, threadID, tabID string) (browser.SnapshotResult, error)
	BrowserClick(ctx context.Context, threadID, tabID string, opts browser.ClickOptions) (BrowserPointResult, error)
	BrowserDrag(ctx context.Context, threadID, tabID string, opts browser.DragOptions) ([]browser.ConsoleEntry, error)
	BrowserWheel(ctx context.Context, threadID, tabID string, x, y, deltaX, deltaY float64, modifiers []string) error
	BrowserType(ctx context.Context, threadID, tabID, text, ref, selector string) ([]browser.ConsoleEntry, error)
	BrowserPress(ctx context.Context, threadID, tabID, key string, modifiers []string) ([]browser.ConsoleEntry, error)
	BrowserEval(ctx context.Context, threadID, tabID, expression string) (any, error)
	BrowserConsole(ctx context.Context, threadID, tabID string, clear bool) ([]browser.ConsoleEntry, error)
	BrowserResize(ctx context.Context, threadID, tabID string, width, height int) (browser.SessionInfo, error)
	BrowserBatch(ctx context.Context, threadID, tabID string, steps []browser.BatchStep) (browser.BatchResult, []browser.ConsoleEntry, error)
}

var browserTools = []Tool{
	{
		Name:        "browser_open",
		Description: "Open a URL in the embedded Orchestrator browser (reusing the thread's open tab if one exists and newTab is false). Automatically shows the live browser pane side-by-side with chat.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"url":    {Type: "string", Description: "URL to open, e.g. http://localhost:5173"},
				"width":  {Type: "integer", Description: "Optional viewport width in CSS pixels"},
				"height": {Type: "integer", Description: "Optional viewport height in CSS pixels"},
				"newTab": {Type: "boolean", Description: "Force opening a new tab instead of reusing the current thread tab"},
			},
			Required: []string{"url"},
		},
	},
	{
		Name:        "browser_tabs",
		Description: "List open tabs in the embedded Orchestrator browser for this thread.",
		InputSchema: ToolInputSchema{Type: "object"},
	},
	{
		Name:        "browser_navigate",
		Description: "Navigate the active browser tab (or tabId) to a URL.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"url":   {Type: "string"},
				"tabId": {Type: "string"},
			},
			Required: []string{"url"},
		},
	},
	{
		Name:        "browser_screenshot",
		Description: "Capture a visual screenshot of the current browser viewport and return it as an image. Set grid:true for a 100px coordinate ruler on canvas apps.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"grid":  {Type: "boolean"},
				"tabId": {Type: "string"},
			},
		},
	},
	{
		Name:        "browser_snapshot",
		Description: "Inspect visible interactive DOM elements and <canvas> bounds with ref IDs (e1, c1, ...) and center coordinates.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"tabId": {Type: "string"},
			},
		},
	},
	{
		Name:        "browser_click",
		Description: "Click an element by ref (from browser_snapshot), CSS selector, or (x, y). Set screenshot:true to also return the updated viewport image.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"x":          {Type: "number"},
				"y":          {Type: "number"},
				"ref":        {Type: "string"},
				"selector":   {Type: "string"},
				"button":     {Type: "string", Enum: []string{"left", "middle", "right"}},
				"clickCount": {Type: "integer"},
				"modifiers":  {Type: "array", Items: &PropertySchema{Type: "string", Enum: []string{"ctrl", "shift", "alt", "meta"}}},
				"screenshot": {Type: "boolean"},
				"grid":       {Type: "boolean"},
				"tabId":      {Type: "string"},
			},
		},
	},
	{
		Name:        "browser_drag",
		Description: "Smoothly drag from (fromX, fromY) to (toX, toY) with interpolated steps — ideal for 3D camera orbit/pan, sliders, or canvas drawing.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"fromX":      {Type: "number"},
				"fromY":      {Type: "number"},
				"toX":        {Type: "number"},
				"toY":        {Type: "number"},
				"button":     {Type: "string", Enum: []string{"left", "middle", "right"}},
				"steps":      {Type: "integer"},
				"durationMs": {Type: "integer"},
				"modifiers":  {Type: "array", Items: &PropertySchema{Type: "string", Enum: []string{"ctrl", "shift", "alt", "meta"}}},
				"screenshot": {Type: "boolean"},
				"grid":       {Type: "boolean"},
				"tabId":      {Type: "string"},
			},
			Required: []string{"fromX", "fromY", "toX", "toY"},
		},
	},
	{
		Name:        "browser_wheel",
		Description: "Scroll or zoom (mouse wheel) at viewport coordinates (x, y).",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"x":         {Type: "number"},
				"y":         {Type: "number"},
				"deltaX":    {Type: "number"},
				"deltaY":    {Type: "number"},
				"modifiers": {Type: "array", Items: &PropertySchema{Type: "string", Enum: []string{"ctrl", "shift", "alt", "meta"}}},
				"tabId":     {Type: "string"},
			},
			Required: []string{"x", "y", "deltaY"},
		},
	},
	{
		Name:        "browser_type",
		Description: "Type text into the focused element, or click ref/selector first and type.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"text":       {Type: "string"},
				"ref":        {Type: "string"},
				"selector":   {Type: "string"},
				"screenshot": {Type: "boolean"},
				"tabId":      {Type: "string"},
			},
			Required: []string{"text"},
		},
	},
	{
		Name:        "browser_press",
		Description: "Press a keyboard key or shortcut (e.g. Escape, Enter, Delete, ArrowLeft, or 'z' with modifiers: ['ctrl']).",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"key":        {Type: "string"},
				"modifiers":  {Type: "array", Items: &PropertySchema{Type: "string", Enum: []string{"ctrl", "shift", "alt", "meta"}}},
				"screenshot": {Type: "boolean"},
				"tabId":      {Type: "string"},
			},
			Required: []string{"key"},
		},
	},
	{
		Name:        "browser_batch",
		Description: "Execute a sequence of browser actions (click, drag, wheel, type, press, wait, eval, navigate, snapshot) in one fast call, stopping on error. Optionally capture a final snapshot and/or screenshot.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"steps":      {Type: "array", Items: &PropertySchema{Type: "object"}},
				"snapshot":   {Type: "boolean"},
				"screenshot": {Type: "boolean"},
				"grid":       {Type: "boolean"},
				"tabId":      {Type: "string"},
			},
			Required: []string{"steps"},
		},
	},
	{
		Name:        "browser_eval",
		Description: "Evaluate a JavaScript expression in the page and return the JSON result.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"expression": {Type: "string"},
				"tabId":      {Type: "string"},
			},
			Required: []string{"expression"},
		},
	},
	{
		Name:        "browser_console",
		Description: "Read recent browser console logs, uncaught JS exceptions, and failed network requests.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"clear": {Type: "boolean"},
				"tabId": {Type: "string"},
			},
		},
	},
	{
		Name:        "browser_resize",
		Description: "Resize the browser viewport to explicit CSS pixel dimensions.",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"width":  {Type: "integer"},
				"height": {Type: "integer"},
				"tabId":  {Type: "string"},
			},
			Required: []string{"width", "height"},
		},
	},
}

var browserToolAcceptedKeys = map[string][]string{
	"browser_open":       {"height", "newTab", "url", "width"},
	"browser_tabs":       {},
	"browser_navigate":   {"tabId", "url"},
	"browser_screenshot": {"grid", "tabId"},
	"browser_snapshot":   {"tabId"},
	"browser_click":      {"button", "clickCount", "grid", "modifiers", "ref", "screenshot", "selector", "tabId", "x", "y"},
	"browser_drag":       {"button", "durationMs", "fromX", "fromY", "grid", "modifiers", "screenshot", "steps", "tabId", "toX", "toY"},
	"browser_wheel":      {"deltaX", "deltaY", "modifiers", "tabId", "x", "y"},
	"browser_type":       {"ref", "screenshot", "selector", "tabId", "text"},
	"browser_press":      {"key", "modifiers", "screenshot", "tabId"},
	"browser_batch":      {"grid", "screenshot", "snapshot", "steps", "tabId"},
	"browser_eval":       {"expression", "tabId"},
	"browser_console":    {"clear", "tabId"},
	"browser_resize":     {"height", "tabId", "width"},
}

func isBrowserTool(name string) bool {
	_, ok := browserToolAcceptedKeys[name]
	return ok
}

func executeBrowserTool(ctx context.Context, backend Backend, scope CallerScope, name string, rawArgs json.RawMessage) CallToolResult {
	bb, ok := backend.(BrowserBackend)
	if !ok || bb == nil {
		return toolError("embedded browser is not available")
	}

	switch name {
	case "browser_open":
		var args struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			NewTab bool   `json:"newTab"`
		}
		if len(rawArgs) > 0 && string(rawArgs) != "null" {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return toolError(fmt.Sprintf("invalid arguments: %v", err))
			}
		}
		if strings.TrimSpace(args.URL) == "" {
			return toolError("url is required")
		}
		if !args.NewTab {
			if tabs, err := bb.BrowserListTabs(ctx, scope.ThreadID); err == nil && len(tabs) > 0 {
				info, navErr := bb.BrowserNavigate(ctx, scope.ThreadID, tabs[len(tabs)-1].ID, args.URL)
				if navErr == nil {
					return toolSuccess(info)
				}
			}
		}
		info, err := bb.BrowserOpen(ctx, scope.ThreadID, args.URL, args.Width, args.Height)
		if err != nil {
			return toolError(err.Error())
		}
		return toolSuccess(info)

	case "browser_tabs":
		tabs, err := bb.BrowserListTabs(ctx, scope.ThreadID)
		if err != nil {
			return toolError(err.Error())
		}
		if tabs == nil {
			tabs = []browser.SessionInfo{}
		}
		return toolSuccess(tabs)

	case "browser_navigate":
		var args struct {
			URL   string `json:"url"`
			TabID string `json:"tabId"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err))
		}
		if strings.TrimSpace(args.URL) == "" {
			return toolError("url is required")
		}
		info, err := bb.BrowserNavigate(ctx, scope.ThreadID, args.TabID, args.URL)
		if err != nil {
			return toolError(err.Error())
		}
		return toolSuccess(info)

	case "browser_screenshot":
		var args struct {
			Grid  bool   `json:"grid"`
			TabID string `json:"tabId"`
		}
		if len(rawArgs) > 0 && string(rawArgs) != "null" {
			_ = json.Unmarshal(rawArgs, &args)
		}
		shot, err := bb.BrowserScreenshot(ctx, scope.ThreadID, args.TabID, args.Grid)
		if err != nil {
			return toolError(err.Error())
		}
		metaBytes, _ := json.Marshal(shot)
		return CallToolResult{
			Content: []ContentItem{
				{
					Type:     "image",
					Data:     shot.Base64JPEG,
					MimeType: "image/jpeg",
				},
				{
					Type: "text",
					Text: string(metaBytes),
				},
			},
		}

	case "browser_snapshot":
		var args struct {
			TabID string `json:"tabId"`
		}
		if len(rawArgs) > 0 && string(rawArgs) != "null" {
			_ = json.Unmarshal(rawArgs, &args)
		}
		snap, err := bb.BrowserSnapshot(ctx, scope.ThreadID, args.TabID)
		if err != nil {
			return toolError(err.Error())
		}
		return toolSuccess(snap)

	case "browser_click":
		var args struct {
			X          *float64 `json:"x"`
			Y          *float64 `json:"y"`
			Ref        string   `json:"ref"`
			Selector   string   `json:"selector"`
			Button     string   `json:"button"`
			ClickCount int      `json:"clickCount"`
			Modifiers  []string `json:"modifiers"`
			Screenshot bool     `json:"screenshot"`
			Grid       bool     `json:"grid"`
			TabID      string   `json:"tabId"`
		}
		if len(rawArgs) > 0 && string(rawArgs) != "null" {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return toolError(fmt.Sprintf("invalid arguments: %v", err))
			}
		}
		pt, err := bb.BrowserClick(ctx, scope.ThreadID, args.TabID, browser.ClickOptions{
			X:          args.X,
			Y:          args.Y,
			Ref:        args.Ref,
			Selector:   args.Selector,
			Button:     args.Button,
			ClickCount: args.ClickCount,
			Modifiers:  args.Modifiers,
		})
		if err != nil {
			return toolError(err.Error())
		}
		return withOptionalScreenshot(ctx, bb, scope.ThreadID, pt.TabID, args.Screenshot, args.Grid, pt)

	case "browser_drag":
		var args struct {
			FromX      float64  `json:"fromX"`
			FromY      float64  `json:"fromY"`
			ToX        float64  `json:"toX"`
			ToY        float64  `json:"toY"`
			Button     string   `json:"button"`
			Steps      int      `json:"steps"`
			DurationMs int      `json:"durationMs"`
			Modifiers  []string `json:"modifiers"`
			Screenshot bool     `json:"screenshot"`
			Grid       bool     `json:"grid"`
			TabID      string   `json:"tabId"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err))
		}
		errs, err := bb.BrowserDrag(ctx, scope.ThreadID, args.TabID, browser.DragOptions{
			FromX:      args.FromX,
			FromY:      args.FromY,
			ToX:        args.ToX,
			ToY:        args.ToY,
			Button:     args.Button,
			Steps:      args.Steps,
			DurationMs: args.DurationMs,
			Modifiers:  args.Modifiers,
		})
		if err != nil {
			return toolError(err.Error())
		}
		return withOptionalScreenshot(ctx, bb, scope.ThreadID, args.TabID, args.Screenshot, args.Grid, BrowserActionResult{OK: true, Errors: errs})

	case "browser_wheel":
		var args struct {
			X         float64  `json:"x"`
			Y         float64  `json:"y"`
			DeltaX    float64  `json:"deltaX"`
			DeltaY    float64  `json:"deltaY"`
			Modifiers []string `json:"modifiers"`
			TabID     string   `json:"tabId"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err))
		}
		if err := bb.BrowserWheel(ctx, scope.ThreadID, args.TabID, args.X, args.Y, args.DeltaX, args.DeltaY, args.Modifiers); err != nil {
			return toolError(err.Error())
		}
		return toolSuccess(map[string]any{"ok": true})

	case "browser_type":
		var args struct {
			Text       string `json:"text"`
			Ref        string `json:"ref"`
			Selector   string `json:"selector"`
			Screenshot bool   `json:"screenshot"`
			TabID      string `json:"tabId"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err))
		}
		errs, err := bb.BrowserType(ctx, scope.ThreadID, args.TabID, args.Text, args.Ref, args.Selector)
		if err != nil {
			return toolError(err.Error())
		}
		return withOptionalScreenshot(ctx, bb, scope.ThreadID, args.TabID, args.Screenshot, false, BrowserActionResult{OK: true, Errors: errs})

	case "browser_press":
		var args struct {
			Key        string   `json:"key"`
			Modifiers  []string `json:"modifiers"`
			Screenshot bool     `json:"screenshot"`
			TabID      string   `json:"tabId"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err))
		}
		errs, err := bb.BrowserPress(ctx, scope.ThreadID, args.TabID, args.Key, args.Modifiers)
		if err != nil {
			return toolError(err.Error())
		}
		return withOptionalScreenshot(ctx, bb, scope.ThreadID, args.TabID, args.Screenshot, false, BrowserActionResult{OK: true, Errors: errs})

	case "browser_batch":
		var args struct {
			Steps      []browser.BatchStep `json:"steps"`
			Snapshot   bool                `json:"snapshot"`
			Screenshot bool                `json:"screenshot"`
			Grid       bool                `json:"grid"`
			TabID      string              `json:"tabId"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err))
		}
		if len(args.Steps) == 0 {
			return toolError("steps must not be empty")
		}
		if len(args.Steps) > 25 {
			return toolError("maximum 25 steps per batch")
		}
		res, errs, err := bb.BrowserBatch(ctx, scope.ThreadID, args.TabID, args.Steps)
		if err != nil {
			return toolError(err.Error())
		}
		if args.Snapshot && res.LastSnapshot == nil {
			if snap, snapErr := bb.BrowserSnapshot(ctx, scope.ThreadID, res.TabID); snapErr == nil {
				res.LastSnapshot = &snap
			}
		}
		payload := struct {
			browser.BatchResult
			Errors []browser.ConsoleEntry `json:"errors,omitempty"`
		}{BatchResult: res, Errors: errs}
		return withOptionalScreenshot(ctx, bb, scope.ThreadID, res.TabID, args.Screenshot, args.Grid, payload)

	case "browser_eval":
		var args struct {
			Expression string `json:"expression"`
			TabID      string `json:"tabId"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err))
		}
		val, err := bb.BrowserEval(ctx, scope.ThreadID, args.TabID, args.Expression)
		if err != nil {
			return toolError(err.Error())
		}
		return toolSuccess(map[string]any{"result": val})

	case "browser_console":
		var args struct {
			Clear bool   `json:"clear"`
			TabID string `json:"tabId"`
		}
		if len(rawArgs) > 0 && string(rawArgs) != "null" {
			_ = json.Unmarshal(rawArgs, &args)
		}
		entries, err := bb.BrowserConsole(ctx, scope.ThreadID, args.TabID, args.Clear)
		if err != nil {
			return toolError(err.Error())
		}
		if entries == nil {
			entries = []browser.ConsoleEntry{}
		}
		return toolSuccess(entries)

	case "browser_resize":
		var args struct {
			Width  int    `json:"width"`
			Height int    `json:"height"`
			TabID  string `json:"tabId"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err))
		}
		info, err := bb.BrowserResize(ctx, scope.ThreadID, args.TabID, args.Width, args.Height)
		if err != nil {
			return toolError(err.Error())
		}
		return toolSuccess(info)

	default:
		return toolError(fmt.Sprintf("unknown browser tool %q", name))
	}
}

func withOptionalScreenshot(ctx context.Context, bb BrowserBackend, threadID, tabID string, wantShot, grid bool, data any) CallToolResult {
	b, err := json.Marshal(data)
	if err != nil {
		return toolError(fmt.Sprintf("failed to serialize response: %v", err))
	}
	if !wantShot {
		return CallToolResult{Content: []ContentItem{{Type: "text", Text: string(b)}}}
	}
	shot, shotErr := bb.BrowserScreenshot(ctx, threadID, tabID, grid)
	if shotErr != nil {
		return CallToolResult{Content: []ContentItem{{Type: "text", Text: string(b)}}}
	}
	return CallToolResult{
		Content: []ContentItem{
			{Type: "image", Data: shot.Base64JPEG, MimeType: "image/jpeg"},
			{Type: "text", Text: string(b)},
		},
	}
}
