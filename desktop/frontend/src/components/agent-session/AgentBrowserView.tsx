import { MaterialIcon } from '../common/icons';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';
import { servers } from '../../../wailsjs/go/models';
import { Maximize } from '../common/icons';

export interface AgentBrowserViewProps {
  threadId?: string;
  detectedServers?: servers.Server[];
  isFocused?: boolean;
  isAnimating?: boolean;
  onFocusCard?: () => void;
  toolbarHost?: HTMLElement | null;
  immersive?: boolean;
  onEnterImmersive?: () => void;
  className?: string;
}

export function smartNormalizeUrl(input: string, fallbackPort?: number): string {
  const trimmed = input.trim();
  if (!trimmed) {
    if (fallbackPort) return `http://localhost:${fallbackPort}`;
    return 'about:blank';
  }

  // Bare port like 5173 or :5173
  if (/^:?\d+$/.test(trimmed)) {
    const port = trimmed.replace(/^:/, '');
    return `http://localhost:${port}`;
  }

  // Starts with scheme like http:// or https://
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//i.test(trimmed)) {
    return trimmed;
  }

  // localhost with optional port/path
  if (/^localhost(:\d+)?(\/.*)?$/i.test(trimmed)) {
    return `http://${trimmed}`;
  }

  // IP address with optional port/path
  if (/^(\d{1,3}\.){3}\d{1,3}(:\d+)?(\/.*)?$/.test(trimmed)) {
    return `http://${trimmed}`;
  }

  // Standard domain like github.com, apple.com/design, foo.dev
  if (/^[a-zA-Z0-9-]+(\.[a-zA-Z0-9-]+)+(\/.*)?$/i.test(trimmed)) {
    return `https://${trimmed}`;
  }

  // Search query (e.g. "how to do X in vite")
  return `https://www.google.com/search?q=${encodeURIComponent(trimmed)}`;
}

export const AgentBrowserView: React.FC<AgentBrowserViewProps> = ({
  threadId,
  detectedServers = [],
  isFocused = true,
  isAnimating = false,
  onFocusCard,
  toolbarHost,
  immersive = false,
  onEnterImmersive,
  className = '',
}) => {
  // Find primary web dev server port
  const webServers = detectedServers.filter((s) => !s.agent && s.port > 0);
  const primaryServer = webServers[0];
  const defaultUrl = primaryServer ? `http://localhost:${primaryServer.port}` : 'about:blank';

  const [currentUrl, setCurrentUrl] = useState<string>(defaultUrl);
  const [inputUrl, setInputUrl] = useState<string>(defaultUrl === 'about:blank' ? '' : defaultUrl);
  const [reloadNonce, setReloadNonce] = useState<number>(0);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [isBlocked, setIsBlocked] = useState<boolean>(false);
  const [historyStack, setHistoryStack] = useState<string[]>([defaultUrl]);
  const [historyIndex, setHistoryIndex] = useState<number>(0);

  const iframeRef = useRef<HTMLIFrameElement>(null);
  const blockTimerRef = useRef<number | null>(null);

  // When card regains focus, dispatch synthetic resize to refresh WebGL viewport buffer
  useEffect(() => {
    if (isFocused && iframeRef.current) {
      const timer = setTimeout(() => {
        try {
          iframeRef.current?.contentWindow?.dispatchEvent(new Event('resize'));
        } catch {
          // Cross-origin safety
        }
      }, 60);
      return () => clearTimeout(timer);
    }
  }, [isFocused]);

  // If a dev server was newly detected and we were on about:blank, auto-navigate to it
  useEffect(() => {
    if (primaryServer && (currentUrl === 'about:blank' || !currentUrl)) {
      const url = `http://localhost:${primaryServer.port}`;
      setCurrentUrl(url);
      setInputUrl(url);
      setHistoryStack([url]);
      setHistoryIndex(0);
    }
  }, [primaryServer, currentUrl]);

  // Keep inputUrl in sync when currentUrl changes
  useEffect(() => {
    setInputUrl(currentUrl === 'about:blank' ? '' : currentUrl);
  }, [currentUrl]);

  // Handle iframe load detection & block probe
  useEffect(() => {
    if (blockTimerRef.current !== null) {
      window.clearTimeout(blockTimerRef.current);
      blockTimerRef.current = null;
    }

    if (!currentUrl || currentUrl === 'about:blank') {
      setIsLoading(false);
      setIsBlocked(false);
      return;
    }

    setIsLoading(true);
    setIsBlocked(false);

    blockTimerRef.current = window.setTimeout(() => {
      setIsLoading(false);
      const frame = iframeRef.current;
      if (!frame) return;
      try {
        const doc = frame.contentDocument;
        if (doc && doc.body && doc.body.childElementCount === 0) {
          setIsBlocked(true);
        }
      } catch {
        // Cross-origin means it rendered successfully
      }
    }, 3200);

    return () => {
      if (blockTimerRef.current !== null) {
        window.clearTimeout(blockTimerRef.current);
      }
    };
  }, [currentUrl, reloadNonce]);

  const navigateTo = useCallback(
    (target: string) => {
      const normalized = smartNormalizeUrl(target, primaryServer?.port);
      setCurrentUrl(normalized);
      setInputUrl(normalized === 'about:blank' ? '' : normalized);
      setHistoryStack((prev) => [...prev.slice(0, historyIndex + 1), normalized]);
      setHistoryIndex((prev) => prev + 1);
    },
    [primaryServer, historyIndex],
  );

  const handleInputKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      navigateTo(inputUrl);
    }
  };

  const handleBack = () => {
    if (historyIndex > 0) {
      const newIdx = historyIndex - 1;
      setHistoryIndex(newIdx);
      const prevUrl = historyStack[newIdx];
      setCurrentUrl(prevUrl);
      setInputUrl(prevUrl === 'about:blank' ? '' : prevUrl);
    }
  };

  const handleForward = () => {
    if (historyIndex < historyStack.length - 1) {
      const newIdx = historyIndex + 1;
      setHistoryIndex(newIdx);
      const nextUrl = historyStack[newIdx];
      setCurrentUrl(nextUrl);
      setInputUrl(nextUrl === 'about:blank' ? '' : nextUrl);
    }
  };

  const handleReload = () => {
    setReloadNonce((prev) => prev + 1);
  };

  const handleOpenExternal = () => {
    if (currentUrl && currentUrl !== 'about:blank') {
      void BrowserOpenURL(currentUrl);
    } else if (primaryServer) {
      void BrowserOpenURL(`http://localhost:${primaryServer.port}`);
    }
  };

  const isLocalhost = currentUrl.includes('localhost') || currentUrl.includes('127.0.0.1');
  const shouldPause = !isFocused;

  const toolbar = (
      <div className="flex items-center gap-2 flex-1 min-w-0">
        <div className="flex items-center gap-0.5 shrink-0">
          {[
            { title: 'Back', icon: 'arrow_back', enabled: historyIndex > 0, run: handleBack, spin: false },
            { title: 'Forward', icon: 'arrow_forward', enabled: historyIndex < historyStack.length - 1, run: handleForward, spin: false },
            { title: 'Reload', icon: 'refresh', enabled: true, run: handleReload, spin: isLoading },
          ].map((action) => (
            <button
              key={action.title}
              type="button"
              title={action.title}
              aria-label={action.title}
              disabled={!action.enabled}
              onClick={action.run}
              className={`w-[28px] h-[28px] rounded-full flex items-center justify-center transition-all duration-150 ${
                action.enabled
                  ? 'text-white/70 hover:text-white hover:bg-white/[0.08] active:scale-90 cursor-pointer'
                  : 'text-white/20 cursor-default'
              }`}
            >
              <MaterialIcon name={action.icon} className={`text-[16px] ${action.spin ? 'animate-spin' : ''}`} />
            </button>
          ))}
        </div>

        <div className="flex-1 min-w-[120px] flex justify-center">
          <div className="w-full max-w-[560px] h-[30px] px-3 rounded-full bg-white/[0.06] hover:bg-white/[0.085] focus-within:bg-white/[0.1] focus-within:shadow-[0_0_0_1.5px_rgba(10,132,255,0.65)] flex items-center gap-2 transition-all duration-200">
            <MaterialIcon name={isLocalhost ? 'computer' : currentUrl.startsWith('https') ? 'lock' : 'search'} className="text-[13px] text-white/40 shrink-0" />
            <input
              type="text"
              value={inputUrl}
              placeholder={primaryServer ? `localhost:${primaryServer.port} or search Google…` : 'Enter address or search Google…'}
              onChange={(e) => setInputUrl(e.target.value)}
              onKeyDown={handleInputKeyDown}
              onFocus={(e) => e.target.select()}
              className="w-full bg-transparent text-[12.5px] font-medium font-(family-name:--app-font) text-white/90 placeholder:text-white/35 focus:outline-none tracking-tight select-text text-center focus:text-left"
            />
            {isLoading && <span className="w-2.5 h-2.5 rounded-full border border-white/60 border-t-transparent animate-spin shrink-0" />}
          </div>
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          {webServers.length > 0 && (
            <div className="h-[28px] p-[3px] rounded-full bg-white/[0.06] flex items-center gap-[2px]">
              {webServers.map((s) => {
                const active = currentUrl.includes(`:${s.port}`);
                return (
                  <button
                    key={s.port}
                    type="button"
                    title={`Switch to dev server on port ${s.port}`}
                    onClick={() => navigateTo(`http://localhost:${s.port}`)}
                    className={`h-[22px] px-2.5 rounded-full flex items-center gap-1.5 text-[11px] font-medium font-(family-name:--app-font) tabular-nums transition-all duration-150 cursor-pointer ${
                      active ? 'bg-white/[0.16] text-white shadow-sm' : 'text-white/55 hover:text-white'
                    }`}
                  >
                    <span className={`w-1.5 h-1.5 rounded-full ${active ? 'bg-emerald-400' : 'bg-white/30'}`} />
                    {s.port}
                  </button>
                );
              })}
            </div>
          )}

          {onEnterImmersive && (
            <button
              type="button"
              title="Full screen browser"
              aria-label="Full screen browser"
              onClick={onEnterImmersive}
              className="w-[28px] h-[28px] rounded-full flex items-center justify-center text-white/70 hover:text-white bg-white/[0.06] hover:bg-white/[0.12] active:scale-90 transition-all duration-150 cursor-pointer"
            >
              <Maximize size={13} strokeWidth={2} />
            </button>
          )}

          <button
            type="button"
            title="Open in external browser"
            aria-label="Open in external browser"
            onClick={handleOpenExternal}
            className="w-[28px] h-[28px] rounded-full flex items-center justify-center text-white/70 hover:text-white bg-white/[0.06] hover:bg-white/[0.12] active:scale-90 transition-all duration-150 cursor-pointer"
          >
            <MaterialIcon name="open_in_new" className="text-[14px]" />
          </button>
        </div>
      </div>
  );

  return (
    <div className={`flex flex-col h-full w-full min-h-0 select-none ${className}`}>
      {immersive ? null : toolbarHost ? (
        createPortal(toolbar, toolbarHost)
      ) : (
        <div className="flex items-center pb-2 pt-0.5 px-0.5 shrink-0">{toolbar}</div>
      )}

      <div
        data-browser-viewport
        className={`flex-1 min-h-0 relative overflow-hidden bg-[#121316] ${
          immersive ? '' : 'rounded-[14px] shadow-[0_0_0_1px_rgba(255,255,255,0.08),0_12px_32px_-12px_rgba(0,0,0,0.6)]'
        }`}
        style={{
          contain: 'strict',
          transform: 'translate3d(0, 0, 0)',
        }}
      >
        {currentUrl && currentUrl !== 'about:blank' ? (
          <>
            {/* Unfocused Click Shield: captures clicks to focus card and prevents mousemove/drag from hijacking 3D WebGL */}
            {!isFocused && (
              <div
                onClick={(e) => {
                  e.stopPropagation();
                  onFocusCard?.();
                }}
                className="absolute inset-0 z-30 cursor-pointer bg-black/10 transition-opacity"
                title="Click to focus card"
              />
            )}

            {/* Animation Shield: disables pointer events & prevents layout jitter during deck/grid fly transitions */}
            {isAnimating && (
              <div className="absolute inset-0 z-40 pointer-events-none bg-transparent" />
            )}

            {/* Paused Overlay: shown when unfocused with Power Save enabled */}
            {shouldPause && (
              <div
                onClick={(e) => {
                  e.stopPropagation();
                  onFocusCard?.();
                }}
                className="absolute inset-0 z-25 bg-[#121316]/92 backdrop-blur-md flex flex-col items-center justify-center text-center p-6 cursor-pointer group transition-all duration-200"
              >
                <div className="w-12 h-12 rounded-2xl bg-white/[0.06] border border-white/10 flex items-center justify-center mb-3 shadow-xl group-hover:scale-105 group-hover:bg-white/[0.1] transition-all duration-200">
                  <MaterialIcon name="view_in_ar" className="text-emerald-400 text-2xl"/>
                </div>
                <div className="flex items-center gap-1.5 px-2.5 py-0.5 rounded-full bg-emerald-500/15 border border-emerald-500/30 text-emerald-300 text-[11px] font-mono font-medium mb-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" />
                  <span>3D Preview Paused • State Preserved</span>
                </div>
                <p className="text-[12px] font-medium font-(family-name:--app-font) text-white/50 max-w-xs mb-3 leading-relaxed">
                  WebGL rendering halted to maintain silky 120 FPS across your agent cards.
                </p>
                <div className="px-3.5 py-1.5 rounded-[8px] bg-white/[0.08] hover:bg-white/15 border border-white/15 text-white text-[12px] font-medium flex items-center gap-1.5 transition-all shadow-md group-hover:border-white/30">
                  <span>Click to Resume Preview</span>
                  <MaterialIcon name="play_arrow" className="text-[14px]"/>
                </div>
              </div>
            )}

            <iframe
              key={`${currentUrl}-${reloadNonce}`}
              ref={iframeRef}
              src={currentUrl}
              title="Agent Web Preview"
              onLoad={() => setIsLoading(false)}
              className="w-full h-full border-none bg-white"
              style={{
                visibility: shouldPause ? 'hidden' : 'visible',
                pointerEvents: isFocused && !isAnimating ? 'auto' : 'none',
              }}
              sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-modals allow-downloads"
              allow="clipboard-read; clipboard-write; fullscreen; camera; microphone"
            />

            {isLoading && (
              <div className="absolute top-0 inset-x-0 h-[2.5px] bg-black/10 overflow-hidden z-10">
                <div className="h-full w-1/3 bg-[#007AFF] animate-[browser-progress_1.1s_ease-in-out_infinite]" />
              </div>
            )}

            {isBlocked && (
              <div className="absolute inset-0 bg-[#16181d] flex flex-col items-center justify-center text-center p-6 z-20">
                <div className="w-10 h-10 rounded-xl bg-white/5 border border-white/10 flex items-center justify-center mb-3">
                  <MaterialIcon name="public_off" className="text-white/60 text-xl"/>
                </div>
                <h4 className="text-[13.5px] font-semibold font-(family-name:--app-font) text-white/90 mb-1">
                  Site embedding restricted
                </h4>
                <p className="text-[12px] text-white/50 max-w-sm mb-4 leading-relaxed font-(family-name:--app-font)">
                  This site restricts iframe embedding for security reasons. You can view it directly in your browser.
                </p>
                <button
                  type="button"
                  onClick={handleOpenExternal}
                  className="px-3.5 py-1.5 rounded-[6px] bg-[#007AFF] hover:bg-[#0A84FF] text-white text-[12px] font-medium flex items-center gap-1.5 shadow-lg active:scale-95 transition-all cursor-pointer"
                >
                  <span>Open in Default Browser</span>
                  <MaterialIcon name="open_in_new" className="text-[14px]"/>
                </button>
              </div>
            )}
          </>
        ) : (
          /* Empty / Start Surface */
          <div className="w-full h-full bg-[#121316] flex flex-col items-center justify-center text-center p-6 select-none">
            <div className="w-12 h-12 rounded-2xl bg-white/[0.05] border border-white/10 flex items-center justify-center mb-3.5 shadow-xl">
              <MaterialIcon name="language" className="text-white/60 text-2xl"/>
            </div>
            <h3 className="text-[15px] font-semibold font-(family-name:--app-font) text-white/90 tracking-tight mb-1">
              Web Preview
            </h3>
            <p className="text-[12px] font-medium font-(family-name:--app-font) text-white/45 max-w-xs mb-5 leading-relaxed">
              {primaryServer
                ? `Dev server running on localhost:${primaryServer.port}`
                : 'Start a dev server or enter an address above to preview.'}
            </p>

            {primaryServer && (
              <button
                type="button"
                onClick={() => navigateTo(`http://localhost:${primaryServer.port}`)}
                className="px-4 py-2 rounded-[8px] bg-emerald-500/20 hover:bg-emerald-500/30 border border-emerald-400/40 text-emerald-200 text-[12.5px] font-medium flex items-center gap-2 shadow-lg active:scale-95 transition-all cursor-pointer"
              >
                <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse" />
                <span>Launch localhost:{primaryServer.port}</span>
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default AgentBrowserView;
