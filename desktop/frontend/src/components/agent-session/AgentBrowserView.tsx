import { MaterialIcon } from '../common/icons';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';
import { ListProjects } from '../../../wailsjs/go/main/App';
import { servers } from '../../../wailsjs/go/models';
import { Maximize } from '../common/icons';
import { useWebBookmarks, getDomain, getFaviconUrl, type WebBookmark } from './useWebBookmarks';
import { BookmarkModal } from './BookmarkModal';
import { CdpBrowserSurface } from '../browser/CdpBrowserSurface';

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
  targetUrl?: string;
  targetUrlNonce?: number;
  projectName?: string;
  projectPath?: string;
}

export function smartNormalizeUrl(input: string, fallbackPort?: number): string {
  const trimmed = input.trim();
  if (!trimmed) {
    return 'https://www.google.com';
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
  targetUrl,
  targetUrlNonce,
  projectName,
  projectPath,
}) => {
  const {
    bookmarks,
    visibleBookmarks,
    addBookmark,
    updateBookmark,
    deleteBookmark,
    isBookmarked,
    getBookmarkByUrl,
  } = useWebBookmarks(projectName);

  const [availableProjects, setAvailableProjects] = useState<string[]>([]);
  useEffect(() => {
    ListProjects()
      .then((list) => {
        if (list) {
          setAvailableProjects(list.map((p) => p.name).filter(Boolean));
        }
      })
      .catch(() => {});
  }, []);

  const [isBookmarkModalOpen, setIsBookmarkModalOpen] = useState(false);
  const [editingBookmark, setEditingBookmark] = useState<WebBookmark | null>(null);

  // Find web dev servers
  const webServers = detectedServers.filter((s) => !s.agent && s.port > 0);
  const primaryServer = webServers[0];
  const defaultUrl = targetUrl || (primaryServer ? `http://localhost:${primaryServer.port}` : 'https://www.google.com');

  const [currentUrl, setCurrentUrl] = useState<string>(defaultUrl);
  const [inputUrl, setInputUrl] = useState<string>(defaultUrl);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [canGoBack, setCanGoBack] = useState<boolean>(false);
  const [canGoForward, setCanGoForward] = useState<boolean>(false);
  const [browserError, setBrowserError] = useState<string | null>(null);

  const isInputEditingRef = useRef<boolean>(false);
  const surfaceActionRef = useRef<{
    back(): void;
    forward(): void;
    reload(): void;
    navigate(url: string): void;
  } | null>(null);

  const [islandSlot, setIslandSlot] = useState<HTMLElement | null>(() =>
    typeof document !== 'undefined' ? document.getElementById('dynamic-island-browser-slot') : null,
  );

  useEffect(() => {
    if (islandSlot) return;
    const el = document.getElementById('dynamic-island-browser-slot');
    if (el) {
      setIslandSlot(el);
      return;
    }
    const timer = setInterval(() => {
      const found = document.getElementById('dynamic-island-browser-slot');
      if (found) {
        setIslandSlot(found);
        clearInterval(timer);
      }
    }, 60);
    return () => clearInterval(timer);
  }, [islandSlot]);

  const navigateTo = useCallback(
    (target: string) => {
      const normalized = smartNormalizeUrl(target, primaryServer?.port);
      setCurrentUrl(normalized);
      setInputUrl(normalized === 'about:blank' ? '' : normalized);
      surfaceActionRef.current?.navigate(normalized);
    },
    [primaryServer],
  );

  // Navigate when external targetUrl / targetUrlNonce changes (e.g. clicking Git in rail)
  const lastTargetNonce = useRef<number | undefined>(targetUrlNonce);
  useEffect(() => {
    if (targetUrl && targetUrlNonce !== lastTargetNonce.current) {
      lastTargetNonce.current = targetUrlNonce;
      navigateTo(targetUrl);
    }
  }, [targetUrl, targetUrlNonce, navigateTo]);

  const handleInputKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      navigateTo(inputUrl);
    }
  };

  const handleBack = () => {
    surfaceActionRef.current?.back();
  };

  const handleForward = () => {
    surfaceActionRef.current?.forward();
  };

  const handleReload = () => {
    surfaceActionRef.current?.reload();
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

  const handleStarClick = () => {
    const existing = getBookmarkByUrl(currentUrl);
    if (existing) {
      setEditingBookmark(existing);
    } else {
      setEditingBookmark({
        id: '',
        title: inputUrl ? getDomain(inputUrl) : '',
        url: currentUrl && currentUrl !== 'about:blank' ? currentUrl : 'https://',
        project: projectName || null,
        createdAt: 0,
      });
    }
    setIsBookmarkModalOpen(true);
  };

  const handleAddBookmarkClick = () => {
    setEditingBookmark({
      id: '',
      title: '',
      url: currentUrl && currentUrl !== 'about:blank' ? currentUrl : 'https://',
      project: projectName || null,
      createdAt: 0,
    });
    setIsBookmarkModalOpen(true);
  };

  const handleEditBookmark = (bm: WebBookmark) => {
    setEditingBookmark(bm);
    setIsBookmarkModalOpen(true);
  };

  const handleSaveBookmark = (data: { title: string; url: string; project?: string | null }) => {
    if (editingBookmark?.id) {
      updateBookmark(editingBookmark.id, data);
    } else {
      addBookmark(data);
    }
  };

  const handleDeleteBookmark = () => {
    if (editingBookmark?.id) {
      deleteBookmark(editingBookmark.id);
    }
  };

  const isCurrentBookmarked = isBookmarked(currentUrl);
  const effectiveHost = islandSlot || toolbarHost;

  const toolbar = (
    <div className="flex items-center gap-1.5 w-full h-full min-w-0 select-none">
      <div className="flex items-center gap-0.5 shrink-0">
        {[
          { title: 'Back', icon: 'arrow_back', enabled: canGoBack, run: handleBack, spin: false },
          { title: 'Forward', icon: 'arrow_forward', enabled: canGoForward, run: handleForward, spin: false },
          { title: 'Reload', icon: 'refresh', enabled: true, run: handleReload, spin: isLoading },
        ].map((action) => (
          <button
            key={action.title}
            type="button"
            title={action.title}
            aria-label={action.title}
            disabled={!action.enabled}
            onClick={action.run}
            className={`w-[26px] h-[26px] rounded-full flex items-center justify-center transition-all duration-150 ${
              action.enabled
                ? 'text-white/70 hover:text-white hover:bg-white/[0.1] active:scale-90 cursor-pointer'
                : 'text-white/20 cursor-default'
            }`}
          >
            <MaterialIcon name={action.icon} className={`text-[15px] ${action.spin ? 'animate-spin' : ''}`} />
          </button>
        ))}
      </div>

      <div className="flex-1 min-w-[100px] flex justify-center">
        <div className="w-full max-w-[480px] h-[26px] px-2.5 rounded-full bg-white/[0.08] hover:bg-white/[0.12] focus-within:bg-white/[0.14] focus-within:ring-1 focus-within:ring-blue-400/50 flex items-center gap-1.5 transition-all duration-150">
          <MaterialIcon
            name={isLocalhost ? 'computer' : currentUrl.startsWith('https') ? 'lock' : 'search'}
            className="text-[12px] text-white/45 shrink-0"
          />
          <input
            type="text"
            value={inputUrl}
            placeholder="Search or enter URL…"
            onChange={(e) => setInputUrl(e.target.value)}
            onKeyDown={handleInputKeyDown}
            onFocus={(e) => {
              isInputEditingRef.current = true;
              e.target.select();
            }}
            onBlur={() => {
              isInputEditingRef.current = false;
            }}
            className="w-full bg-transparent text-[11.5px] font-medium font-(family-name:--app-font) text-white/90 placeholder:text-white/35 focus:outline-none tracking-tight select-text text-center focus:text-left"
          />
          {isLoading && <span className="w-2.5 h-2.5 rounded-full border border-white/60 border-t-transparent animate-spin shrink-0" />}
          {currentUrl && currentUrl !== 'about:blank' && (
            <button
              type="button"
              title={isCurrentBookmarked ? 'Edit bookmark for this page' : 'Bookmark this page'}
              onClick={handleStarClick}
              className={`p-0.5 rounded-full hover:bg-white/10 transition-colors shrink-0 cursor-pointer ${
                isCurrentBookmarked ? 'text-amber-400' : 'text-white/35 hover:text-white/80'
              }`}
            >
              <MaterialIcon name={isCurrentBookmarked ? 'star' : 'star_border'} className="text-[14px]" />
            </button>
          )}
        </div>
      </div>

      <div className="flex items-center gap-1 shrink-0">
        {onEnterImmersive && (
          <button
            type="button"
            title="Full screen browser"
            aria-label="Full screen browser"
            onClick={onEnterImmersive}
            className="w-[26px] h-[26px] rounded-full flex items-center justify-center text-white/70 hover:text-white hover:bg-white/10 active:scale-90 transition-all duration-150 cursor-pointer"
          >
            <Maximize size={12} strokeWidth={2} />
          </button>
        )}

        <button
          type="button"
          title="Open in external browser"
          aria-label="Open in external browser"
          onClick={handleOpenExternal}
          className="w-[26px] h-[26px] rounded-full flex items-center justify-center text-white/70 hover:text-white hover:bg-white/10 active:scale-90 transition-all duration-150 cursor-pointer"
        >
          <MaterialIcon name="open_in_new" className="text-[13px]" />
        </button>
      </div>
    </div>
  );

  return (
    <div className={`flex flex-col h-full w-full min-h-0 select-none ${className}`}>
      {immersive ? null : effectiveHost ? (
        createPortal(toolbar, effectiveHost)
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
            {/* Unfocused Click Shield: captures clicks to focus card and prevents mousemove/drag from hijacking */}
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

            {/* Paused Overlay: shown when unfocused */}
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
                  <span>CDP Browser Stream Paused</span>
                </div>
                <p className="text-[12px] font-medium font-(family-name:--app-font) text-white/50 max-w-xs mb-3 leading-relaxed">
                  Headless Chromium screencast halted to save 0% CPU across inactive cards.
                </p>
                <div className="px-3.5 py-1.5 rounded-[8px] bg-white/[0.08] hover:bg-white/15 border border-white/15 text-white text-[12px] font-medium flex items-center gap-1.5 transition-all shadow-md group-hover:border-white/30">
                  <span>Click to Resume Preview</span>
                  <MaterialIcon name="play_arrow" className="text-[14px]"/>
                </div>
              </div>
            )}

            <CdpBrowserSurface
              threadId={threadId}
              initialUrl={currentUrl}
              paused={shouldPause}
              isFocused={isFocused && !isAnimating}
              actionRef={surfaceActionRef}
              onStateChange={(state) => {
                setCurrentUrl(state.url);
                if (!isInputEditingRef.current) {
                  setInputUrl(state.url === 'about:blank' ? '' : state.url);
                }
                setIsLoading(state.loading);
                setCanGoBack(state.canGoBack);
                setCanGoForward(state.canGoForward);
              }}
              onError={(err) => setBrowserError(err)}
              className="w-full h-full"
            />

            {isLoading && (
              <div className="absolute top-0 inset-x-0 h-[2.5px] bg-black/10 overflow-hidden z-10">
                <div className="h-full w-1/3 bg-[#007AFF] animate-[browser-progress_1.1s_ease-in-out_infinite]" />
              </div>
            )}

            {browserError && (
              <div className="absolute inset-0 bg-[#16181d] flex flex-col items-center justify-center text-center p-6 z-20">
                <div className="w-10 h-10 rounded-xl bg-amber-500/10 border border-amber-500/20 flex items-center justify-center mb-3">
                  <MaterialIcon name="public_off" className="text-amber-400 text-xl"/>
                </div>
                <h4 className="text-[13.5px] font-semibold font-(family-name:--app-font) text-white/90 mb-1">
                  Chromium Browser Unavailable
                </h4>
                <p className="text-[12px] text-white/50 max-w-sm mb-4 leading-relaxed font-(family-name:--app-font)">
                  {browserError}
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

            {visibleBookmarks.length > 0 && (
              <div className="flex flex-wrap items-center justify-center gap-2 max-w-lg mt-2 mb-4">
                {visibleBookmarks.slice(0, 8).map((bm) => (
                  <button
                    key={bm.id}
                    type="button"
                    onClick={() => navigateTo(bm.url)}
                    className="flex items-center gap-2 px-3 py-1.5 rounded-[12px] bg-white/[0.05] hover:bg-white/[0.1] border border-white/10 hover:border-white/20 text-white/90 text-[12px] font-medium transition-all active:scale-95 cursor-pointer shadow-sm"
                  >
                    <img
                      src={getFaviconUrl(bm.url)}
                      alt=""
                      className="w-4 h-4 object-contain rounded-xs shrink-0"
                      onError={(e) => {
                        (e.currentTarget as HTMLElement).style.display = 'none';
                      }}
                    />
                    <span className="truncate max-w-[140px]">{bm.title}</span>
                  </button>
                ))}
              </div>
            )}

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

      <BookmarkModal
        isOpen={isBookmarkModalOpen}
        onClose={() => setIsBookmarkModalOpen(false)}
        onSave={handleSaveBookmark}
        onDelete={editingBookmark?.id ? handleDeleteBookmark : undefined}
        initialData={editingBookmark || undefined}
        currentProjectName={projectName}
        availableProjects={availableProjects}
        isEditing={Boolean(editingBookmark?.id)}
      />
    </div>
  );
};

export default AgentBrowserView;
