import React, { useCallback, useEffect, useRef, useState } from 'react';
import { BrowserClose, BrowserEndpoint, BrowserOpen } from '../../../wailsjs/go/main/App';
import { browser } from '../../../wailsjs/go/models';

export interface CdpBrowserSurfaceProps {
  threadId?: string;
  initialUrl: string;
  paused?: boolean;
  isFocused?: boolean;
  externalNav?: { url: string; nonce: number };
  actionRef?: React.MutableRefObject<{
    back(): void;
    forward(): void;
    reload(): void;
    navigate(url: string): void;
  } | null>;
  onStateChange?: (state: {
    url: string;
    title: string;
    loading: boolean;
    canGoBack: boolean;
    canGoForward: boolean;
  }) => void;
  onError?: (message: string) => void;
  className?: string;
}

export const CdpBrowserSurface: React.FC<CdpBrowserSurfaceProps> = ({
  threadId = '',
  initialUrl,
  paused = false,
  isFocused = true,
  externalNav,
  actionRef,
  onStateChange,
  onError,
  className = '',
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const sessionRef = useRef<browser.SessionInfo | null>(null);
  const latestFrameSeq = useRef<number>(0);
  const tabStateRef = useRef<{
    width: number;
    height: number;
    url: string;
    title: string;
    loading: boolean;
    canGoBack: boolean;
    canGoForward: boolean;
  }>({
    width: 1280,
    height: 800,
    url: initialUrl,
    title: '',
    loading: false,
    canGoBack: false,
    canGoForward: false,
  });

  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [isConnected, setIsConnected] = useState<boolean>(false);

  const send = useCallback((msg: any) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(msg));
    }
  }, []);

  // Expose action controls to parent component
  useEffect(() => {
    if (actionRef) {
      actionRef.current = {
        back: () => send({ type: 'back' }),
        forward: () => send({ type: 'forward' }),
        reload: () => send({ type: 'reload' }),
        navigate: (url: string) => send({ type: 'navigate', url }),
      };
    }
    return () => {
      if (actionRef) {
        actionRef.current = null;
      }
    };
  }, [actionRef, send]);

  // Sync external navigation requests
  const lastNavNonce = useRef<number | undefined>(externalNav?.nonce);
  useEffect(() => {
    if (externalNav && externalNav.nonce !== lastNavNonce.current) {
      lastNavNonce.current = externalNav.nonce;
      send({ type: 'navigate', url: externalNav.url });
    }
  }, [externalNav, send]);

  // Sync paused state
  useEffect(() => {
    send({ type: 'pause', paused });
  }, [paused, send]);

  // Setup CDP Browser session and WebSocket connection
  useEffect(() => {
    let unmounted = false;
    let createdSessionId: string | null = null;
    let ws: WebSocket | null = null;

    async function init() {
      try {
        const rect = containerRef.current?.getBoundingClientRect();
        const w = Math.max(200, Math.round(rect?.width || 1280));
        const h = Math.max(160, Math.round(rect?.height || 800));

        const [endpoint, session] = await Promise.all([
          BrowserEndpoint(),
          BrowserOpen(threadId, initialUrl, w, h),
        ]);

        if (unmounted) {
          if (session?.id) void BrowserClose(session.id);
          return;
        }

        createdSessionId = session.id;
        sessionRef.current = session;
        tabStateRef.current = {
          width: session.width || w,
          height: session.height || h,
          url: session.url || initialUrl,
          title: session.title || '',
          loading: session.loading || false,
          canGoBack: session.canGoBack || false,
          canGoForward: session.canGoForward || false,
        };

        if (onStateChange) {
          onStateChange({
            url: session.url || initialUrl,
            title: session.title || '',
            loading: session.loading || false,
            canGoBack: session.canGoBack || false,
            canGoForward: session.canGoForward || false,
          });
        }

        const wsUrl = `${endpoint.wsUrl}?id=${encodeURIComponent(session.id)}&token=${encodeURIComponent(endpoint.token)}`;
        ws = new WebSocket(wsUrl);
        ws.binaryType = 'arraybuffer';
        wsRef.current = ws;

        ws.onopen = () => {
          if (unmounted) return;
          setIsConnected(true);
          setErrorMessage(null);
          if (paused) {
            ws?.send(JSON.stringify({ type: 'pause', paused: true }));
          }
        };

        ws.onmessage = async (event: MessageEvent) => {
          if (unmounted) return;

          // Binary screencast JPEG frame
          if (event.data instanceof ArrayBuffer) {
            const seq = ++latestFrameSeq.current;
            try {
              const blob = new Blob([event.data], { type: 'image/jpeg' });
              const bitmap = await createImageBitmap(blob);
              if (seq < latestFrameSeq.current || unmounted) {
                bitmap.close();
                return;
              }
              const canvas = canvasRef.current;
              if (canvas) {
                if (canvas.width !== bitmap.width || canvas.height !== bitmap.height) {
                  canvas.width = bitmap.width;
                  canvas.height = bitmap.height;
                }
                const ctx = canvas.getContext('2d');
                if (ctx) {
                  ctx.drawImage(bitmap, 0, 0);
                }
              }
              bitmap.close();
            } catch {
              // Frame decode error; drop frame
            }
            return;
          }

          // JSON state or error frame
          if (typeof event.data === 'string') {
            try {
              const payload = JSON.parse(event.data);
              if (payload.type === 'state' && payload.state) {
                const s = payload.state;
                tabStateRef.current = {
                  width: s.width || tabStateRef.current.width,
                  height: s.height || tabStateRef.current.height,
                  url: s.url || '',
                  title: s.title || '',
                  loading: s.loading ?? false,
                  canGoBack: s.canGoBack ?? false,
                  canGoForward: s.canGoForward ?? false,
                };
                onStateChange?.({
                  url: s.url || '',
                  title: s.title || '',
                  loading: s.loading ?? false,
                  canGoBack: s.canGoBack ?? false,
                  canGoForward: s.canGoForward ?? false,
                });
              } else if (payload.type === 'error') {
                const msg = payload.message || 'Browser session error';
                setErrorMessage(msg);
                onError?.(msg);
              }
            } catch {
              // JSON parse error
            }
          }
        };

        ws.onerror = () => {
          if (unmounted) return;
          const msg = 'WebSocket connection to browser stream failed';
          setErrorMessage(msg);
          onError?.(msg);
        };

        ws.onclose = () => {
          if (unmounted) return;
          setIsConnected(false);
        };
      } catch (err: any) {
        if (unmounted) return;
        const msg = err?.message || String(err) || 'Failed to start headless Chromium';
        setErrorMessage(msg);
        onError?.(msg);
      }
    }

    void init();

    return () => {
      unmounted = true;
      if (ws) {
        ws.close();
        wsRef.current = null;
      }
      if (createdSessionId) {
        void BrowserClose(createdSessionId);
      }
    };
  }, [threadId, initialUrl, onStateChange, onError]);

  // Debounced ResizeObserver to track container dimension changes
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let resizeTimer: number | null = null;
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const { width, height } = entry.contentRect;
        if (width < 50 || height < 50) return;

        if (resizeTimer !== null) {
          window.clearTimeout(resizeTimer);
        }
        resizeTimer = window.setTimeout(() => {
          const dpr = Math.min(window.devicePixelRatio || 1, 2);
          const w = Math.round(width);
          const h = Math.round(height);
          tabStateRef.current.width = w;
          tabStateRef.current.height = h;
          send({
            type: 'resize',
            width: w,
            height: h,
            dpr,
          });
        }, 80);
      }
    });

    observer.observe(container);
    return () => {
      if (resizeTimer !== null) window.clearTimeout(resizeTimer);
      observer.disconnect();
    };
  }, [send]);

  // Pointer event helpers
  const getCoordinates = (e: React.PointerEvent | React.WheelEvent) => {
    const canvas = canvasRef.current;
    if (!canvas) return { x: 0, y: 0 };
    const rect = canvas.getBoundingClientRect();
    const emulatedWidth = tabStateRef.current.width || canvas.width || 1280;
    const emulatedHeight = tabStateRef.current.height || canvas.height || 800;
    const scaleX = emulatedWidth / (rect.width || 1);
    const scaleY = emulatedHeight / (rect.height || 1);
    return {
      x: Math.max(0, Math.min(emulatedWidth, (e.clientX - rect.left) * scaleX)),
      y: Math.max(0, Math.min(emulatedHeight, (e.clientY - rect.top) * scaleY)),
    };
  };

  const getModifiers = (e: React.MouseEvent | React.KeyboardEvent) => {
    let modifiers = 0;
    if (e.altKey) modifiers |= 1;
    if (e.ctrlKey) modifiers |= 2;
    if (e.metaKey) modifiers |= 4;
    if (e.shiftKey) modifiers |= 8;
    return modifiers;
  };

  const mapButton = (b: number): string => {
    if (b === 0) return 'left';
    if (b === 1) return 'middle';
    if (b === 2) return 'right';
    return 'none';
  };

  const handlePointerDown = (e: React.PointerEvent) => {
    if (!isFocused) return;
    const { x, y } = getCoordinates(e);
    canvasRef.current?.focus();
    send({
      type: 'mouse',
      mouseType: 'mousePressed',
      x,
      y,
      button: mapButton(e.button),
      buttons: e.buttons,
      clickCount: e.detail || 1,
      modifiers: getModifiers(e),
    });
  };

  const handlePointerUp = (e: React.PointerEvent) => {
    if (!isFocused) return;
    const { x, y } = getCoordinates(e);
    send({
      type: 'mouse',
      mouseType: 'mouseReleased',
      x,
      y,
      button: mapButton(e.button),
      buttons: e.buttons,
      clickCount: e.detail || 1,
      modifiers: getModifiers(e),
    });
  };

  const handlePointerMove = (e: React.PointerEvent) => {
    if (!isFocused) return;
    const { x, y } = getCoordinates(e);
    send({
      type: 'mouse',
      mouseType: 'mouseMoved',
      x,
      y,
      button: 'none',
      buttons: e.buttons,
      modifiers: getModifiers(e),
    });
  };

  const handleWheel = (e: React.WheelEvent) => {
    if (!isFocused) return;
    const { x, y } = getCoordinates(e);
    send({
      type: 'mouse',
      mouseType: 'mouseWheel',
      x,
      y,
      deltaX: e.deltaX,
      deltaY: e.deltaY,
      modifiers: getModifiers(e),
    });
  };

  // Keyboard handlers
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (!isFocused) return;

    // Handle paste shortcut (Ctrl+V / Cmd+V)
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'v') {
      return; // Handled by handlePaste
    }

    send({
      type: 'key',
      keyType: 'keyDown',
      key: e.key,
      code: e.code,
      text: e.key.length === 1 ? e.key : '',
      unmodifiedText: e.key.length === 1 ? e.key : '',
      windowsVirtualKeyCode: e.keyCode,
      nativeVirtualKeyCode: e.keyCode,
      modifiers: getModifiers(e),
    });
  };

  const handleKeyUp = (e: React.KeyboardEvent) => {
    if (!isFocused) return;
    send({
      type: 'key',
      keyType: 'keyUp',
      key: e.key,
      code: e.code,
      text: '',
      unmodifiedText: '',
      windowsVirtualKeyCode: e.keyCode,
      nativeVirtualKeyCode: e.keyCode,
      modifiers: getModifiers(e),
    });
  };

  const handlePaste = (e: React.ClipboardEvent) => {
    if (!isFocused) return;
    const text = e.clipboardData.getData('text');
    if (text) {
      e.preventDefault();
      send({ type: 'paste', text });
    }
  };

  return (
    <div
      ref={containerRef}
      className={`relative w-full h-full min-h-0 overflow-hidden bg-black flex items-center justify-center ${className}`}
    >
      <canvas
        ref={canvasRef}
        tabIndex={0}
        onPointerDown={handlePointerDown}
        onPointerUp={handlePointerUp}
        onPointerMove={handlePointerMove}
        onWheel={handleWheel}
        onKeyDown={handleKeyDown}
        onKeyUp={handleKeyUp}
        onPaste={handlePaste}
        onContextMenu={(e) => {
          if (isFocused) e.preventDefault();
        }}
        className="w-full h-full block object-contain focus:outline-none cursor-default"
        style={{
          touchAction: 'none',
          imageRendering: 'auto',
        }}
      />

      {errorMessage && (
        <div className="absolute inset-0 z-30 bg-[#16181d] flex flex-col items-center justify-center text-center p-6">
          <div className="w-10 h-10 rounded-xl bg-red-500/10 border border-red-500/20 flex items-center justify-center mb-3 text-red-400">
            <span className="material-symbols-outlined text-[20px]">error_outline</span>
          </div>
          <h4 className="text-[13.5px] font-semibold text-white/90 mb-1">
            Browser Unavailable
          </h4>
          <p className="text-[12px] text-white/50 max-w-sm mb-4 leading-relaxed font-mono">
            {errorMessage}
          </p>
        </div>
      )}
    </div>
  );
};

export default CdpBrowserSurface;
