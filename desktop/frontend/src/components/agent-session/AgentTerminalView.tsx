import React, { useCallback, useEffect, useRef, useState } from 'react';
import '@xterm/xterm/css/xterm.css';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import {
  TerminalEndpoint,
  TerminalList,
  TerminalOpen,
  TerminalClose,
  TerminalResize,
} from '../../../wailsjs/go/main/App';
import { terminal } from '../../../wailsjs/go/models';
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';
import { Plus, X, Send, RotateCw, Terminal as TerminalIcon } from '../common/icons';

export interface AgentTerminalViewProps {
  threadId: string;
  projectPath?: string;
  isFocused?: boolean;
  isLight?: boolean;
  onSendToChat?: (snippet: string) => void;
  className?: string;
}

export const AgentTerminalView: React.FC<AgentTerminalViewProps> = ({
  threadId,
  projectPath,
  isFocused = true,
  isLight = false,
  onSendToChat,
  className = '',
}) => {
  const [sessions, setSessions] = useState<terminal.SessionInfo[]>([]);
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [endpoint, setEndpoint] = useState<{ wsUrl: string; token: string } | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  const containerRef = useRef<HTMLDivElement>(null);
  const activeTermRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);

  // Fetch initial endpoint and sessions list
  useEffect(() => {
    let cancelled = false;

    async function init() {
      try {
        const ep = await TerminalEndpoint();
        if (cancelled) return;
        setEndpoint(ep);

        const list = await TerminalList(threadId);
        if (cancelled) return;

        if (list && list.length > 0) {
          setSessions(list);
          setActiveSessionId(list[0].id);
        } else {
          // No sessions yet: open default terminal 1
          const initial = await TerminalOpen(threadId, projectPath ?? '', 'Terminal 1', 100, 28);
          if (cancelled) return;
          setSessions([initial]);
          setActiveSessionId(initial.id);
        }
      } catch (err) {
        console.error('Failed to initialize terminal sessions:', err);
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    }

    void init();
    return () => {
      cancelled = true;
    };
  }, [threadId, projectPath]);

  const activeSession = sessions.find((s) => s.id === activeSessionId) ?? null;

  // Mount active session into xterm instance + loopback WebSocket
  useEffect(() => {
    if (!endpoint || !activeSession || !containerRef.current) return;

    const term = new Terminal({
      theme: isLight
        ? {
            background: '#f6f6f6',
            foreground: '#1e1e20',
            cursor: '#1e1e20',
            cursorAccent: '#ffffff',
            selectionBackground: 'rgba(0, 102, 204, 0.25)',
            black: '#000000',
            red: '#cd3131',
            green: '#00bc00',
            yellow: '#949800',
            blue: '#0451a5',
            magenta: '#bc05bc',
            cyan: '#0598bc',
            white: '#555555',
            brightBlack: '#666666',
            brightRed: '#cd3131',
            brightGreen: '#14ce14',
            brightYellow: '#b5ba00',
            brightBlue: '#0451a5',
            brightMagenta: '#bc05bc',
            brightCyan: '#0598bc',
            brightWhite: '#a5a5a5',
          }
        : {
            background: '#0B0A09',
            foreground: '#e6e6e6',
            cursor: '#ffffff',
            cursorAccent: '#0B0A09',
            selectionBackground: 'rgba(255, 255, 255, 0.2)',
            black: '#1e1e1e',
            red: '#f44747',
            green: '#4ec9b0',
            yellow: '#dcdcaa',
            blue: '#569cd6',
            magenta: '#c586c0',
            cyan: '#4dc4ff',
            white: '#d4d4d4',
            brightBlack: '#808080',
            brightRed: '#f44747',
            brightGreen: '#4ec9b0',
            brightYellow: '#dcdcaa',
            brightBlue: '#569cd6',
            brightMagenta: '#c586c0',
            brightCyan: '#4dc4ff',
            brightWhite: '#ffffff',
          },
      fontFamily: "'JetBrains Mono', 'Cascadia Code', Consolas, monospace",
      fontSize: 12.5,
      lineHeight: 1.35,
      cursorBlink: true,
      scrollback: 5000,
      allowProposedApi: true,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);

    const webLinksAddon = new WebLinksAddon((_, uri) => {
      void BrowserOpenURL(uri);
    });
    term.loadAddon(webLinksAddon);

    // Open terminal inside host container
    containerRef.current.innerHTML = '';
    term.open(containerRef.current);
    try {
      fitAddon.fit();
    } catch {
      // Container sizing guard
    }

    activeTermRef.current = term;
    fitAddonRef.current = fitAddon;

    // Connect loopback WebSocket
    const url = `${endpoint.wsUrl}?id=${encodeURIComponent(activeSession.id)}&token=${encodeURIComponent(endpoint.token)}`;
    const ws = new WebSocket(url);
    ws.binaryType = 'arraybuffer';
    wsRef.current = ws;

    ws.onopen = () => {
      try {
        fitAddon.fit();
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
        void TerminalResize(activeSession.id, term.cols, term.rows);
      } catch {
        // Safe resize
      }
      if (isFocused) {
        term.focus();
      }
    };

    ws.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(ev.data));
      } else if (typeof ev.data === 'string') {
        try {
          const msg = JSON.parse(ev.data) as { type?: string; code?: number };
          if (msg.type === 'exit') {
            setSessions((prev) =>
              prev.map((s) =>
                s.id === activeSession.id ? { ...s, running: false, exitCode: msg.code ?? 0 } : s
              )
            );
            term.write(`\r\n\x1b[2m[Process exited with code ${msg.code ?? 0}]\x1b[0m\r\n`);
          }
        } catch {
          // Ignore non-json frames
        }
      }
    };

    const dataDisposable = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data));
      }
    });

    // Ergonomic clipboard handling
    term.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown') return true;

      const isCtrlOrCmd = e.ctrlKey || e.metaKey;

      // Copy: Ctrl+C / Cmd+C when selection exists
      if (isCtrlOrCmd && !e.shiftKey && !e.altKey && (e.key === 'c' || e.key === 'C')) {
        if (term.hasSelection()) {
          void navigator.clipboard.writeText(term.getSelection());
          term.clearSelection();
          return false;
        }
        return true; // No selection -> let ^C pass to terminal
      }

      // Paste: Ctrl+V / Cmd+V
      if (isCtrlOrCmd && !e.shiftKey && !e.altKey && (e.key === 'v' || e.key === 'V')) {
        void navigator.clipboard.readText().then((pasted) => {
          if (pasted && ws.readyState === WebSocket.OPEN) {
            ws.send(new TextEncoder().encode(pasted));
          }
        });
        return false;
      }

      return true;
    });

    // ResizeObserver debounced to keep terminal layout crisp
    let resizeTimer: number | undefined;
    const observer = new ResizeObserver(() => {
      window.clearTimeout(resizeTimer);
      resizeTimer = window.setTimeout(() => {
        try {
          fitAddon.fit();
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
          }
          void TerminalResize(activeSession.id, term.cols, term.rows);
        } catch {
          // Safe fit
        }
      }, 50);
    });
    observer.observe(containerRef.current);

    // CLEANUP: Close WS and dispose terminal ONLY. Never call TerminalClose here!
    return () => {
      window.clearTimeout(resizeTimer);
      observer.disconnect();
      dataDisposable.dispose();
      ws.close();
      term.dispose();
      activeTermRef.current = null;
      fitAddonRef.current = null;
      wsRef.current = null;
    };
  }, [endpoint, activeSession?.id, isLight]);

  // Handle focus changes
  useEffect(() => {
    if (isFocused && activeTermRef.current) {
      try {
        fitAddonRef.current?.fit();
        if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
          wsRef.current.send(
            JSON.stringify({
              type: 'resize',
              cols: activeTermRef.current.cols,
              rows: activeTermRef.current.rows,
            })
          );
        }
      } catch {
        // Safe fit
      }
      activeTermRef.current.focus();
    }
  }, [isFocused]);

  // Send to chat: selection or up to last 40 non-empty lines, max 6000 chars
  const handleSendToChat = useCallback(() => {
    if (!activeTermRef.current || !activeSession) return;
    const term = activeTermRef.current;
    let text = term.getSelection().trim();

    if (!text) {
      const buf = term.buffer.active;
      const lines: string[] = [];
      const maxLines = 40;
      for (let i = buf.length - 1; i >= 0 && lines.length < maxLines; i--) {
        const line = buf.getLine(i)?.translateToString(true) ?? '';
        if (lines.length === 0 && !line.trim()) continue;
        lines.unshift(line);
      }
      while (lines.length > 0 && !lines[lines.length - 1].trim()) {
        lines.pop();
      }
      text = lines.join('\n');
    }

    if (text.length > 6000) {
      text = text.slice(text.length - 6000);
    }

    if (!text.trim()) return;

    const snippet = `Terminal output (\`${activeSession.title}\`):\n\`\`\`text\n${text}\n\`\`\``;
    onSendToChat?.(snippet);
  }, [activeSession, onSendToChat]);

  // Add new terminal tab
  const handleNewTab = async () => {
    try {
      const nextNum = sessions.length + 1;
      const title = `Terminal ${nextNum}`;
      const newSess = await TerminalOpen(threadId, projectPath ?? '', title, 100, 28);
      setSessions((prev) => [...prev, newSess]);
      setActiveSessionId(newSess.id);
    } catch (err) {
      console.error('Failed to create terminal session:', err);
    }
  };

  // Close terminal tab
  const handleCloseTab = async (e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    try {
      await TerminalClose(id);
      const remaining = sessions.filter((s) => s.id !== id);
      setSessions(remaining);
      if (activeSessionId === id) {
        if (remaining.length > 0) {
          setActiveSessionId(remaining[remaining.length - 1].id);
        } else {
          setActiveSessionId(null);
        }
      }
    } catch (err) {
      console.error('Failed to close terminal session:', err);
    }
  };

  // Restart dead terminal session
  const handleRestart = async () => {
    if (!activeSession) return;
    try {
      const deadId = activeSession.id;
      const deadTitle = activeSession.title;
      await TerminalClose(deadId);
      const newSess = await TerminalOpen(threadId, projectPath ?? '', deadTitle, 100, 28);
      setSessions((prev) => prev.map((s) => (s.id === deadId ? newSess : s)));
      setActiveSessionId(newSess.id);
    } catch (err) {
      console.error('Failed to restart terminal session:', err);
    }
  };

  if (isLoading) {
    return (
      <div className={`flex items-center justify-center h-full w-full ${className}`}>
        <div className="flex items-center gap-2 text-xs opacity-50 font-mono">
          <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse" />
          <span>Starting terminal session…</span>
        </div>
      </div>
    );
  }

  return (
    <div
      className={`flex flex-col h-full w-full min-h-0 select-none overflow-hidden ${
        isLight ? 'bg-[#f6f6f6]' : 'bg-[#0B0A09]'
      } ${className}`}
    >
      {/* Compact flat tab bar */}
      <div
        className={`h-[36px] shrink-0 px-2.5 flex items-center justify-between gap-2 border-b ${
          isLight
            ? 'border-black/[0.06] bg-transparent'
            : 'border-white/[0.06] bg-transparent'
        }`}
      >
        {/* Left: Shell tabs & new tab + button */}
        <div className="flex items-center gap-1 min-w-0 overflow-x-auto no-scrollbar py-1">
          {sessions.map((sess) => {
            const isActive = sess.id === activeSessionId;
            return (
              <button
                key={sess.id}
                type="button"
                onClick={() => setActiveSessionId(sess.id)}
                className={`group flex items-center gap-2 px-2.5 py-1 rounded-[8px] text-[12px] transition-all duration-150 cursor-pointer shrink-0 ${
                  isActive
                    ? isLight
                      ? 'bg-black/[0.06] text-black font-medium'
                      : 'bg-white/[0.08] text-white font-medium'
                    : isLight
                    ? 'text-black/45 hover:text-black/80 hover:bg-black/[0.03]'
                    : 'text-white/45 hover:text-white/80 hover:bg-white/[0.04]'
                }`}
                title={`${sess.title} (${sess.shell})`}
              >
                {/* Live subtle dot or exited badge */}
                {sess.running ? (
                  <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${isActive ? 'bg-white/80' : 'bg-white/40'}`} />
                ) : (
                  <span className="text-[9.5px] px-1 py-0.2 rounded font-mono bg-white/[0.06] text-white/45 leading-none">
                    exit {sess.exitCode ?? 0}
                  </span>
                )}

                <span className="truncate max-w-[120px]">{sess.title}</span>

                {/* Close tab × button */}
                <span
                  role="button"
                  tabIndex={0}
                  onClick={(e) => handleCloseTab(e, sess.id)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      void handleCloseTab(e as unknown as React.MouseEvent, sess.id);
                    }
                  }}
                  className={`w-3.5 h-3.5 rounded-full flex items-center justify-center opacity-40 group-hover:opacity-100 transition-opacity ${
                    isLight ? 'hover:bg-black/10' : 'hover:bg-white/15'
                  }`}
                  title="Close terminal"
                >
                  <X size={10} strokeWidth={2.2} />
                </span>
              </button>
            );
          })}

          <button
            type="button"
            onClick={handleNewTab}
            className={`w-[26px] h-[26px] rounded-[7px] flex items-center justify-center transition-all duration-150 cursor-pointer shrink-0 ${
              isLight
                ? 'text-black/45 hover:text-black hover:bg-black/[0.05]'
                : 'text-white/45 hover:text-white hover:bg-white/[0.06]'
            }`}
            title="New terminal tab"
            aria-label="New terminal tab"
          >
            <Plus size={13} strokeWidth={2} />
          </button>
        </div>

        {/* Right action buttons: Restart (if exited) & Send to chat */}
        <div className="flex items-center gap-1.5 shrink-0">
          {activeSession && !activeSession.running && (
            <button
              type="button"
              onClick={handleRestart}
              className={`flex items-center gap-1.5 px-2.5 py-1 rounded-[7px] text-[11px] font-medium transition-all active:scale-95 cursor-pointer border ${
                isLight
                  ? 'bg-black/[0.05] hover:bg-black/[0.1] text-black/75 border-black/10'
                  : 'bg-white/[0.06] hover:bg-white/[0.12] text-white/75 hover:text-white border-white/10'
              }`}
              title="Restart session in same directory"
            >
              <RotateCw size={11} strokeWidth={2} />
              <span>Restart</span>
            </button>
          )}

          <button
            type="button"
            onClick={handleSendToChat}
            disabled={!activeSession}
            className={`flex items-center gap-1.5 px-2.5 py-1 rounded-[7px] text-[11px] font-medium transition-all duration-150 shadow-xs cursor-pointer active:scale-95 border ${
              isLight
                ? 'bg-black/[0.06] hover:bg-black/[0.12] text-black/85 border-black/10'
                : 'bg-white/[0.08] hover:bg-white/[0.14] text-white/90 border-white/[0.08]'
            }`}
            title="Send selection or recent output to active chat"
          >
            <Send size={11} strokeWidth={2} />
            <span>Send to chat</span>
          </button>
        </div>
      </div>

      {/* Terminal Viewport */}
      <div className="flex-1 min-h-0 relative px-2 py-1 overflow-hidden">
        {sessions.length > 0 ? (
          <div ref={containerRef} className="w-full h-full min-h-0" />
        ) : (
          <div className="w-full h-full flex flex-col items-center justify-center gap-3 text-center p-6 select-none opacity-60">
            <TerminalIcon size={24} strokeWidth={1.5} />
            <div className="text-xs">No active terminal sessions</div>
            <button
              type="button"
              onClick={handleNewTab}
              className={`px-3 py-1.5 rounded-[8px] text-xs font-medium border ${
                isLight
                  ? 'bg-black/5 hover:bg-black/10 border-black/10'
                  : 'bg-white/10 hover:bg-white/15 border-white/15'
              }`}
            >
              Open Terminal
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

export default AgentTerminalView;
