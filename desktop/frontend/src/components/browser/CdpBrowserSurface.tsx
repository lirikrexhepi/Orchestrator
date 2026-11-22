import React, { useCallback, useEffect, useRef, useState } from 'react';
import { BrowserClose, BrowserEndpoint, BrowserOpen } from '../../../wailsjs/go/main/App';
import { browser } from '../../../wailsjs/go/models';
import { CircleAlert, RefreshCw } from '../common/icons';

export interface CdpBrowserAction {
  back(): void;
  forward(): void;
  reload(): void;
  navigate(url: string): void;
  retry(): void;
  captureScreenshot(): Promise<string | null>;
}

export interface CdpBrowserSurfaceProps {
  threadId?: string;
  tabId?: string;
  initialUrl: string;
  paused?: boolean;
  isFocused?: boolean;
  hideErrorOverlay?: boolean;
  externalNav?: { url: string; nonce: number };
  actionRef?: React.MutableRefObject<CdpBrowserAction | null>;
  onStateChange?: (state: {
    url: string;
    title: string;
    favicon?: string;
    loading: boolean;
    canGoBack: boolean;
    canGoForward: boolean;
  }) => void;
  onError?: (message: string) => void;
  className?: string;
}

const PREVENT_KEYS = new Set([
  ' ',
  'Spacebar',
  'ArrowUp',
  'ArrowDown',
  'ArrowLeft',
  'ArrowRight',
  'Tab',
  'Backspace',
  'Escape',
  'Enter',
]);

export const CdpBrowserSurface: React.FC<CdpBrowserSurfaceProps> = ({
  threadId = '',
  tabId = 'default',
  initialUrl,
  paused = false,
  isFocused = true,
  hideErrorOverlay = false,
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
  const pendingFrameRef = useRef<ArrayBuffer | null>(null);
  const decodingFrameRef = useRef<boolean>(false);
  const lastClickRef = useRef<{ time: number; x: number; y: number; button: number; count: number }>({
    time: 0,
    x: 0,
    y: 0,
    button: -1,
    count: 0,
  });

  const pendingResizeRef = useRef<{ width: number; height: number; dpr: number } | null>(null);
  const pendingNavRef = useRef<string | null>(null);
  const pendingCapturesRef = useRef<Map<string, (data: string | null) => void>>(new Map());
  const captureSeqRef = useRef<number>(0);

  const moveRafRef = useRef<number | null>(null);
  const pendingMoveRef = useRef<{
    x: number;
    y: number;
    buttons: number;
    modifiers: number;
  } | null>(null);

  const wheelRafRef = useRef<number | null>(null);
  const pendingWheelRef = useRef<{
    x: number;
    y: number;
    deltaX: number;
    deltaY: number;
    modifiers: number;
  } | null>(null);

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

  const initialUrlRef = useRef(initialUrl);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  const isFocusedRef = useRef(isFocused);
  isFocusedRef.current = isFocused;

  const onStateChangeRef = useRef(onStateChange);
  onStateChangeRef.current = onStateChange;
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;

  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [cursorStyle, setCursorStyle] = useState<string>('default');
  const [reconnectNonce, setReconnectNonce] = useState<number>(0);

  const sessionThreadKey =
    tabId && tabId !== 'default'
      ? threadId
        ? `${threadId}:${tabId}`
        : `browser:${tabId}`
      : threadId;

  const send = useCallback((msg: Record<string, unknown>) => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
      return;
    }
    if (msg.type === 'resize') {
      pendingResizeRef.current = {
        width: Number(msg.width) || 1280,
        height: Number(msg.height) || 800,
        dpr: Number(msg.dpr) || 1,
      };
    } else if (msg.type === 'navigate' && typeof msg.url === 'string') {
      pendingNavRef.current = msg.url;
    }
  }, []);

  const captureFromCanvas = useCallback((): string | null => {
    const canvas = canvasRef.current;
    if (!canvas || canvas.width === 0 || canvas.height === 0) return null;
    try {
      return canvas.toDataURL('image/png');
    } catch {
      return null;
    }
  }, []);

  const captureScreenshot = useCallback((): Promise<string | null> => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return Promise.resolve(captureFromCanvas());
    }
    const requestId = `cap-${Date.now()}-${++captureSeqRef.current}`;
    return new Promise<string | null>((resolve) => {
      const timer = window.setTimeout(() => {
        if (pendingCapturesRef.current.has(requestId)) {
          pendingCapturesRef.current.delete(requestId);
          resolve(captureFromCanvas());
        }
      }, 4500);

      pendingCapturesRef.current.set(requestId, (data) => {
        window.clearTimeout(timer);
        if (data && data.length > 0) {
          resolve(data.startsWith('data:') ? data : `data:image/png;base64,${data}`);
        } else {
          resolve(captureFromCanvas());
        }
      });

      try {
        ws.send(JSON.stringify({ type: 'capture', requestId }));
      } catch {
        pendingCapturesRef.current.delete(requestId);
        window.clearTimeout(timer);
        resolve(captureFromCanvas());
      }
    });
  }, [captureFromCanvas]);

  const retry = useCallback(() => {
    setErrorMessage(null);
    const activeUrl = tabStateRef.current.url;
    if (activeUrl && activeUrl !== 'about:blank') {
      initialUrlRef.current = activeUrl;
    }
    if (wsRef.current) {
      try {
        wsRef.current.close();
      } catch {
        // Ignore close errors
      }
      wsRef.current = null;
    }
    if (sessionRef.current?.id) {
      const oldId = sessionRef.current.id;
      sessionRef.current = null;
      void BrowserClose(oldId);
    }
    setReconnectNonce((n) => n + 1);
  }, []);

  // Expose action controls to parent component
  useEffect(() => {
    if (actionRef) {
      actionRef.current = {
        back: () => send({ type: 'back' }),
        forward: () => send({ type: 'forward' }),
        reload: () => send({ type: 'reload' }),
        navigate: (url: string) => {
          if (url && url !== 'about:blank') {
            initialUrlRef.current = url;
          }
          send({ type: 'navigate', url });
        },
        retry,
        captureScreenshot,
      };
    }
    return () => {
      if (actionRef) {
        actionRef.current = null;
      }
    };
  }, [actionRef, send, retry, captureScreenshot]);

  // Sync external navigation requests
  const lastNavNonce = useRef<number | undefined>(externalNav?.nonce);
  useEffect(() => {
    if (externalNav && externalNav.nonce !== lastNavNonce.current) {
      lastNavNonce.current = externalNav.nonce;
      if (externalNav.url && externalNav.url !== 'about:blank') {
        initialUrlRef.current = externalNav.url;
      }
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
        const dpr = Math.max(1, Math.min(window.devicePixelRatio || 1, 2.5));
        const startUrl = initialUrlRef.current || 'about:blank';

        const [endpoint, session] = await Promise.all([
          BrowserEndpoint(),
          BrowserOpen(sessionThreadKey, startUrl, w, h),
        ]);

        if (unmounted) {
          if (session?.id) void BrowserClose(session.id);
          return;
        }

        const activeUrl = session.url && session.url !== 'about:blank' ? session.url : startUrl;
        createdSessionId = session.id;
        sessionRef.current = session;
        tabStateRef.current = {
          width: session.width || w,
          height: session.height || h,
          url: activeUrl,
          title: session.title || '',
          loading: session.loading || false,
          canGoBack: session.canGoBack || false,
          canGoForward: session.canGoForward || false,
        };

        onStateChangeRef.current?.({
          url: activeUrl,
          title: session.title || '',
          loading: session.loading || false,
          canGoBack: session.canGoBack || false,
          canGoForward: session.canGoForward || false,
        });

        const wsUrl = `${endpoint.wsUrl}?id=${encodeURIComponent(session.id)}&token=${encodeURIComponent(endpoint.token)}`;
        ws = new WebSocket(wsUrl);
        ws.binaryType = 'arraybuffer';
        wsRef.current = ws;

        ws.onopen = () => {
          if (unmounted || !ws) return;
          setErrorMessage(null);

          // Flush queued resize (or apply initial HiDPI DPR sizing)
          const pendingResize = pendingResizeRef.current;
          pendingResizeRef.current = null;
          if (pendingResize) {
            tabStateRef.current.width = pendingResize.width;
            tabStateRef.current.height = pendingResize.height;
            ws.send(JSON.stringify({ type: 'resize', ...pendingResize }));
          } else {
            const curRect = containerRef.current?.getBoundingClientRect();
            const curW = Math.max(200, Math.round(curRect?.width || w));
            const curH = Math.max(160, Math.round(curRect?.height || h));
            tabStateRef.current.width = curW;
            tabStateRef.current.height = curH;
            ws.send(JSON.stringify({ type: 'resize', width: curW, height: curH, dpr }));
          }

          // Flush queued navigation
          const pendingNav = pendingNavRef.current;
          pendingNavRef.current = null;
          if (pendingNav) {
            ws.send(JSON.stringify({ type: 'navigate', url: pendingNav }));
          }

          ws.send(JSON.stringify({ type: 'pause', paused: pausedRef.current }));
        };

        const pumpFrameDecode = async () => {
          if (decodingFrameRef.current) return;
          decodingFrameRef.current = true;
          try {
            while (!unmounted && pendingFrameRef.current) {
              const frameData = pendingFrameRef.current;
              pendingFrameRef.current = null;
              try {
                const blob = new Blob([frameData], { type: 'image/jpeg' });
                const bitmap = await createImageBitmap(blob);
                if (unmounted) {
                  bitmap.close();
                  break;
                }
                // If an even newer frame arrived while decoding, skip painting stale bitmap
                if (!pendingFrameRef.current) {
                  const canvas = canvasRef.current;
                  if (canvas) {
                    if (canvas.width !== bitmap.width || canvas.height !== bitmap.height) {
                      canvas.width = bitmap.width;
                      canvas.height = bitmap.height;
                    }
                    const ctx = canvas.getContext('2d', { alpha: false });
                    if (ctx) {
                      ctx.imageSmoothingEnabled = true;
                      ctx.imageSmoothingQuality = 'high';
                      ctx.drawImage(bitmap, 0, 0);
                    }
                  }
                }
                bitmap.close();
              } catch {
                // Frame decode error; drop frame
              }
            }
          } finally {
            decodingFrameRef.current = false;
          }
        };

        ws.onmessage = (event: MessageEvent) => {
          if (unmounted) return;

          // Binary screencast JPEG frame — keep only the newest undecoded frame
          if (event.data instanceof ArrayBuffer) {
            pendingFrameRef.current = event.data;
            void pumpFrameDecode();
            return;
          }

          // JSON state, screenshot, or error frame
          if (typeof event.data === 'string') {
            try {
              const payload = JSON.parse(event.data);
              if (payload.type === 'state' && payload.state) {
                const s = payload.state;
                const stateUrl =
                  s.url && s.url !== 'about:blank'
                    ? s.url
                    : tabStateRef.current.url || initialUrlRef.current;
                tabStateRef.current = {
                  width: s.width || tabStateRef.current.width,
                  height: s.height || tabStateRef.current.height,
                  url: stateUrl,
                  title: s.title || '',
                  loading: s.loading ?? false,
                  canGoBack: s.canGoBack ?? false,
                  canGoForward: s.canGoForward ?? false,
                };
                if (s.cursor) {
                  setCursorStyle(s.cursor);
                }
                onStateChangeRef.current?.({
                  url: stateUrl,
                  title: s.title || '',
                  favicon: s.favicon || undefined,
                  loading: s.loading ?? false,
                  canGoBack: s.canGoBack ?? false,
                  canGoForward: s.canGoForward ?? false,
                });
              } else if (payload.type === 'screenshot' && payload.requestId) {
                const resolver = pendingCapturesRef.current.get(payload.requestId);
                if (resolver) {
                  pendingCapturesRef.current.delete(payload.requestId);
                  resolver(typeof payload.data === 'string' ? payload.data : null);
                }
              } else if (payload.type === 'error') {
                const msg = payload.message || 'Browser session error';
                setErrorMessage(msg);
                onErrorRef.current?.(msg);
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
          onErrorRef.current?.(msg);
        };

        ws.onclose = () => {
          if (unmounted) return;
          for (const [, resolver] of pendingCapturesRef.current.entries()) {
            resolver(null);
          }
          pendingCapturesRef.current.clear();
          if (tabStateRef.current.loading) {
            tabStateRef.current.loading = false;
            onStateChangeRef.current?.({
              url: tabStateRef.current.url,
              title: tabStateRef.current.title,
              loading: false,
              canGoBack: tabStateRef.current.canGoBack,
              canGoForward: tabStateRef.current.canGoForward,
            });
          }
        };
      } catch (err: unknown) {
        if (unmounted) return;
        const msg =
          err instanceof Error ? err.message : String(err) || 'Failed to start headless Chromium';
        setErrorMessage(msg);
        onErrorRef.current?.(msg);
      }
    }

    void init();

    return () => {
      unmounted = true;
      for (const [, resolver] of pendingCapturesRef.current.entries()) {
        resolver(null);
      }
      pendingCapturesRef.current.clear();
      if (ws) {
        ws.close();
        if (wsRef.current === ws) {
          wsRef.current = null;
        }
      }
      if (createdSessionId) {
        if (sessionRef.current?.id === createdSessionId) {
          sessionRef.current = null;
        }
        void BrowserClose(createdSessionId);
      }
    };
  }, [sessionThreadKey, reconnectNonce]);

  // Debounced ResizeObserver to track container dimension changes
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let resizeTimer: number | null = null;
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const { width, height } = entry.contentRect;
        if (width < 50 || height < 50) continue;

        if (resizeTimer !== null) {
          window.clearTimeout(resizeTimer);
        }
        resizeTimer = window.setTimeout(() => {
          const dpr = Math.max(1, Math.min(window.devicePixelRatio || 1, 2.5));
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

  // Cancel any pending rAF batches on unmount
  useEffect(() => {
    return () => {
      if (moveRafRef.current !== null) cancelAnimationFrame(moveRafRef.current);
      if (wheelRafRef.current !== null) cancelAnimationFrame(wheelRafRef.current);
    };
  }, []);

  // Compute exact rendered image coordinates inside object-contain canvas
  const getCoordinates = useCallback((clientX: number, clientY: number) => {
    const canvas = canvasRef.current;
    if (!canvas) return { x: 0, y: 0 };
    const rect = canvas.getBoundingClientRect();
    const emulatedWidth = tabStateRef.current.width || canvas.width || 1280;
    const emulatedHeight = tabStateRef.current.height || canvas.height || 800;

    const boxW = rect.width || 1;
    const boxH = rect.height || 1;
    const srcW = canvas.width || emulatedWidth;
    const srcH = canvas.height || emulatedHeight;
    const srcAspect = srcW / (srcH || 1);
    const boxAspect = boxW / boxH;

    let drawW = boxW;
    let drawH = boxH;
    let offsetX = 0;
    let offsetY = 0;
    if (boxAspect > srcAspect) {
      drawH = boxH;
      drawW = boxH * srcAspect;
      offsetX = (boxW - drawW) / 2;
    } else {
      drawW = boxW;
      drawH = boxW / srcAspect;
      offsetY = (boxH - drawH) / 2;
    }

    const relX = (clientX - rect.left - offsetX) / (drawW || 1);
    const relY = (clientY - rect.top - offsetY) / (drawH || 1);
    return {
      x: Math.max(0, Math.min(emulatedWidth, relX * emulatedWidth)),
      y: Math.max(0, Math.min(emulatedHeight, relY * emulatedHeight)),
    };
  }, []);

  const getModifiers = (e: {
    altKey: boolean;
    ctrlKey: boolean;
    metaKey: boolean;
    shiftKey: boolean;
  }) => {
    let modifiers = 0;
    if (e.altKey) modifiers |= 1;
    if (e.ctrlKey) modifiers |= 2;
    if (e.metaKey) modifiers |= 4;
    if (e.shiftKey) modifiers |= 8;
    return modifiers;
  };

  // Non-passive native wheel listener to block outer scroll & rAF-batch deltaMode-scaled wheel events
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const onWheel = (e: WheelEvent) => {
      if (!isFocusedRef.current) return;
      e.preventDefault();
      e.stopPropagation();

      const { x, y } = getCoordinates(e.clientX, e.clientY);
      let scale = 1;
      if (e.deltaMode === 1) {
        scale = 32;
      } else if (e.deltaMode === 2) {
        scale = tabStateRef.current.height || container.clientHeight || 800;
      }

      const dx = e.deltaX * scale;
      const dy = e.deltaY * scale;
      const modifiers = getModifiers(e);

      if (pendingWheelRef.current) {
        pendingWheelRef.current.x = x;
        pendingWheelRef.current.y = y;
        pendingWheelRef.current.deltaX += dx;
        pendingWheelRef.current.deltaY += dy;
        pendingWheelRef.current.modifiers = modifiers;
      } else {
        pendingWheelRef.current = { x, y, deltaX: dx, deltaY: dy, modifiers };
      }

      if (wheelRafRef.current === null) {
        wheelRafRef.current = requestAnimationFrame(() => {
          wheelRafRef.current = null;
          const w = pendingWheelRef.current;
          pendingWheelRef.current = null;
          if (!w) return;
          send({
            type: 'mouse',
            mouseType: 'mouseWheel',
            x: w.x,
            y: w.y,
            deltaX: w.deltaX,
            deltaY: w.deltaY,
            modifiers: w.modifiers,
          });
        });
      }
    };

    container.addEventListener('wheel', onWheel, { passive: false });
    return () => {
      container.removeEventListener('wheel', onWheel);
    };
  }, [getCoordinates, send]);

  const mapButton = (b: number): string => {
    if (b === 0) return 'left';
    if (b === 1) return 'middle';
    if (b === 2) return 'right';
    if (b === 3) return '3';
    if (b === 4) return '4';
    return 'none';
  };

  const flushPendingMove = () => {
    if (moveRafRef.current !== null) {
      cancelAnimationFrame(moveRafRef.current);
      moveRafRef.current = null;
    }
    const m = pendingMoveRef.current;
    pendingMoveRef.current = null;
    if (m) {
      send({
        type: 'mouse',
        mouseType: 'mouseMoved',
        x: m.x,
        y: m.y,
        button: 'none',
        buttons: m.buttons,
        modifiers: m.modifiers,
      });
    }
  };

  const handlePointerDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!isFocused) return;
    e.stopPropagation();
    if (e.button === 3 || e.button === 4) {
      e.preventDefault();
    }
    flushPendingMove();
    try {
      e.currentTarget.setPointerCapture(e.pointerId);
    } catch {
      // Ignore if synthetic event cannot capture pointer
    }
    const { x, y } = getCoordinates(e.clientX, e.clientY);
    canvasRef.current?.focus({ preventScroll: true });

    const now = performance.now();
    const prev = lastClickRef.current;
    const isMulti =
      prev.button === e.button &&
      now - prev.time < 420 &&
      Math.hypot(x - prev.x, y - prev.y) < 8;
    const clickCount = isMulti ? Math.min(prev.count + 1, 3) : 1;
    lastClickRef.current = { time: now, x, y, button: e.button, count: clickCount };

    send({
      type: 'mouse',
      mouseType: 'mousePressed',
      x,
      y,
      button: mapButton(e.button),
      buttons: e.buttons,
      clickCount,
      modifiers: getModifiers(e),
    });
  };

  const handlePointerUp = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!isFocused) return;
    e.stopPropagation();
    if (e.button === 3 || e.button === 4) {
      e.preventDefault();
    }
    flushPendingMove();
    try {
      if (e.currentTarget.hasPointerCapture(e.pointerId)) {
        e.currentTarget.releasePointerCapture(e.pointerId);
      }
    } catch {
      // Ignore release errors
    }
    const { x, y } = getCoordinates(e.clientX, e.clientY);
    const clickCount = lastClickRef.current.button === e.button ? lastClickRef.current.count || 1 : 1;
    send({
      type: 'mouse',
      mouseType: 'mouseReleased',
      x,
      y,
      button: mapButton(e.button),
      buttons: e.buttons,
      clickCount,
      modifiers: getModifiers(e),
    });
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!isFocused) return;
    const { x, y } = getCoordinates(e.clientX, e.clientY);
    pendingMoveRef.current = {
      x,
      y,
      buttons: e.buttons,
      modifiers: getModifiers(e),
    };
    if (moveRafRef.current === null) {
      moveRafRef.current = requestAnimationFrame(() => {
        moveRafRef.current = null;
        const m = pendingMoveRef.current;
        pendingMoveRef.current = null;
        if (!m) return;
        send({
          type: 'mouse',
          mouseType: 'mouseMoved',
          x: m.x,
          y: m.y,
          button: 'none',
          buttons: m.buttons,
          modifiers: m.modifiers,
        });
      });
    }
  };

  // Keyboard handlers
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (!isFocused) return;
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;

    // Handle paste shortcut (Ctrl+V / Cmd+V)
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'v') {
      return; // Handled by handlePaste
    }

    if (PREVENT_KEYS.has(e.key) || PREVENT_KEYS.has(e.code)) {
      e.preventDefault();
      e.stopPropagation();
    }

    const hasShortcutMod = e.ctrlKey || e.metaKey || e.altKey;
    const text =
      e.key === 'Enter'
        ? '\r'
        : hasShortcutMod
          ? ''
          : e.key.length === 1
            ? e.key
            : '';

    send({
      type: 'key',
      keyType: 'keyDown',
      key: e.key,
      code: e.code,
      text,
      unmodifiedText: text,
      windowsVirtualKeyCode: e.keyCode,
      nativeVirtualKeyCode: e.keyCode,
      modifiers: getModifiers(e),
    });
  };

  const handleKeyUp = (e: React.KeyboardEvent) => {
    if (!isFocused) return;
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;

    if (PREVENT_KEYS.has(e.key) || PREVENT_KEYS.has(e.code)) {
      e.preventDefault();
      e.stopPropagation();
    }

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
      className={`relative w-full h-full min-h-0 overflow-hidden bg-[#0A0A0A] flex items-center justify-center ${className}`}
    >
      <canvas
        ref={canvasRef}
        tabIndex={0}
        onPointerDown={handlePointerDown}
        onPointerUp={handlePointerUp}
        onPointerMove={handlePointerMove}
        onKeyDown={handleKeyDown}
        onKeyUp={handleKeyUp}
        onPaste={handlePaste}
        onContextMenu={(e) => {
          if (isFocused) e.preventDefault();
        }}
        className="w-full h-full block object-contain focus:outline-none"
        style={{
          touchAction: 'none',
          imageRendering: 'auto',
          cursor: cursorStyle,
        }}
      />

      {!hideErrorOverlay && errorMessage && (
        <div className="absolute inset-0 z-30 bg-[#0A0A0A] flex flex-col items-center justify-center text-center p-6">
          <div className="w-10 h-10 rounded-xl bg-red-500/10 border border-red-500/20 flex items-center justify-center mb-3 text-red-400">
            <CircleAlert size={20} className="text-red-400" />
          </div>
          <h4 className="text-[13.5px] font-semibold text-white/90 mb-1">
            Browser Unavailable
          </h4>
          <p className="text-[12px] text-white/50 max-w-sm mb-4 leading-relaxed font-mono">
            {errorMessage}
          </p>
          <button
            type="button"
            onClick={retry}
            className="px-3.5 py-1.5 rounded-[8px] bg-white/10 hover:bg-white/18 text-white/90 text-[12px] font-medium flex items-center gap-1.5 active:scale-95 transition-all cursor-pointer border border-white/10"
          >
            <RefreshCw size={13} />
            <span>Retry</span>
          </button>
        </div>
      )}
    </div>
  );
};

export default CdpBrowserSurface;
