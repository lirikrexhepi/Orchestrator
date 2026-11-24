package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const maxConsoleEntries = 200

func (t *tab) appendConsole(entry ConsoleEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(entry.Text) > 2000 {
		entry.Text = entry.Text[:2000] + "…"
	}
	t.consoleLogs = append(t.consoleLogs, entry)
	if len(t.consoleLogs) > maxConsoleEntries {
		trim := len(t.consoleLogs) - maxConsoleEntries
		t.consoleLogs = t.consoleLogs[trim:]
		t.consoleCursor -= trim
		if t.consoleCursor < 0 {
			t.consoleCursor = 0
		}
	}
}

// DrainNewErrors returns up to 5 new error/exception/network entries since the
// previous drain, keeping MCP tool responses compact while surfacing failures.
func (m *Manager) DrainNewErrors(id string) []ConsoleEntry {
	m.mu.Lock()
	t, ok := m.tabs[id]
	m.mu.Unlock()
	if !ok || t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.consoleCursor >= len(t.consoleLogs) {
		return nil
	}
	var out []ConsoleEntry
	for i := t.consoleCursor; i < len(t.consoleLogs); i++ {
		e := t.consoleLogs[i]
		if e.Level == "error" || e.Level == "exception" || e.Level == "network" {
			if len(e.Text) > 200 {
				e.Text = e.Text[:200] + "…"
			}
			out = append(out, e)
			if len(out) >= 5 {
				break
			}
		}
	}
	t.consoleCursor = len(t.consoleLogs)
	return out
}

func (m *Manager) broadcastAgentAction(t *tab, action AgentAction) {
	if t == nil {
		return
	}
	if action.Timestamp == 0 {
		action.Timestamp = time.Now().UnixMilli()
	}
	t.mu.RLock()
	subscribers := make([]chan tabEvent, 0, len(t.subscribers))
	for ch := range t.subscribers {
		subscribers = append(subscribers, ch)
	}
	t.mu.RUnlock()

	actCopy := action
	for _, ch := range subscribers {
		select {
		case ch <- tabEvent{agentAction: &actCopy}:
		default:
		}
	}
}

func matchesThreadScope(tabThreadID, threadID string) bool {
	if threadID == "" {
		return true
	}
	if tabThreadID == threadID {
		return true
	}
	return strings.HasPrefix(tabThreadID, threadID+":")
}

// ListTabs returns active tabs scoped to threadID (matching exact threadID or "threadID:*").
// If threadID is empty, returns all tabs.
func (m *Manager) ListTabs(threadID string) []SessionInfo {
	m.mu.Lock()
	out := make([]SessionInfo, 0, len(m.tabs))
	for _, t := range m.tabs {
		if matchesThreadScope(t.threadID, threadID) {
			out = append(out, t.info())
		}
	}
	m.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// ResolveTabID finds an existing tab matching tabID (if given) or the most recently created tab for threadID.
func (m *Manager) ResolveTabID(threadID, tabID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if tabID != "" {
		if _, ok := m.tabs[tabID]; ok {
			return tabID, nil
		}
		return "", ErrTabNotFound
	}

	var matched []string
	for id, t := range m.tabs {
		if matchesThreadScope(t.threadID, threadID) {
			matched = append(matched, id)
		}
	}
	if len(matched) == 0 {
		return "", ErrTabNotFound
	}
	sort.Strings(matched)
	return matched[len(matched)-1], nil
}

// GetTabInfo returns SessionInfo for a specific tab ID.
func (m *Manager) GetTabInfo(id string) (SessionInfo, bool) {
	m.mu.Lock()
	t, ok := m.tabs[id]
	m.mu.Unlock()
	if !ok || t == nil {
		return SessionInfo{}, false
	}
	return t.info(), true
}

// ConsoleLogs returns recent console/exception/network log entries for a tab.
func (m *Manager) ConsoleLogs(id string, clear bool) ([]ConsoleEntry, error) {
	m.mu.Lock()
	t, ok := m.tabs[id]
	m.mu.Unlock()
	if !ok || t == nil {
		return nil, ErrTabNotFound
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]ConsoleEntry, len(t.consoleLogs))
	copy(out, t.consoleLogs)
	if clear {
		t.consoleLogs = nil
	}
	return out, nil
}

// Evaluate executes a JavaScript expression in the tab's main world and returns the JSON-serializable result.
func (m *Manager) Evaluate(ctx context.Context, id, expression string) (any, error) {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return nil, ErrTabNotFound
	}
	if cdp == nil {
		return nil, ErrBrowserClosed
	}

	var evalRes struct {
		Result struct {
			Type        string `json:"type"`
			Subtype     string `json:"subtype"`
			Value       any    `json:"value"`
			Description string `json:"description"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}

	if err := cdp.Call(ctx, t.sessionID, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"awaitPromise":  true,
		"returnByValue": true,
	}, &evalRes); err != nil {
		return nil, err
	}

	if evalRes.ExceptionDetails != nil {
		msg := strings.TrimSpace(evalRes.ExceptionDetails.Exception.Description)
		if msg == "" {
			msg = strings.TrimSpace(evalRes.ExceptionDetails.Text)
		}
		if msg == "" {
			msg = "JavaScript evaluation threw an exception"
		}
		return nil, fmt.Errorf("%s", msg)
	}

	if evalRes.Result.Value != nil {
		return evalRes.Result.Value, nil
	}
	if evalRes.Result.Type == "undefined" {
		return nil, nil
	}
	if evalRes.Result.Description != "" {
		return evalRes.Result.Description, nil
	}
	return nil, nil
}

// SnapshotElement describes a visible interactive DOM element or canvas with its ref ID and center coordinates.
type SnapshotElement struct {
	Ref    string  `json:"ref"`
	Tag    string  `json:"tag"`
	Role   string  `json:"role,omitempty"`
	Text   string  `json:"text,omitempty"`
	Value  string  `json:"value,omitempty"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// SnapshotResult summarizes the current page DOM for an agent.
type SnapshotResult struct {
	TabID    string              `json:"tabId"`
	URL      string              `json:"url"`
	Title    string              `json:"title"`
	Width    int                 `json:"width"`
	Height   int                 `json:"height"`
	Canvases []SnapshotElement   `json:"canvases,omitempty"`
	Elements []SnapshotElement   `json:"elements"`
	Bridges  map[string][]string `json:"bridges,omitempty"`
	Text     string              `json:"text,omitempty"`
}

const snapshotJS = `(() => {
	const refs = (window.__orchRefs = Object.create(null));
	let seq = 0;
	const vw = window.innerWidth || document.documentElement.clientWidth || 0;
	const vh = window.innerHeight || document.documentElement.clientHeight || 0;

	const isVisible = (el) => {
		const r = el.getBoundingClientRect();
		if (r.width < 2 || r.height < 2) return false;
		if (r.bottom < 0 || r.right < 0 || r.top > vh || r.left > vw) return false;
		const st = getComputedStyle(el);
		if (st.display === 'none' || st.visibility === 'hidden' || parseFloat(st.opacity) === 0) return false;
		return true;
	};

	const labelFor = (el) => {
		const raw =
			el.getAttribute('aria-label') ||
			el.getAttribute('title') ||
			el.getAttribute('placeholder') ||
			el.innerText ||
			el.textContent ||
			'';
		return raw.replace(/\s+/g, ' ').trim().slice(0, 80);
	};

	const canvases = [];
	for (const c of document.querySelectorAll('canvas')) {
		if (!isVisible(c)) continue;
		const r = c.getBoundingClientRect();
		const ref = 'c' + (++seq);
		refs[ref] = c;
		canvases.push({
			ref,
			tag: 'canvas',
			text: c.getAttribute('aria-label') || c.id || c.className?.toString().slice(0, 40) || 'canvas',
			x: Math.round(r.left + r.width / 2),
			y: Math.round(r.top + r.height / 2),
			width: Math.round(r.width),
			height: Math.round(r.height),
		});
	}

	const selector = 'a[href], button, input, select, textarea, [role="button"], [role="tab"], [role="menuitem"], [role="checkbox"], [role="switch"], [role="slider"], [contenteditable="true"], [tabindex]:not([tabindex="-1"])';
	const elements = [];
	const seen = new Set();
	for (const el of document.querySelectorAll(selector)) {
		if (elements.length >= 160) break;
		if (seen.has(el) || !isVisible(el)) continue;
		seen.add(el);
		const r = el.getBoundingClientRect();
		const ref = 'e' + (++seq);
		refs[ref] = el;
		const item = {
			ref,
			tag: el.tagName.toLowerCase(),
			role: el.getAttribute('role') || el.getAttribute('type') || undefined,
			text: labelFor(el) || undefined,
			x: Math.round(r.left + r.width / 2),
			y: Math.round(r.top + r.height / 2),
			width: Math.round(r.width),
			height: Math.round(r.height),
		};
		if ('value' in el && typeof el.value === 'string' && el.value.length > 0 && el.type !== 'password') {
			item.value = el.value.slice(0, 80);
		}
		elements.push(item);
	}

	const discoverBridges = () => {
		const candidates = [
			['window.__ORCHESTRATOR_BRIDGE__', window.__ORCHESTRATOR_BRIDGE__],
			['window.__APP_BRIDGE__', window.__APP_BRIDGE__],
			['window.planner.agent', window.planner && window.planner.agent],
		];
		const out = {};
		let count = 0;
		for (const [name, obj] of candidates) {
			if (!obj || typeof obj !== 'object') continue;
			const keys = new Set(Object.keys(obj));
			const proto = Object.getPrototypeOf(obj);
			if (proto && proto !== Object.prototype) {
				for (const k of Object.getOwnPropertyNames(proto)) {
					if (k !== 'constructor') keys.add(k);
				}
			}
			const list = Array.from(keys).slice(0, 30);
			if (list.length > 0) {
				out[name] = list;
				count++;
			}
		}
		return count > 0 ? out : undefined;
	};

	const bodyText = (document.body?.innerText || '').replace(/\s+/g, ' ').trim().slice(0, 1200);
	return {
		url: location.href,
		title: document.title || '',
		width: vw,
		height: vh,
		canvases,
		elements,
		bridges: discoverBridges(),
		text: bodyText,
	};
})()`

// Snapshot inspects the current page's visible interactive elements and canvases,
// assigning ref IDs ("e1", "c1", ...) that can be passed to Click/TypeText.
func (m *Manager) Snapshot(ctx context.Context, id string) (SnapshotResult, error) {
	raw, err := m.Evaluate(ctx, id, snapshotJS)
	if err != nil {
		return SnapshotResult{}, err
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return SnapshotResult{}, err
	}
	var res SnapshotResult
	if err := json.Unmarshal(b, &res); err != nil {
		return SnapshotResult{}, err
	}
	res.TabID = id
	return res, nil
}

func (m *Manager) resolveElementCenter(ctx context.Context, id, ref, selector string) (float64, float64, string, error) {
	var expr string
	if ref != "" {
		refJSON, _ := json.Marshal(ref)
		expr = fmt.Sprintf(`(() => {
			const el = window.__orchRefs && window.__orchRefs[%s];
			if (!el || !el.isConnected) return { error: 'element ref not found; call browser_snapshot again' };
			el.scrollIntoView({ block: 'center', inline: 'center', behavior: 'instant' });
			const r = el.getBoundingClientRect();
			return { x: r.left + r.width / 2, y: r.top + r.height / 2, label: (el.getAttribute('aria-label') || el.innerText || el.tagName || '').trim().slice(0, 40) };
		})()`, string(refJSON))
	} else {
		selJSON, _ := json.Marshal(selector)
		expr = fmt.Sprintf(`(() => {
			const el = document.querySelector(%s);
			if (!el) return { error: 'selector not found: ' + %s };
			el.scrollIntoView({ block: 'center', inline: 'center', behavior: 'instant' });
			const r = el.getBoundingClientRect();
			return { x: r.left + r.width / 2, y: r.top + r.height / 2, label: (el.getAttribute('aria-label') || el.innerText || el.tagName || '').trim().slice(0, 40) };
		})()`, string(selJSON), string(selJSON))
	}

	raw, err := m.Evaluate(ctx, id, expr)
	if err != nil {
		return 0, 0, "", err
	}
	b, _ := json.Marshal(raw)
	var out struct {
		X     float64 `json:"x"`
		Y     float64 `json:"y"`
		Label string  `json:"label"`
		Error string  `json:"error"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return 0, 0, "", err
	}
	if out.Error != "" {
		return 0, 0, "", fmt.Errorf("%s", out.Error)
	}
	return out.X, out.Y, out.Label, nil
}

// ParseModifiers converts modifier names ("ctrl", "control", "shift", "alt", "meta", "cmd") to CDP bitmask.
func ParseModifiers(mods []string) int {
	mask := 0
	for _, m := range mods {
		switch strings.ToLower(strings.TrimSpace(m)) {
		case "alt", "option":
			mask |= 1
		case "ctrl", "control":
			mask |= 2
		case "meta", "cmd", "command", "super":
			mask |= 4
		case "shift":
			mask |= 8
		}
	}
	return mask
}

func buttonBit(button string) (string, int) {
	switch strings.ToLower(strings.TrimSpace(button)) {
	case "right":
		return "right", 2
	case "middle":
		return "middle", 4
	default:
		return "left", 1
	}
}

// ClickOptions configures an agent click.
type ClickOptions struct {
	X          *float64
	Y          *float64
	Ref        string
	Selector   string
	Button     string
	ClickCount int
	Modifiers  []string
}

// Click moves the pointer and performs a click (or double/triple click) with live cursor feedback.
func (m *Manager) Click(ctx context.Context, id string, opts ClickOptions) (float64, float64, error) {
	m.mu.Lock()
	t, ok := m.tabs[id]
	m.mu.Unlock()
	if !ok || t == nil {
		return 0, 0, ErrTabNotFound
	}

	var x, y float64
	label := ""
	if opts.Ref != "" || opts.Selector != "" {
		var err error
		x, y, label, err = m.resolveElementCenter(ctx, id, opts.Ref, opts.Selector)
		if err != nil {
			return 0, 0, err
		}
	} else if opts.X != nil && opts.Y != nil {
		x, y = *opts.X, *opts.Y
	} else {
		return 0, 0, fmt.Errorf("click requires either (x, y), ref, or selector")
	}

	btn, btnMask := buttonBit(opts.Button)
	clicks := opts.ClickCount
	if clicks <= 0 {
		clicks = 1
	}
	if clicks > 3 {
		clicks = 3
	}
	mods := ParseModifiers(opts.Modifiers)

	actionLabel := fmt.Sprintf("Click (%d, %d)", int(math.Round(x)), int(math.Round(y)))
	if label != "" {
		actionLabel = fmt.Sprintf("Click %q", label)
	}
	m.broadcastAgentAction(t, AgentAction{
		Kind:   "click",
		X:      x,
		Y:      y,
		Button: btn,
		Label:  actionLabel,
	})

	_ = m.DispatchMouse(ctx, id, MouseEvent{
		Type:      "mouseMoved",
		X:         x,
		Y:         y,
		Button:    "none",
		Buttons:   0,
		Modifiers: mods,
	})
	for i := 1; i <= clicks; i++ {
		if err := m.DispatchMouse(ctx, id, MouseEvent{
			Type:       "mousePressed",
			X:          x,
			Y:          y,
			Button:     btn,
			Buttons:    btnMask,
			ClickCount: i,
			Modifiers:  mods,
		}); err != nil {
			return x, y, err
		}
		time.Sleep(25 * time.Millisecond)
		if err := m.DispatchMouse(ctx, id, MouseEvent{
			Type:       "mouseReleased",
			X:          x,
			Y:          y,
			Button:     btn,
			Buttons:    0,
			ClickCount: i,
			Modifiers:  mods,
		}); err != nil {
			return x, y, err
		}
		if i < clicks {
			time.Sleep(35 * time.Millisecond)
		}
	}
	return x, y, nil
}

// DragOptions configures a smooth interpolated drag (essential for 3D orbit/pan and canvas drawing).
type DragOptions struct {
	FromX      float64
	FromY      float64
	ToX        float64
	ToY        float64
	Button     string
	Steps      int
	DurationMs int
	Modifiers  []string
}

// Drag performs a smooth interpolated mouse drag from (FromX, FromY) to (ToX, ToY).
func (m *Manager) Drag(ctx context.Context, id string, opts DragOptions) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	m.mu.Unlock()
	if !ok || t == nil {
		return ErrTabNotFound
	}

	btn, btnMask := buttonBit(opts.Button)
	mods := ParseModifiers(opts.Modifiers)
	steps := opts.Steps
	if steps <= 0 {
		steps = 14
	}
	if steps > 60 {
		steps = 60
	}
	durMs := opts.DurationMs
	if durMs <= 0 {
		durMs = 260
	}
	if durMs > 3000 {
		durMs = 3000
	}
	stepDelay := time.Duration(durMs/steps) * time.Millisecond

	label := fmt.Sprintf("Drag (%d,%d) → (%d,%d)", int(math.Round(opts.FromX)), int(math.Round(opts.FromY)), int(math.Round(opts.ToX)), int(math.Round(opts.ToY)))
	m.broadcastAgentAction(t, AgentAction{
		Kind:   "drag_start",
		X:      opts.FromX,
		Y:      opts.FromY,
		ToX:    opts.ToX,
		ToY:    opts.ToY,
		Button: btn,
		Label:  label,
	})

	_ = m.DispatchMouse(ctx, id, MouseEvent{
		Type:      "mouseMoved",
		X:         opts.FromX,
		Y:         opts.FromY,
		Button:    "none",
		Buttons:   0,
		Modifiers: mods,
	})
	if err := m.DispatchMouse(ctx, id, MouseEvent{
		Type:       "mousePressed",
		X:          opts.FromX,
		Y:          opts.FromY,
		Button:     btn,
		Buttons:    btnMask,
		ClickCount: 1,
		Modifiers:  mods,
	}); err != nil {
		return err
	}

	for i := 1; i <= steps; i++ {
		select {
		case <-ctx.Done():
			_ = m.DispatchMouse(context.Background(), id, MouseEvent{
				Type:       "mouseReleased",
				X:          opts.ToX,
				Y:          opts.ToY,
				Button:     btn,
				Buttons:    0,
				ClickCount: 1,
				Modifiers:  mods,
			})
			return ctx.Err()
		default:
		}
		frac := float64(i) / float64(steps)
		cx := opts.FromX + (opts.ToX-opts.FromX)*frac
		cy := opts.FromY + (opts.ToY-opts.FromY)*frac
		_ = m.DispatchMouse(ctx, id, MouseEvent{
			Type:      "mouseMoved",
			X:         cx,
			Y:         cy,
			Button:    btn,
			Buttons:   btnMask,
			Modifiers: mods,
		})
		m.broadcastAgentAction(t, AgentAction{
			Kind:   "drag_move",
			X:      cx,
			Y:      cy,
			ToX:    opts.ToX,
			ToY:    opts.ToY,
			Button: btn,
			Label:  label,
		})
		if stepDelay > 0 {
			time.Sleep(stepDelay)
		}
	}

	m.broadcastAgentAction(t, AgentAction{
		Kind:   "drag_end",
		X:      opts.ToX,
		Y:      opts.ToY,
		Button: btn,
		Label:  label,
	})
	return m.DispatchMouse(ctx, id, MouseEvent{
		Type:       "mouseReleased",
		X:          opts.ToX,
		Y:          opts.ToY,
		Button:     btn,
		Buttons:    0,
		ClickCount: 1,
		Modifiers:  mods,
	})
}

// Wheel dispatches a mouse wheel scroll/zoom event at (x, y).
func (m *Manager) Wheel(ctx context.Context, id string, x, y, deltaX, deltaY float64, modifiers []string) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	m.mu.Unlock()
	if !ok || t == nil {
		return ErrTabNotFound
	}
	m.broadcastAgentAction(t, AgentAction{
		Kind:  "wheel",
		X:     x,
		Y:     y,
		Label: fmt.Sprintf("Scroll (%.0f, %.0f)", deltaX, deltaY),
	})
	return m.DispatchMouse(ctx, id, MouseEvent{
		Type:      "mouseWheel",
		X:         x,
		Y:         y,
		DeltaX:    deltaX,
		DeltaY:    deltaY,
		Modifiers: ParseModifiers(modifiers),
	})
}

// TypeText optionally clicks a target ref/selector, then inserts text.
func (m *Manager) TypeText(ctx context.Context, id, text, ref, selector string) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	m.mu.Unlock()
	if !ok || t == nil {
		return ErrTabNotFound
	}
	var x, y float64
	if ref != "" || selector != "" {
		var err error
		x, y, err = m.Click(ctx, id, ClickOptions{Ref: ref, Selector: selector})
		if err != nil {
			return err
		}
		time.Sleep(30 * time.Millisecond)
	}
	preview := text
	if len(preview) > 24 {
		preview = preview[:24] + "…"
	}
	m.broadcastAgentAction(t, AgentAction{
		Kind:  "type",
		X:     x,
		Y:     y,
		Label: fmt.Sprintf("Type %q", preview),
	})
	return m.InsertText(ctx, id, text)
}

type keyDef struct {
	key  string
	code string
	vk   int
	text string
}

var namedKeys = map[string]keyDef{
	"escape":     {key: "Escape", code: "Escape", vk: 27},
	"esc":        {key: "Escape", code: "Escape", vk: 27},
	"enter":      {key: "Enter", code: "Enter", vk: 13, text: "\r"},
	"return":     {key: "Enter", code: "Enter", vk: 13, text: "\r"},
	"tab":        {key: "Tab", code: "Tab", vk: 9},
	"backspace":  {key: "Backspace", code: "Backspace", vk: 8},
	"delete":     {key: "Delete", code: "Delete", vk: 46},
	"del":        {key: "Delete", code: "Delete", vk: 46},
	"space":      {key: " ", code: "Space", vk: 32, text: " "},
	" ":          {key: " ", code: "Space", vk: 32, text: " "},
	"arrowup":    {key: "ArrowUp", code: "ArrowUp", vk: 38},
	"up":         {key: "ArrowUp", code: "ArrowUp", vk: 38},
	"arrowdown":  {key: "ArrowDown", code: "ArrowDown", vk: 40},
	"down":       {key: "ArrowDown", code: "ArrowDown", vk: 40},
	"arrowleft":  {key: "ArrowLeft", code: "ArrowLeft", vk: 37},
	"left":       {key: "ArrowLeft", code: "ArrowLeft", vk: 37},
	"arrowright": {key: "ArrowRight", code: "ArrowRight", vk: 39},
	"right":      {key: "ArrowRight", code: "ArrowRight", vk: 39},
	"home":       {key: "Home", code: "Home", vk: 36},
	"end":        {key: "End", code: "End", vk: 35},
	"pageup":     {key: "PageUp", code: "PageUp", vk: 33},
	"pagedown":   {key: "PageDown", code: "PageDown", vk: 34},
}

// PressKey dispatches a keyDown + keyUp pair for a named key or single character (with optional modifiers).
func (m *Manager) PressKey(ctx context.Context, id, key string, modifiers []string) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	m.mu.Unlock()
	if !ok || t == nil {
		return ErrTabNotFound
	}

	mods := ParseModifiers(modifiers)
	lower := strings.ToLower(strings.TrimSpace(key))
	def, ok := namedKeys[lower]
	if !ok {
		k := strings.TrimSpace(key)
		if k == "" {
			return fmt.Errorf("key is required")
		}
		def = keyDef{key: k}
		if len(k) == 1 {
			ch := k[0]
			if ch >= 'a' && ch <= 'z' {
				def.code = "Key" + strings.ToUpper(k)
				def.vk = int(ch - 'a' + 'A')
			} else if ch >= 'A' && ch <= 'Z' {
				def.code = "Key" + k
				def.vk = int(ch)
			} else if ch >= '0' && ch <= '9' {
				def.code = "Digit" + k
				def.vk = int(ch)
			}
			if mods == 0 || mods == 8 {
				def.text = k
			}
		}
	}

	label := "Press " + def.key
	if len(modifiers) > 0 {
		label = "Press " + strings.Join(modifiers, "+") + "+" + def.key
	}
	m.broadcastAgentAction(t, AgentAction{
		Kind:  "key",
		Label: label,
	})

	if err := m.DispatchKey(ctx, id, KeyEvent{
		Type:                  "keyDown",
		Key:                   def.key,
		Code:                  def.code,
		Text:                  def.text,
		UnmodifiedText:        def.text,
		WindowsVirtualKeyCode: def.vk,
		NativeVirtualKeyCode:  def.vk,
		Modifiers:             mods,
	}); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	return m.DispatchKey(ctx, id, KeyEvent{
		Type:                  "keyUp",
		Key:                   def.key,
		Code:                  def.code,
		WindowsVirtualKeyCode: def.vk,
		NativeVirtualKeyCode:  def.vk,
		Modifiers:             mods,
	})
}

// CaptureScreenshotJPEG captures a JPEG screenshot (smaller for MCP vision payloads) and returns base64 + dimensions.
func (m *Manager) CaptureScreenshotJPEG(ctx context.Context, id string, quality int) (string, int, int, error) {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return "", 0, 0, ErrTabNotFound
	}
	if cdp == nil {
		return "", 0, 0, ErrBrowserClosed
	}

	if quality <= 0 || quality > 100 {
		quality = 82
	}

	m.broadcastAgentAction(t, AgentAction{
		Kind:  "screenshot",
		Label: "Captured screenshot",
	})

	var res struct {
		Data string `json:"data"`
	}
	if err := cdp.Call(ctx, t.sessionID, "Page.captureScreenshot", map[string]any{
		"format":  "jpeg",
		"quality": quality,
	}, &res); err != nil {
		return "", 0, 0, err
	}
	t.mu.RLock()
	w, h := t.width, t.height
	t.mu.RUnlock()
	return res.Data, w, h, nil
}

// InspectedElement describes a DOM element under a viewport coordinate for selection mode.
type InspectedElement struct {
	Tag       string  `json:"tag"`
	ID        string  `json:"id,omitempty"`
	ClassName string  `json:"className,omitempty"`
	Label     string  `json:"label"`
	Selector  string  `json:"selector"`
	Text      string  `json:"text,omitempty"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
}

// InspectElementAt returns the bounding box and selector info for the DOM element at (x, y).
func (m *Manager) InspectElementAt(ctx context.Context, id string, x, y float64) (*InspectedElement, error) {
	expr := fmt.Sprintf(`(() => {
		const el = document.elementFromPoint(%f, %f);
		if (!el) return null;
		const r = el.getBoundingClientRect();
		const tag = el.tagName.toLowerCase();
		const elId = el.id ? String(el.id) : '';
		const rawCls = typeof el.className === 'string' ? el.className : (el.className && el.className.baseVal ? el.className.baseVal : '');
		const classes = rawCls.trim().split(/\s+/).filter(Boolean).slice(0, 3);
		let label = tag;
		if (elId) label += '#' + elId;
		else if (classes.length > 0) label += '.' + classes.slice(0, 2).join('.');
		let selector = tag;
		if (elId) selector += '#' + CSS.escape(elId);
		else if (classes.length > 0) selector += '.' + classes.map(c => CSS.escape(c)).join('.');
		const text = (el.getAttribute('aria-label') || el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 120);
		return {
			tag,
			id: elId || undefined,
			className: classes.join(' ') || undefined,
			label,
			selector,
			text: text || undefined,
			x: r.left,
			y: r.top,
			width: r.width,
			height: r.height,
		};
	})()`, x, y)

	raw, err := m.Evaluate(ctx, id, expr)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var el InspectedElement
	if err := json.Unmarshal(b, &el); err != nil {
		return nil, err
	}
	return &el, nil
}

// BatchStep defines one action inside a browser_batch sequence.
type BatchStep struct {
	Action     string   `json:"action"`         // "click", "drag", "wheel", "type", "press", "wait", "eval", "navigate", "snapshot"
	Type       string   `json:"type,omitempty"` // alias for action
	X          *float64 `json:"x,omitempty"`
	Y          *float64 `json:"y,omitempty"`
	FromX      float64  `json:"fromX,omitempty"`
	FromY      float64  `json:"fromY,omitempty"`
	ToX        float64  `json:"toX,omitempty"`
	ToY        float64  `json:"toY,omitempty"`
	DeltaX     float64  `json:"deltaX,omitempty"`
	DeltaY     float64  `json:"deltaY,omitempty"`
	Ref        string   `json:"ref,omitempty"`
	Selector   string   `json:"selector,omitempty"`
	Button     string   `json:"button,omitempty"`
	ClickCount int      `json:"clickCount,omitempty"`
	Modifiers  []string `json:"modifiers,omitempty"`
	Steps      int      `json:"steps,omitempty"`
	DurationMs int      `json:"durationMs,omitempty"`
	Text       string   `json:"text,omitempty"`
	Key        string   `json:"key,omitempty"`
	Expression string   `json:"expression,omitempty"`
	URL        string   `json:"url,omitempty"`
	Ms         int      `json:"ms,omitempty"`
}

// BatchResult summarizes a multi-step browser_batch execution.
type BatchResult struct {
	TabID          string          `json:"tabId"`
	CompletedSteps int             `json:"completedSteps"`
	TotalSteps     int             `json:"totalSteps"`
	LastEval       any             `json:"lastEval,omitempty"`
	LastSnapshot   *SnapshotResult `json:"lastSnapshot,omitempty"`
	FailedStep     int             `json:"failedStep,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// Batch executes multiple browser actions sequentially in one tool call, stopping on first error.
func (m *Manager) Batch(ctx context.Context, id string, steps []BatchStep) BatchResult {
	res := BatchResult{
		TabID:      id,
		TotalSteps: len(steps),
	}
	for i, st := range steps {
		var stepErr error
		action := strings.ToLower(strings.TrimSpace(st.Action))
		if action == "" {
			action = strings.ToLower(strings.TrimSpace(st.Type))
		}
		switch action {
		case "click":
			_, _, stepErr = m.Click(ctx, id, ClickOptions{
				X:          st.X,
				Y:          st.Y,
				Ref:        st.Ref,
				Selector:   st.Selector,
				Button:     st.Button,
				ClickCount: st.ClickCount,
				Modifiers:  st.Modifiers,
			})
		case "drag":
			stepErr = m.Drag(ctx, id, DragOptions{
				FromX:      st.FromX,
				FromY:      st.FromY,
				ToX:        st.ToX,
				ToY:        st.ToY,
				Button:     st.Button,
				Steps:      st.Steps,
				DurationMs: st.DurationMs,
				Modifiers:  st.Modifiers,
			})
		case "wheel":
			var wx, wy float64
			if st.X != nil {
				wx = *st.X
			}
			if st.Y != nil {
				wy = *st.Y
			}
			stepErr = m.Wheel(ctx, id, wx, wy, st.DeltaX, st.DeltaY, st.Modifiers)
		case "type":
			stepErr = m.TypeText(ctx, id, st.Text, st.Ref, st.Selector)
		case "press":
			stepErr = m.PressKey(ctx, id, st.Key, st.Modifiers)
		case "navigate":
			stepErr = m.Navigate(ctx, id, st.URL)
		case "eval":
			var val any
			val, stepErr = m.Evaluate(ctx, id, st.Expression)
			if stepErr == nil {
				res.LastEval = val
			}
		case "snapshot":
			var snap SnapshotResult
			snap, stepErr = m.Snapshot(ctx, id)
			if stepErr == nil {
				res.LastSnapshot = &snap
			}
		case "wait":
			ms := st.Ms
			if ms <= 0 {
				ms = 250
			}
			if ms > 5000 {
				ms = 5000
			}
			select {
			case <-ctx.Done():
				stepErr = ctx.Err()
			case <-time.After(time.Duration(ms) * time.Millisecond):
			}
		default:
			stepErr = fmt.Errorf("unknown batch action %q", st.Action)
		}
		if stepErr != nil {
			res.FailedStep = i + 1
			res.Error = stepErr.Error()
			return res
		}
		res.CompletedSteps = i + 1
		if i < len(steps)-1 {
			time.Sleep(40 * time.Millisecond)
		}
	}
	return res
}
