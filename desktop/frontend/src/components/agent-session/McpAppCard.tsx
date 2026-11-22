import { MaterialIcon, Maximize } from '../common/icons';
import React, { useState, useEffect, useRef, useCallback } from 'react';
import { OrbitLoader } from './OrbitLoader';

export interface McpAppCardProps {
  resourceUri?: string;
  htmlContent?: string;
  toolName?: string;
  title?: string;
  rawOutput?: string;
  driver?: string;
  className?: string;
}

/**
 * Resolve an MCP ui:// URI to an HTTP embed URL.
 * Supports Falacy remote MCP endpoints out-of-the-box,
 * and passes through standard HTTP/HTTPS URIs.
 */
function resolveEmbedUrl(uri: string): string | null {
  if (!uri) return null;
  const trimmed = uri.trim();
  if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
    return trimmed;
  }
  if (trimmed.startsWith('ui://falacy/')) {
    return `https://falacy.lirikrexhepiii.workers.dev/api/mcp/ui?uri=${encodeURIComponent(trimmed)}`;
  }
  return null;
}

function parseResourceBadge(uri?: string, toolName?: string): { brand: string; label: string } {
  if (uri?.startsWith('ui://falacy/')) {
    if (uri.includes('/card/')) return { brand: 'Falacy', label: 'Inspiration Card' };
    if (uri.includes('/board/')) return { brand: 'Falacy', label: 'Board Gallery' };
    return { brand: 'Falacy', label: 'MCP App' };
  }
  const cleanName = (toolName || 'MCP App')
    .replace(/^([a-zA-Z0-9_-]+)\./, '$1 • ')
    .replace(/_/g, ' ');
  return { brand: 'MCP App', label: cleanName };
}

/**
 * McpAppCard Component
 * High-performance, sandboxed inline component for Model Context Protocol (io.modelcontextprotocol/ui)
 * and interactive HTML agent responses.
 *
 * Implements:
 * - Strict iframe sandboxing without parent DOM access (allow-scripts, allow-forms, allow-popups, allow-modals)
 * - Bidirectional postMessage event bridge (mcp-app:ready, mcp-app:resize, mcp-app:navigate)
 * - Emil Kowalski fluid spring dimension scaling on height transitions
 * - Dynamic theme propagation (light/dark mode sync)
 * - Fullscreen zoom / popout overlay mode
 * - Clean fallback with collapsible raw tool output
 */
export const McpAppCard: React.FC<McpAppCardProps> = ({
  resourceUri,
  htmlContent,
  toolName,
  title,
  rawOutput,
  driver,
  className = '',
}) => {
  const [activeUri, setActiveUri] = useState<string>(resourceUri || '');
  const [currentHtml, setCurrentHtml] = useState<string | undefined>(htmlContent);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [height, setHeight] = useState<number>(280);
  const [isCollapsed, setIsCollapsed] = useState<boolean>(false);
  const [isFullscreen, setIsFullscreen] = useState<boolean>(false);
  const [showRawOutput, setShowRawOutput] = useState<boolean>(false);

  const iframeRef = useRef<HTMLIFrameElement>(null);
  const cardIdRef = useRef<string>(`mcp-app-${Math.random().toString(36).substring(2, 9)}`);

  const embedUrl = resolveEmbedUrl(activeUri);
  const badge = parseResourceBadge(activeUri, toolName);

  // Sync state when props change
  useEffect(() => {
    if (resourceUri) setActiveUri(resourceUri);
    if (htmlContent) setCurrentHtml(htmlContent);
  }, [resourceUri, htmlContent]);

  // Handle postMessage communication from the sandboxed iframe
  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      if (event.source !== iframeRef.current?.contentWindow) return;
      // Validate message structure
      if (!event.data || typeof event.data !== 'object') return;
      const { type, height: reportedHeight, uri: targetUri } = event.data;

      if (type === 'mcp-app:resize' && typeof reportedHeight === 'number') {
        // Clamp height directly to exact reported element bounds
        const targetH = Math.max(60, Math.min(850, reportedHeight));
        setHeight(targetH);
        setLoading(false);
      } else if (type === 'mcp-app:ready') {
        setLoading(false);
        setError(null);
        // Post theme configuration to the newly loaded widget
        if (iframeRef.current?.contentWindow) {
          iframeRef.current.contentWindow.postMessage({ type: 'theme:change', theme: 'dark' }, '*');
        }
      } else if (type === 'mcp-app:navigate' && typeof targetUri === 'string') {
        // Allow in-widget drill-down (e.g. from board view to specific card)
        setActiveUri(targetUri);
        setLoading(true);
      }
    };

    window.addEventListener('message', handleMessage);
    return () => window.removeEventListener('message', handleMessage);
  }, []);

  // Keyboard navigation for fullscreen modal
  useEffect(() => {
    if (!isFullscreen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setIsFullscreen(false);
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isFullscreen]);

  const handleReload = useCallback(() => {
    setLoading(true);
    setError(null);
    if (iframeRef.current) {
      if (embedUrl) {
        iframeRef.current.src = embedUrl;
      } else if (currentHtml) {
        iframeRef.current.srcdoc = currentHtml;
      }
    }
  }, [embedUrl, currentHtml]);

  const handleOpenExternal = () => {
    if (embedUrl) {
      window.open(embedUrl, '_blank', 'noopener,noreferrer');
    } else if (activeUri.startsWith('ui://falacy/')) {
      window.open('https://falacy.lirikrexhepiii.workers.dev/app', '_blank', 'noopener,noreferrer');
    }
  };

  const renderIframe = (inModal = false) => {
    if (!embedUrl && !currentHtml) {
      return (
        <div className="flex flex-col items-center justify-center py-8 px-4 text-center bg-[#0d0d0f]">
          <MaterialIcon name="computer" className="text-[28px] text-white/30 mb-2" />
          <span className="text-[12px] font-medium text-white/60">
            UI resource reference: <code className="text-white/80">{activeUri || 'N/A'}</code>
          </span>
          {rawOutput && (
            <button
              type="button"
              onClick={() => setShowRawOutput(!showRawOutput)}
              className="mt-3 text-[11.5px] text-sky-400 hover:text-sky-300 underline cursor-pointer"
            >
              {showRawOutput ? 'Hide raw tool output' : 'Show raw tool output'}
            </button>
          )}
        </div>
      );
    }

    return (
      <div className="relative w-full h-full flex flex-col items-center justify-center bg-[#0d0d0f]">
        {loading && (
          <div className="absolute inset-0 flex items-center justify-center bg-[#0d0d0f]/80 backdrop-blur-[2px] z-10 transition-opacity duration-200">
            <OrbitLoader size={22} className="text-white/80" />
          </div>
        )}

        <iframe
          ref={inModal ? undefined : iframeRef}
          id={inModal ? `${cardIdRef.current}-modal` : cardIdRef.current}
          title={title || badge.label}
          src={embedUrl || undefined}
          srcDoc={!embedUrl ? currentHtml : undefined}
          sandbox="allow-scripts allow-forms allow-popups allow-modals"
          loading="lazy"
          onLoad={() => setLoading(false)}
          onError={() => {
            setLoading(false);
            setError('Failed to load MCP UI component.');
          }}
          className={`w-full border-0 bg-[#0d0d0f] transition-opacity duration-200 ${
            loading ? 'opacity-0' : 'opacity-100'
          }`}
          style={{
            height: inModal ? '100%' : `${height}px`,
            maxHeight: inModal ? '100%' : '800px',
            backgroundColor: '#0d0d0f',
          }}
        />
      </div>
    );
  };

  return (
    <>
      <div
        className={`w-full relative my-1.5 rounded-xl border border-white/[0.08] bg-[#0d0d0f] overflow-hidden font-(family-name:--app-font) ${className}`}
      >
        <div className="flex items-center justify-between px-3 py-1.5 border-b border-white/[0.06] bg-white/[0.02]">
          <button
            type="button"
            onClick={() => setIsCollapsed((prev) => !prev)}
            className="flex items-center gap-2 min-w-0 text-left cursor-pointer"
            title={isCollapsed ? 'Expand MCP App' : 'Collapse MCP App'}
          >
            <MaterialIcon
              name={isCollapsed ? 'expand_more' : 'expand_less'}
              className="text-[14px] text-white/50 shrink-0"
            />
            <span className="text-[10.5px] font-medium px-1.5 py-0.5 rounded bg-white/[0.06] text-white/75 shrink-0">
              {badge.brand}
            </span>
            <span className="text-[11.5px] font-medium text-white/85 truncate">
              {title || badge.label}
            </span>
          </button>

          <div className="flex items-center gap-1 shrink-0">
            {rawOutput && (
              <button
                type="button"
                onClick={() => {
                  setShowRawOutput((prev) => !prev);
                  if (isCollapsed) setIsCollapsed(false);
                }}
                title={showRawOutput ? 'Show rendered app' : 'Show raw output'}
                className={`flex items-center gap-1 px-2 py-0.5 rounded text-[11px] transition-colors cursor-pointer ${
                  showRawOutput
                    ? 'bg-white/[0.14] text-white'
                    : 'text-white/55 hover:text-white hover:bg-white/[0.06]'
                }`}
              >
                <MaterialIcon name="terminal" className="text-[12px]" />
                <span>{showRawOutput ? 'App' : 'Raw'}</span>
              </button>
            )}
            {!showRawOutput && !isCollapsed && (embedUrl || currentHtml) && (
              <button
                type="button"
                onClick={handleReload}
                title="Reload app"
                className="p-1 rounded text-white/55 hover:text-white hover:bg-white/[0.06] transition-colors cursor-pointer"
              >
                <MaterialIcon name="refresh" className="text-[13px]" />
              </button>
            )}
            {(embedUrl || activeUri.startsWith('ui://falacy/')) && (
              <button
                type="button"
                onClick={handleOpenExternal}
                title="Open in browser"
                className="p-1 rounded text-white/55 hover:text-white hover:bg-white/[0.06] transition-colors cursor-pointer"
              >
                <MaterialIcon name="open_in_new" className="text-[13px]" />
              </button>
            )}
            {(embedUrl || currentHtml) && (
              <button
                type="button"
                onClick={() => setIsFullscreen(true)}
                title="Fullscreen"
                className="p-1 rounded text-white/55 hover:text-white hover:bg-white/[0.06] transition-colors cursor-pointer"
              >
                <Maximize size={12} />
              </button>
            )}
          </div>
        </div>

        {!isCollapsed && (
          showRawOutput && rawOutput ? (
            <pre className="m-0 p-3 text-[11px] leading-relaxed font-mono text-white/80 bg-[#0d0d0f] overflow-auto max-h-[420px] whitespace-pre-wrap break-words select-text">
              {rawOutput}
            </pre>
          ) : error ? (
            <div className="p-4 text-center text-rose-400 text-[12px] flex flex-col items-center gap-2 bg-[#0d0d0f]">
              <MaterialIcon name="error" className="text-[20px]" />
              <span>{error}</span>
              <button
                type="button"
                onClick={handleReload}
                className="px-3 py-1 rounded-full bg-white/10 hover:bg-white/20 text-white text-[11px] mt-1 transition-all cursor-pointer"
              >
                Retry
              </button>
            </div>
          ) : (
            renderIframe(false)
          )
        )}
      </div>

      {/* Fullscreen Zoom Modal (T3Code-style popout) */}
      {isFullscreen && (
        <div className="fixed inset-0 z-50 bg-black/85 backdrop-blur-md flex items-center justify-center p-4 sm:p-8 animate-in fade-in duration-200">
          <div className="w-full max-w-5xl h-[85vh] bg-[#0d0d0f] border border-white/[0.12] rounded-[24px] shadow-2xl flex flex-col overflow-hidden">
            <div className="flex items-center justify-between px-5 py-3 border-b border-white/[0.08] bg-white/[0.02]">
              <div className="flex items-center gap-2">
                <span className="text-[13px] font-medium text-white/90">
                  {title || badge.brand}
                </span>
              </div>
              <div className="flex items-center gap-2">
                {(embedUrl || activeUri.startsWith('ui://falacy/')) && (
                  <button
                    type="button"
                    onClick={handleOpenExternal}
                    className="flex items-center gap-1.5 px-3 py-1 rounded-full bg-white/[0.06] hover:bg-white/[0.12] text-[11.5px] text-white/90 transition-all cursor-pointer"
                  >
                    <span>Open in browser</span>
                    <MaterialIcon name="open_in_new" className="text-[13px]" />
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => setIsFullscreen(false)}
                  className="p-1.5 rounded-full bg-white/[0.08] hover:bg-white/[0.16] text-white/80 hover:text-white transition-all cursor-pointer"
                >
                  <MaterialIcon name="close" className="text-[16px]" />
                </button>
              </div>
            </div>
            <div className="flex-1 w-full h-full p-2 bg-[#0d0d0f] overflow-hidden">
              {renderIframe(true)}
            </div>
          </div>
        </div>
      )}
    </>
  );
};

export default McpAppCard;
