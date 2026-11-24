import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { BrowserOpenURL, ClipboardSetText, EventsOn } from '../../../wailsjs/runtime/runtime';
import { BrowserListTabs, ListProjects } from '../../../wailsjs/go/main/App';
import { servers } from '../../../wailsjs/go/models';
import {
  Check,
  Copy,
  Globe,
  MaterialIcon,
  Monitor,
  Plus,
  RefreshCw,
  Send,
  Smartphone,
  Square,
  Trash2,
  X,
} from '../common/icons';
import { useWebBookmarks, getDomain, getFaviconUrl, rememberBrowserFavicon, type WebBookmark } from './useWebBookmarks';
import { BookmarkModal } from './BookmarkModal';
import { CdpBrowserAction, CdpBrowserSurface } from '../browser/CdpBrowserSurface';

export interface BrowserTabItem {
  id: string;
  sessionId?: string;
  url: string;
  title: string;
  favicon: string;
  loading: boolean;
  canGoBack: boolean;
  canGoForward: boolean;
  error: string | null;
}

export type ViewportPreset = 'responsive' | 1440 | 768 | 390;
type AnnotationTool = 'interact' | 'draw' | 'rect' | 'text';

interface StrokeAnnotation {
  points: Array<{ x: number; y: number }>;
  color: string;
  width: number;
}

interface RectAnnotation {
  x: number;
  y: number;
  w: number;
  h: number;
  color: string;
  width: number;
}

interface TextAnnotation {
  x: number;
  y: number;
  text: string;
  color: string;
}

interface AnnotationState {
  strokes: StrokeAnnotation[];
  rects: RectAnnotation[];
  texts: TextAnnotation[];
}

const EMPTY_ANNOTATIONS: AnnotationState = { strokes: [], rects: [], texts: [] };
const ANNOTATION_COLOR = '#f43f5e';

export interface AgentBrowserViewProps {
  threadId?: string;
  detectedServers?: servers.Server[];
  isFocused?: boolean;
  isAnimating?: boolean;
  onFocusCard?: () => void;
  toolbarHost?: HTMLElement | null;
  immersive?: boolean;
  onEnterImmersive?: () => void;
  isSplit?: boolean;
  onToggleSplit?: () => void;
  onCloseBrowser?: () => void;
  onAttachImage?: (dataUrl: string, filename: string) => void | Promise<void>;
  className?: string;
  targetUrl?: string;
  targetUrlNonce?: number;
  projectName?: string;
  projectPath?: string;
}

export function smartNormalizeUrl(input: string, fallbackPort?: number): string {
  const trimmed = input.trim();
  if (!trimmed) {
    return fallbackPort ? `http://localhost:${fallbackPort}` : 'about:blank';
  }
  if (trimmed.toLowerCase() === 'about:blank') {
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

  // Search query
  return `https://www.google.com/search?q=${encodeURIComponent(trimmed)}`;
}

function makeTab(url: string, id?: string): BrowserTabItem {
  const cleanUrl = url || 'about:blank';
  const isBlank = cleanUrl === 'about:blank';
  return {
    id: id || `tab-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
    url: cleanUrl,
    title: isBlank ? 'New Tab' : getDomain(cleanUrl),
    favicon: isBlank || cleanUrl.includes('localhost') || cleanUrl.includes('127.0.0.1') ? '' : getFaviconUrl(cleanUrl),
    loading: false,
    canGoBack: false,
    canGoForward: false,
    error: null,
  };
}

function drawAnnotationsToContext(
  ctx: CanvasRenderingContext2D,
  width: number,
  height: number,
  ann: AnnotationState,
  draftStroke?: StrokeAnnotation | null,
  draftRect?: RectAnnotation | null,
) {
  const scale = Math.max(1, width / 900);

  const drawStroke = (s: StrokeAnnotation) => {
    if (s.points.length < 2) return;
    ctx.save();
    ctx.strokeStyle = s.color;
    ctx.lineWidth = s.width * scale;
    ctx.lineCap = 'round';
    ctx.lineJoin = 'round';
    ctx.beginPath();
    ctx.moveTo(s.points[0].x * width, s.points[0].y * height);
    for (let i = 1; i < s.points.length; i++) {
      ctx.lineTo(s.points[i].x * width, s.points[i].y * height);
    }
    ctx.stroke();
    ctx.restore();
  };

  const drawRect = (r: RectAnnotation) => {
    ctx.save();
    ctx.strokeStyle = r.color;
    ctx.lineWidth = r.width * scale;
    ctx.fillStyle = 'rgba(244, 63, 94, 0.08)';
    const rx = r.x * width;
    const ry = r.y * height;
    const rw = r.w * width;
    const rh = r.h * height;
    ctx.fillRect(rx, ry, rw, rh);
    ctx.strokeRect(rx, ry, rw, rh);
    ctx.restore();
  };

  for (const s of ann.strokes) drawStroke(s);
  if (draftStroke) drawStroke(draftStroke);
  for (const r of ann.rects) drawRect(r);
  if (draftRect) drawRect(draftRect);

  for (const t of ann.texts) {
    if (!t.text.trim()) continue;
    ctx.save();
    const fontSize = Math.round(13 * scale);
    ctx.font = `600 ${fontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
    const padX = 8 * scale;
    const padY = 5 * scale;
    const metrics = ctx.measureText(t.text);
    const boxW = metrics.width + padX * 2;
    const boxH = fontSize + padY * 2;
    const tx = Math.min(Math.max(4, t.x * width), Math.max(4, width - boxW - 4));
    const ty = Math.min(Math.max(4, t.y * height), Math.max(4, height - boxH - 4));

    ctx.fillStyle = 'rgba(15, 15, 18, 0.92)';
    ctx.strokeStyle = t.color;
    ctx.lineWidth = 1.5 * scale;
    ctx.beginPath();
    if (typeof ctx.roundRect === 'function') {
      ctx.roundRect(tx, ty, boxW, boxH, 6 * scale);
    } else {
      ctx.rect(tx, ty, boxW, boxH);
    }
    ctx.fill();
    ctx.stroke();

    ctx.fillStyle = '#ffffff';
    ctx.textBaseline = 'middle';
    ctx.fillText(t.text, tx + padX, ty + boxH / 2 + 0.5 * scale);
    ctx.restore();
  }
}

export const AgentBrowserView: React.FC<AgentBrowserViewProps> = ({
  threadId,
  detectedServers = [],
  isFocused = true,
  isAnimating = false,
  onFocusCard,
  isSplit = false,
  onToggleSplit,
  onCloseBrowser,
  onAttachImage,
  className = '',
  targetUrl,
  targetUrlNonce,
  projectName,
}) => {
  const {
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

  const webServers = useMemo(
    () => detectedServers.filter((s) => !s.agent && s.port > 0),
    [detectedServers],
  );
  const primaryServer = webServers[0];
  const initialDefaultUrl =
    targetUrl || (primaryServer ? `http://localhost:${primaryServer.port}` : 'about:blank');

  const [tabs, setTabs] = useState<BrowserTabItem[]>(() => [makeTab(initialDefaultUrl, 'default')]);
  const [activeTabId, setActiveTabId] = useState<string>('default');

  const activeTab = useMemo(
    () => tabs.find((t) => t.id === activeTabId) ?? tabs[0],
    [tabs, activeTabId],
  );

  const [inputUrl, setInputUrl] = useState<string>(() =>
    initialDefaultUrl === 'about:blank' ? '' : initialDefaultUrl,
  );
  const isInputEditingRef = useRef<boolean>(false);

  const tabActionsRef = useRef<Map<string, CdpBrowserAction | null>>(new Map());
  const actionRefForTab = useCallback((id: string) => {
    return {
      get current() {
        return tabActionsRef.current.get(id) ?? null;
      },
      set current(val: CdpBrowserAction | null) {
        if (val) {
          tabActionsRef.current.set(id, val);
        } else {
          tabActionsRef.current.delete(id);
        }
      },
    } as React.MutableRefObject<CdpBrowserAction | null>;
  }, []);

  const tabActionRefs = useRef<Map<string, React.MutableRefObject<CdpBrowserAction | null>>>(new Map());
  const getTabActionRef = useCallback(
    (id: string) => {
      let refObj = tabActionRefs.current.get(id);
      if (!refObj) {
        refObj = actionRefForTab(id);
        tabActionRefs.current.set(id, refObj);
      }
      return refObj;
    },
    [actionRefForTab],
  );

  // Sync address bar text when switching tabs or updating active tab URL
  useEffect(() => {
    if (!activeTab || isInputEditingRef.current) return;
    setInputUrl(activeTab.url === 'about:blank' ? '' : activeTab.url);
  }, [activeTab?.id, activeTab?.url]);

  // Adopt/attach to CDP tabs opened by an agent for this thread
  const adoptBackendTab = useCallback(
    (info: { id?: string; url?: string; title?: string; loading?: boolean; canGoBack?: boolean; canGoForward?: boolean }) => {
      if (!info?.id) return;
      const sid = info.id;
      const tabUrl = info.url && info.url !== 'about:blank' ? info.url : 'about:blank';
      setTabs((prev) => {
        const existing = prev.find((t) => t.sessionId === sid || t.id === sid);
        if (existing) {
          return prev.map((t) =>
            t.id === existing.id
              ? {
                  ...t,
                  sessionId: sid,
                  url: tabUrl !== 'about:blank' ? tabUrl : t.url,
                  title: info.title || t.title,
                  loading: info.loading ?? t.loading,
                }
              : t,
          );
        }
        const nextTab: BrowserTabItem = {
          id: sid,
          sessionId: sid,
          url: tabUrl,
          title: info.title || (tabUrl !== 'about:blank' ? getDomain(tabUrl) : 'Browser'),
          favicon: '',
          loading: Boolean(info.loading),
          canGoBack: Boolean(info.canGoBack),
          canGoForward: Boolean(info.canGoForward),
          error: null,
        };
        if (prev.length === 1 && prev[0].id === 'default' && (!prev[0].url || prev[0].url === 'about:blank')) {
          return [nextTab];
        }
        return [...prev, nextTab];
      });
      setActiveTabId(sid);
    },
    [],
  );

  useEffect(() => {
    if (!threadId || threadId === 'global') return;
    let active = true;
    BrowserListTabs(threadId)
      .then((list) => {
        if (!active || !Array.isArray(list) || list.length === 0) return;
        adoptBackendTab(list[list.length - 1]);
      })
      .catch(() => {});

    const off = EventsOn(
      'browser:agent_tab_opened',
      (payload: { threadId?: string; tab?: { id?: string; url?: string; title?: string; loading?: boolean; canGoBack?: boolean; canGoForward?: boolean } }) => {
        if (!payload || payload.threadId !== threadId || !payload.tab?.id) return;
        adoptBackendTab(payload.tab);
      },
    );
    return () => {
      active = false;
      if (typeof off === 'function') off();
    };
  }, [threadId, adoptBackendTab]);

  const [viewportPreset, setViewportPreset] = useState<ViewportPreset>('responsive');
  const [overflowOpen, setOverflowOpen] = useState(false);
  const [copiedUrl, setCopiedUrl] = useState(false);
  const overflowRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!overflowOpen) return;
    const onPointer = (e: PointerEvent) => {
      if (overflowRef.current && !overflowRef.current.contains(e.target as Node)) {
        setOverflowOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOverflowOpen(false);
    };
    document.addEventListener('pointerdown', onPointer, true);
    window.addEventListener('keydown', onKey, true);
    return () => {
      document.removeEventListener('pointerdown', onPointer, true);
      window.removeEventListener('keydown', onKey, true);
    };
  }, [overflowOpen]);

  // Annotation & capture-to-chat state
  const [tool, setTool] = useState<AnnotationTool>('interact');
  const [annotations, setAnnotations] = useState<AnnotationState>(EMPTY_ANNOTATIONS);
  const [textDraft, setTextDraft] = useState<{ x: number; y: number; value: string } | null>(null);
  const [isCapturing, setIsCapturing] = useState(false);
  const annotationCanvasRef = useRef<HTMLCanvasElement>(null);
  const viewportFrameRef = useRef<HTMLDivElement>(null);
  const draftStrokeRef = useRef<StrokeAnnotation | null>(null);
  const draftRectRef = useRef<RectAnnotation | null>(null);
  const dragStartRef = useRef<{ x: number; y: number } | null>(null);

  const hasAnnotations =
    annotations.strokes.length > 0 ||
    annotations.rects.length > 0 ||
    annotations.texts.length > 0;

  const redrawAnnotations = useCallback(() => {
    const canvas = annotationCanvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    drawAnnotationsToContext(
      ctx,
      canvas.width,
      canvas.height,
      annotations,
      draftStrokeRef.current,
      draftRectRef.current,
    );
  }, [annotations]);

  useEffect(() => {
    const frame = viewportFrameRef.current;
    const canvas = annotationCanvasRef.current;
    if (!frame || !canvas) return;
    const syncSize = () => {
      const rect = frame.getBoundingClientRect();
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      const w = Math.max(1, Math.round(rect.width * dpr));
      const h = Math.max(1, Math.round(rect.height * dpr));
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
      }
      redrawAnnotations();
    };
    syncSize();
    const ro = new ResizeObserver(syncSize);
    ro.observe(frame);
    return () => ro.disconnect();
  }, [redrawAnnotations, tool, activeTab?.url]);

  useEffect(() => {
    redrawAnnotations();
  }, [redrawAnnotations]);

  const updateTab = useCallback((id: string, patch: Partial<BrowserTabItem>) => {
    setTabs((prev) => prev.map((t) => (t.id === id ? { ...t, ...patch } : t)));
  }, []);

  const navigateTab = useCallback(
    (id: string, rawTarget: string) => {
      const normalized = smartNormalizeUrl(rawTarget, primaryServer?.port);
      updateTab(id, {
        url: normalized,
        title: normalized === 'about:blank' ? 'New Tab' : getDomain(normalized),
        favicon:
          normalized === 'about:blank' ||
          normalized.includes('localhost') ||
          normalized.includes('127.0.0.1')
            ? ''
            : getFaviconUrl(normalized),
        error: null,
      });
      if (id === activeTab?.id) {
        setInputUrl(normalized === 'about:blank' ? '' : normalized);
      }
      tabActionsRef.current.get(id)?.navigate(normalized);
    },
    [primaryServer?.port, updateTab, activeTab?.id],
  );

  // Navigate when external targetUrl / targetUrlNonce changes
  const lastTargetNonce = useRef<number | undefined>(targetUrlNonce);
  useEffect(() => {
    if (targetUrl && targetUrlNonce !== lastTargetNonce.current) {
      lastTargetNonce.current = targetUrlNonce;
      if (activeTab) {
        navigateTab(activeTab.id, targetUrl);
      }
    }
  }, [targetUrl, targetUrlNonce, activeTab, navigateTab]);

  const handleNewTab = () => {
    const nt = makeTab('about:blank');
    setTabs((prev) => [...prev, nt]);
    setActiveTabId(nt.id);
    setInputUrl('');
  };

  const handleCloseTab = (e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    setTabs((prev) => {
      if (prev.length <= 1) {
        const blank = makeTab('about:blank', prev[0]?.id || 'default');
        setActiveTabId(blank.id);
        setInputUrl('');
        return [blank];
      }
      const idx = prev.findIndex((t) => t.id === id);
      const next = prev.filter((t) => t.id !== id);
      if (activeTabId === id) {
        const fallback = next[Math.min(idx, next.length - 1)];
        if (fallback) {
          setActiveTabId(fallback.id);
          setInputUrl(fallback.url === 'about:blank' ? '' : fallback.url);
        }
      }
      return next;
    });
    tabActionsRef.current.delete(id);
    tabActionRefs.current.delete(id);
  };

  const handleInputKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && activeTab) {
      e.preventDefault();
      navigateTab(activeTab.id, inputUrl);
      e.currentTarget.blur();
    }
  };

  const activeSurfaceAction = activeTab ? tabActionsRef.current.get(activeTab.id) ?? null : null;
  const currentUrl = activeTab?.url || 'about:blank';
  const isBlank = !currentUrl || currentUrl === 'about:blank';
  const isLocalhost = currentUrl.includes('localhost') || currentUrl.includes('127.0.0.1');
  const shouldPause = !isFocused;

  const handleBack = () => activeSurfaceAction?.back();
  const handleForward = () => activeSurfaceAction?.forward();
  const handleReload = () => activeSurfaceAction?.reload();
  const handleRetry = () => {
    if (!activeTab) return;
    updateTab(activeTab.id, { error: null });
    activeSurfaceAction?.retry();
  };

  const handleOpenExternal = () => {
    if (!isBlank) {
      void BrowserOpenURL(currentUrl);
    } else if (primaryServer) {
      void BrowserOpenURL(`http://localhost:${primaryServer.port}`);
    }
  };

  const handleCopyUrl = async () => {
    if (isBlank) return;
    try {
      await ClipboardSetText(currentUrl);
    } catch {
      await navigator.clipboard?.writeText(currentUrl).catch(() => {});
    }
    setCopiedUrl(true);
    window.setTimeout(() => setCopiedUrl(false), 1500);
  };

  const handleStarClick = () => {
    const existing = getBookmarkByUrl(currentUrl);
    if (existing) {
      setEditingBookmark(existing);
    } else {
      setEditingBookmark({
        id: '',
        title: activeTab?.title || (inputUrl ? getDomain(inputUrl) : ''),
        url: !isBlank ? currentUrl : 'https://',
        project: projectName || null,
        createdAt: 0,
      });
    }
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

  const getNormPoint = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    return {
      x: Math.max(0, Math.min(1, (e.clientX - rect.left) / (rect.width || 1))),
      y: Math.max(0, Math.min(1, (e.clientY - rect.top) / (rect.height || 1))),
    };
  };

  const handleAnnotationPointerDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (tool === 'interact') return;
    e.preventDefault();
    e.stopPropagation();
    const pt = getNormPoint(e);

    if (tool === 'text') {
      setTextDraft({ x: pt.x, y: pt.y, value: '' });
      return;
    }

    try {
      e.currentTarget.setPointerCapture(e.pointerId);
    } catch {
      // Ignore
    }
    dragStartRef.current = pt;

    if (tool === 'draw') {
      draftStrokeRef.current = { points: [pt], color: ANNOTATION_COLOR, width: 3 };
      redrawAnnotations();
    } else if (tool === 'rect') {
      draftRectRef.current = { x: pt.x, y: pt.y, w: 0, h: 0, color: ANNOTATION_COLOR, width: 2.5 };
      redrawAnnotations();
    }
  };

  const handleAnnotationPointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!dragStartRef.current) return;
    const pt = getNormPoint(e);
    if (tool === 'draw' && draftStrokeRef.current) {
      draftStrokeRef.current.points.push(pt);
      redrawAnnotations();
    } else if (tool === 'rect' && draftRectRef.current) {
      const start = dragStartRef.current;
      draftRectRef.current = {
        x: Math.min(start.x, pt.x),
        y: Math.min(start.y, pt.y),
        w: Math.abs(pt.x - start.x),
        h: Math.abs(pt.y - start.y),
        color: ANNOTATION_COLOR,
        width: 2.5,
      };
      redrawAnnotations();
    }
  };

  const handleAnnotationPointerUp = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!dragStartRef.current) return;
    try {
      if (e.currentTarget.hasPointerCapture(e.pointerId)) {
        e.currentTarget.releasePointerCapture(e.pointerId);
      }
    } catch {
      // Ignore
    }
    dragStartRef.current = null;

    if (tool === 'draw' && draftStrokeRef.current) {
      const committed = draftStrokeRef.current;
      draftStrokeRef.current = null;
      if (committed.points.length > 1) {
        setAnnotations((prev) => ({ ...prev, strokes: [...prev.strokes, committed] }));
      }
    } else if (tool === 'rect' && draftRectRef.current) {
      const committed = draftRectRef.current;
      draftRectRef.current = null;
      if (committed.w > 0.005 || committed.h > 0.005) {
        setAnnotations((prev) => ({ ...prev, rects: [...prev.rects, committed] }));
      }
    }
    redrawAnnotations();
  };

  const commitTextDraft = () => {
    if (!textDraft) return;
    const value = textDraft.value.trim();
    if (value) {
      setAnnotations((prev) => ({
        ...prev,
        texts: [...prev.texts, { x: textDraft.x, y: textDraft.y, text: value, color: ANNOTATION_COLOR }],
      }));
    }
    setTextDraft(null);
  };

  const clearAnnotations = () => {
    draftStrokeRef.current = null;
    draftRectRef.current = null;
    dragStartRef.current = null;
    setTextDraft(null);
    setAnnotations(EMPTY_ANNOTATIONS);
    setTool('interact');
  };

  const handleSendToChat = async () => {
    if (isBlank || isCapturing) return;
    setIsCapturing(true);
    try {
      const shot = await activeSurfaceAction?.captureScreenshot();
      if (!shot) return;

      let finalDataUrl = shot;
      if (hasAnnotations) {
        finalDataUrl = await new Promise<string>((resolve) => {
          const img = new Image();
          img.onload = () => {
            const out = document.createElement('canvas');
            out.width = img.naturalWidth || 1280;
            out.height = img.naturalHeight || 800;
            const ctx = out.getContext('2d');
            if (ctx) {
              ctx.drawImage(img, 0, 0, out.width, out.height);
              drawAnnotationsToContext(ctx, out.width, out.height, annotations);
              try {
                resolve(out.toDataURL('image/png'));
                return;
              } catch {
                // Fallback to original shot
              }
            }
            resolve(shot);
          };
          img.onerror = () => resolve(shot);
          img.src = shot;
        });
      }

      clearAnnotations();
      await onAttachImage?.(finalDataUrl, 'browser-capture.png');
    } finally {
      setIsCapturing(false);
    }
  };

  const isCurrentBookmarked = isBookmarked(currentUrl);

  return (
    <div
      className={`flex flex-col h-full w-full min-h-0 select-none bg-[#0A0A0A] pt-[52px] ${className}`}
    >
      {/* Top Row: Multi-Tab Bar + Claude Desktop Top-Right Browser Controls */}
      <div className="h-[36px] shrink-0 px-2.5 flex items-center justify-between gap-2 border-b border-white/[0.06]">
        <div
          role="tablist"
          aria-label="Browser tabs"
          className="flex items-center gap-1 min-w-0 flex-1 overflow-x-auto no-scrollbar py-1"
        >
          {tabs.map((tab) => {
            const isActive = tab.id === activeTab?.id;
            const tabBlank = !tab.url || tab.url === 'about:blank';
            const tabLocal = tab.url.includes('localhost') || tab.url.includes('127.0.0.1');
            return (
              <div
                key={tab.id}
                role="tab"
                aria-selected={isActive}
                className={`group flex items-center gap-1 rounded-[8px] pr-1 transition-all duration-150 shrink-0 max-w-[190px] ${
                  isActive
                    ? 'bg-white/[0.1] text-white'
                    : 'text-white/50 hover:text-white/85 hover:bg-white/[0.04]'
                }`}
              >
                <button
                  type="button"
                  onClick={() => setActiveTabId(tab.id)}
                  title={tab.url === 'about:blank' ? 'New Tab' : `${tab.title} — ${tab.url}`}
                  className="flex items-center gap-1.5 pl-2.5 pr-1 py-1 text-[11.5px] font-medium min-w-0 flex-1 text-left cursor-pointer"
                >
                  {tab.loading ? (
                    <span className="w-3 h-3 rounded-full border border-white/60 border-t-transparent animate-spin shrink-0" />
                  ) : tab.favicon && !tabBlank && !tabLocal ? (
                    <img
                      src={tab.favicon}
                      alt=""
                      className="w-3.5 h-3.5 rounded-xs object-contain shrink-0"
                      onError={(e) => {
                        (e.currentTarget as HTMLElement).style.display = 'none';
                      }}
                    />
                  ) : tabLocal ? (
                    <Monitor size={12} className="text-emerald-400/80 shrink-0" />
                  ) : (
                    <Globe size={12} className="text-white/45 shrink-0" />
                  )}
                  <span className="truncate">{tab.title || 'New Tab'}</span>
                </button>
                <button
                  type="button"
                  onClick={(e) => handleCloseTab(e, tab.id)}
                  aria-label={`Close ${tab.title || 'tab'}`}
                  title="Close tab"
                  className="w-4 h-4 rounded-full flex items-center justify-center opacity-40 group-hover:opacity-100 hover:bg-white/15 transition-opacity cursor-pointer shrink-0"
                >
                  <X size={10} strokeWidth={2.2} />
                </button>
              </div>
            );
          })}

          <button
            type="button"
            onClick={handleNewTab}
            title="New tab"
            aria-label="New tab"
            className="w-[26px] h-[26px] rounded-[7px] flex items-center justify-center text-white/45 hover:text-white hover:bg-white/[0.06] transition-all cursor-pointer shrink-0"
          >
            <Plus size={13} strokeWidth={2} />
          </button>
        </div>

        {/* Top-Right Browser Controls: Split with Chat, ... Overflow Menu, Close Browser */}
        <div className="flex items-center gap-1 shrink-0">
          {onToggleSplit && (
            <button
              type="button"
              onClick={onToggleSplit}
              title={isSplit ? 'Expand browser full width' : 'Split with chat'}
              aria-label={isSplit ? 'Expand browser full width' : 'Split with chat'}
              className={`h-[26px] px-2 rounded-[7px] flex items-center gap-1.5 text-[11px] font-medium transition-all cursor-pointer border ${
                isSplit
                  ? 'bg-white/[0.14] text-white border-white/20'
                  : 'bg-white/[0.04] hover:bg-white/[0.1] text-white/65 hover:text-white border-white/[0.08]'
              }`}
            >
              <svg width="13" height="13" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <rect x="1.5" y="2.5" width="13" height="11" rx="2" stroke="currentColor" strokeWidth="1.4" />
                <line x1="8" y1="2.5" x2="8" y2="13.5" stroke="currentColor" strokeWidth="1.4" />
              </svg>
              <span>Split</span>
            </button>
          )}

          <div ref={overflowRef} className="relative">
            <button
              type="button"
              onClick={() => setOverflowOpen((v) => !v)}
              title="Browser options"
              aria-label="Browser options"
              aria-expanded={overflowOpen}
              className={`w-[26px] h-[26px] rounded-[7px] flex items-center justify-center transition-all cursor-pointer ${
                overflowOpen
                  ? 'bg-white/[0.14] text-white'
                  : 'text-white/60 hover:text-white hover:bg-white/[0.08]'
              }`}
            >
              <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
                <circle cx="3.2" cy="8" r="1.25" />
                <circle cx="8" cy="8" r="1.25" />
                <circle cx="12.8" cy="8" r="1.25" />
              </svg>
            </button>

            {overflowOpen && (
              <div className="absolute right-0 top-[32px] z-50 w-[210px] rounded-xl bg-[#141418] border border-white/12 shadow-[0_18px_48px_rgba(0,0,0,0.7)] p-1.5 text-[12px] text-white/85">
                <button
                  type="button"
                  onClick={() => {
                    setOverflowOpen(false);
                    handleRetry();
                  }}
                  className="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-[8px] hover:bg-white/[0.08] text-left cursor-pointer"
                >
                  <RefreshCw size={13} className="text-white/55" />
                  <span>Hard Reload</span>
                </button>
                <button
                  type="button"
                  disabled={isBlank}
                  onClick={() => {
                    void handleCopyUrl();
                    setOverflowOpen(false);
                  }}
                  className="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-[8px] hover:bg-white/[0.08] disabled:opacity-40 text-left cursor-pointer"
                >
                  {copiedUrl ? <Check size={13} className="text-emerald-400" /> : <Copy size={13} className="text-white/55" />}
                  <span>{copiedUrl ? 'Copied URL' : 'Copy URL'}</span>
                </button>
                <button
                  type="button"
                  onClick={() => {
                    handleOpenExternal();
                    setOverflowOpen(false);
                  }}
                  className="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-[8px] hover:bg-white/[0.08] text-left cursor-pointer"
                >
                  <MaterialIcon name="open_in_new" size={13} className="text-white/55" />
                  <span>Open in External Browser</span>
                </button>

                <div className="my-1 border-t border-white/[0.08]" />
                <div className="px-2.5 py-1 text-[10px] uppercase tracking-wider text-white/35 font-semibold">
                  Viewport Preset
                </div>
                {(
                  [
                    { id: 'responsive', label: 'Responsive / Full', icon: <Globe size={12} /> },
                    { id: 1440, label: 'Desktop 1440', icon: <Monitor size={12} /> },
                    { id: 768, label: 'Tablet 768', icon: <Monitor size={12} /> },
                    { id: 390, label: 'Mobile 390', icon: <Smartphone size={12} /> },
                  ] as Array<{ id: ViewportPreset; label: string; icon: React.ReactNode }>
                ).map((preset) => (
                  <button
                    key={String(preset.id)}
                    type="button"
                    onClick={() => {
                      setViewportPreset(preset.id);
                      setOverflowOpen(false);
                    }}
                    className="w-full flex items-center justify-between gap-2 px-2.5 py-1.5 rounded-[8px] hover:bg-white/[0.08] text-left cursor-pointer"
                  >
                    <span className="flex items-center gap-2 text-white/80">
                      <span className="text-white/50">{preset.icon}</span>
                      <span>{preset.label}</span>
                    </span>
                    {viewportPreset === preset.id && <Check size={12} className="text-blue-400" />}
                  </button>
                ))}
              </div>
            )}
          </div>

          {onCloseBrowser && (
            <button
              type="button"
              onClick={onCloseBrowser}
              title="Close browser"
              aria-label="Close browser"
              className="w-[26px] h-[26px] rounded-[7px] flex items-center justify-center text-white/55 hover:text-white hover:bg-white/[0.08] transition-all cursor-pointer"
            >
              <X size={14} strokeWidth={2} />
            </button>
          )}
        </div>
      </div>

      {/* Second Row: Address Bar + Dev Server Chips + External Button */}
      <div className="h-[40px] shrink-0 px-2.5 flex items-center gap-2 border-b border-white/[0.06]">
        <div className="flex items-center gap-0.5 shrink-0">
          {[
            {
              title: 'Back',
              icon: 'arrow_back',
              enabled: Boolean(activeTab?.canGoBack) && !isBlank,
              run: handleBack,
              spin: false,
            },
            {
              title: 'Forward',
              icon: 'arrow_forward',
              enabled: Boolean(activeTab?.canGoForward) && !isBlank,
              run: handleForward,
              spin: false,
            },
            {
              title: 'Reload',
              icon: 'refresh',
              enabled: !isBlank,
              run: handleReload,
              spin: Boolean(activeTab?.loading),
            },
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

        <div className="flex-1 min-w-[120px]">
          <div className="w-full h-[28px] px-2.5 rounded-full bg-white/[0.07] hover:bg-white/[0.1] focus-within:bg-white/[0.13] focus-within:ring-1 focus-within:ring-blue-400/50 flex items-center gap-1.5 transition-all duration-150">
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
              className="w-full bg-transparent text-[12px] font-medium font-(family-name:--app-font) text-white/90 placeholder:text-white/35 focus:outline-none tracking-tight select-text"
            />
            {activeTab?.loading && (
              <span className="w-2.5 h-2.5 rounded-full border border-white/60 border-t-transparent animate-spin shrink-0" />
            )}
            {!isBlank && (
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

        {/* Dev Server Quick-Switch Chips */}
        {webServers.length > 0 && (
          <div className="hidden md:flex items-center gap-1 shrink-0">
            {webServers.slice(0, 3).map((srv) => {
              const srvUrl = `http://localhost:${srv.port}`;
              const onPort = currentUrl.includes(`:${srv.port}`);
              return (
                <button
                  key={`${srv.pid}-${srv.port}`}
                  type="button"
                  onClick={() => activeTab && navigateTab(activeTab.id, srvUrl)}
                  title={`Open ${srvUrl}`}
                  className={`h-[24px] px-2 rounded-full text-[11px] font-mono flex items-center gap-1.5 transition-all cursor-pointer border ${
                    onPort
                      ? 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30'
                      : 'bg-white/[0.04] hover:bg-white/[0.09] text-white/60 hover:text-white border-white/[0.08]'
                  }`}
                >
                  <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 shrink-0" />
                  <span>:{srv.port}</span>
                </button>
              );
            })}
          </div>
        )}

        {viewportPreset !== 'responsive' && (
          <button
            type="button"
            onClick={() => setViewportPreset('responsive')}
            title="Reset viewport preset to Responsive"
            className="h-[24px] px-2 rounded-full bg-blue-500/15 border border-blue-500/30 text-blue-300 text-[10.5px] font-mono cursor-pointer shrink-0"
          >
            {viewportPreset}px ×
          </button>
        )}

        <button
          type="button"
          title="Open in external browser"
          aria-label="Open in external browser"
          onClick={handleOpenExternal}
          className="w-[26px] h-[26px] rounded-full flex items-center justify-center text-white/65 hover:text-white hover:bg-white/10 active:scale-90 transition-all duration-150 cursor-pointer shrink-0"
        >
          <MaterialIcon name="open_in_new" className="text-[13px]" />
        </button>
      </div>

      {/* Main Viewport Stage */}
      <div
        data-browser-viewport
        className="flex-1 min-h-0 relative overflow-hidden bg-[#050506] flex justify-center"
        style={{
          contain: 'strict',
          transform: 'translate3d(0, 0, 0)',
        }}
      >
        <div
          ref={viewportFrameRef}
          className={`relative h-full overflow-hidden bg-[#0A0A0A] ${
            viewportPreset === 'responsive' ? 'w-full' : 'border-x border-white/[0.08]'
          }`}
          style={
            viewportPreset === 'responsive'
              ? undefined
              : { width: `min(100%, ${viewportPreset}px)` }
          }
        >
          {/* Unfocused Click Shield */}
          {!isFocused && !isBlank && (
            <div
              onClick={(e) => {
                e.stopPropagation();
                onFocusCard?.();
              }}
              className="absolute inset-0 z-30 cursor-pointer bg-black/10 transition-opacity"
              title="Click to focus card"
            />
          )}

          {/* Animation Shield */}
          {isAnimating && (
            <div className="absolute inset-0 z-40 pointer-events-none bg-transparent" />
          )}

          {/* Paused Overlay */}
          {shouldPause && !isBlank && (
            <div
              onClick={(e) => {
                e.stopPropagation();
                onFocusCard?.();
              }}
              className="absolute inset-0 z-25 bg-[#0A0A0A]/92 backdrop-blur-md flex flex-col items-center justify-center text-center p-6 cursor-pointer group transition-all duration-200"
            >
              <div className="w-11 h-11 rounded-2xl bg-white/[0.06] border border-white/10 flex items-center justify-center mb-3 shadow-lg group-hover:scale-105 group-hover:bg-white/[0.1] transition-all duration-200">
                <MaterialIcon name="language" className="text-white/70 text-2xl" />
              </div>
              <div className="flex items-center gap-1.5 px-2.5 py-0.5 rounded-full bg-white/[0.08] border border-white/10 text-white/80 text-[11px] font-mono font-medium mb-2">
                <span className="w-1.5 h-1.5 rounded-full bg-white/70" />
                <span>Browser stream paused</span>
              </div>
              <p className="text-[12px] font-medium font-(family-name:--app-font) text-white/45 max-w-xs mb-3 leading-relaxed">
                Headless Chromium screencast halted to save CPU across inactive cards.
              </p>
              <div className="px-3.5 py-1.5 rounded-[8px] bg-white/[0.08] hover:bg-white/15 border border-white/15 text-white/90 text-[12px] font-medium flex items-center gap-1.5 transition-all shadow-md group-hover:border-white/30">
                <span>Click to resume</span>
                <MaterialIcon name="play_arrow" className="text-[14px]" />
              </div>
            </div>
          )}

          {/* Mounted CdpBrowserSurface per non-blank tab (hidden via CSS when inactive to preserve live state) */}
          {tabs.map((tab) => {
            if (!tab.url || tab.url === 'about:blank') return null;
            const isTabActive = tab.id === activeTab?.id;
            return (
              <CdpBrowserSurface
                key={tab.id}
                threadId={threadId}
                tabId={tab.id}
                existingSessionId={tab.sessionId}
                initialUrl={tab.url}
                paused={shouldPause || !isTabActive}
                isFocused={isFocused && !isAnimating && isTabActive && tool === 'interact'}
                hideErrorOverlay
                actionRef={getTabActionRef(tab.id)}
                onStateChange={(state) => {
                  const nextUrl =
                    state.url && state.url !== 'about:blank' ? state.url : tab.url;
                  if (state.favicon && nextUrl !== 'about:blank') {
                    rememberBrowserFavicon(nextUrl, state.favicon);
                  }
                  updateTab(tab.id, {
                    url: nextUrl,
                    title: state.title || (nextUrl !== 'about:blank' ? getDomain(nextUrl) : 'New Tab'),
                    favicon:
                      state.favicon ||
                      (nextUrl !== 'about:blank' &&
                      !nextUrl.includes('localhost') &&
                      !nextUrl.includes('127.0.0.1')
                        ? getFaviconUrl(nextUrl)
                        : ''),
                    loading: state.loading,
                    canGoBack: state.canGoBack,
                    canGoForward: state.canGoForward,
                    error: null,
                  });
                }}
                onError={(err) => updateTab(tab.id, { error: err })}
                className={isTabActive ? 'w-full h-full' : 'hidden'}
              />
            );
          })}

          {/* Blank / Start Tab Surface */}
          {isBlank && (
            <div className="w-full h-full bg-[#0A0A0A] flex flex-col items-center justify-center text-center p-6 select-none">
              <div className="w-12 h-12 rounded-2xl bg-white/[0.05] border border-white/10 flex items-center justify-center mb-3.5 shadow-lg">
                <MaterialIcon name="language" className="text-white/60 text-2xl" />
              </div>
              <h3 className="text-[15px] font-semibold font-(family-name:--app-font) text-white/90 tracking-tight mb-1">
                Browser
              </h3>
              <p className="text-[12px] font-medium font-(family-name:--app-font) text-white/45 max-w-xs mb-5 leading-relaxed">
                Enter a URL above or launch a local dev server or bookmark below.
              </p>

              {webServers.length > 0 && (
                <div className="flex flex-wrap items-center justify-center gap-2 max-w-lg mb-4">
                  {webServers.map((srv) => (
                    <button
                      key={`${srv.pid}-${srv.port}`}
                      type="button"
                      onClick={() =>
                        activeTab && navigateTab(activeTab.id, `http://localhost:${srv.port}`)
                      }
                      className="px-3.5 py-1.5 rounded-[10px] bg-emerald-500/10 hover:bg-emerald-500/20 border border-emerald-500/25 text-emerald-300 text-[12px] font-mono flex items-center gap-2 active:scale-95 transition-all cursor-pointer"
                    >
                      <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
                      <span>localhost:{srv.port}</span>
                    </button>
                  ))}
                </div>
              )}

              {visibleBookmarks.length > 0 && (
                <div className="flex flex-wrap items-center justify-center gap-2 max-w-lg">
                  {visibleBookmarks.slice(0, 8).map((bm) => (
                    <button
                      key={bm.id}
                      type="button"
                      onClick={() => activeTab && navigateTab(activeTab.id, bm.url)}
                      className="flex items-center gap-2 px-3 py-1.5 rounded-[10px] bg-white/[0.04] hover:bg-white/[0.08] border border-white/[0.08] hover:border-white/15 text-white/85 text-[12px] font-medium transition-all active:scale-95 cursor-pointer"
                    >
                      <img
                        src={getFaviconUrl(bm.url, bm.favicon)}
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
            </div>
          )}

          {/* Top Progress Bar */}
          {!isBlank && activeTab?.loading && (
            <div className="absolute top-0 inset-x-0 h-[2px] bg-black/20 overflow-hidden z-10 pointer-events-none">
              <div className="h-full w-1/3 bg-white/75 animate-[browser-progress_1.1s_ease-in-out_infinite]" />
            </div>
          )}

          {/* Single Unified Error Overlay */}
          {!isBlank && activeTab?.error && (
            <div className="absolute inset-0 bg-[#0A0A0A] flex flex-col items-center justify-center text-center p-6 z-20 select-none">
              <div className="w-10 h-10 rounded-xl bg-white/[0.06] border border-white/10 flex items-center justify-center mb-3">
                <MaterialIcon name="public_off" className="text-white/60 text-xl" />
              </div>
              <h4 className="text-[13.5px] font-semibold font-(family-name:--app-font) text-white/90 mb-1">
                Browser Surface Unavailable
              </h4>
              <p className="text-[12px] text-white/45 max-w-sm mb-4 leading-relaxed font-mono">
                {activeTab.error}
              </p>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={handleRetry}
                  className="px-3.5 py-1.5 rounded-[8px] bg-white/10 hover:bg-white/18 text-white/90 text-[12px] font-medium flex items-center gap-1.5 active:scale-95 transition-all cursor-pointer border border-white/10"
                >
                  <RefreshCw size={13} />
                  <span>Retry</span>
                </button>
                <button
                  type="button"
                  onClick={handleOpenExternal}
                  className="px-3.5 py-1.5 rounded-[8px] bg-white/[0.06] hover:bg-white/10 text-white/70 hover:text-white text-[12px] font-medium flex items-center gap-1.5 active:scale-95 transition-all cursor-pointer border border-white/10"
                >
                  <span>Open in Default Browser</span>
                  <MaterialIcon name="open_in_new" className="text-[13px]" />
                </button>
              </div>
            </div>
          )}

          {/* Transparent Annotation Canvas Overlay */}
          {!isBlank && (tool !== 'interact' || hasAnnotations) && (
            <canvas
              ref={annotationCanvasRef}
              onPointerDown={handleAnnotationPointerDown}
              onPointerMove={handleAnnotationPointerMove}
              onPointerUp={handleAnnotationPointerUp}
              className={`absolute inset-0 w-full h-full z-20 ${
                tool === 'interact'
                  ? 'pointer-events-none'
                  : tool === 'text'
                    ? 'pointer-events-auto cursor-text'
                    : 'pointer-events-auto cursor-crosshair'
              }`}
              style={{ touchAction: 'none' }}
            />
          )}

          {/* Inline Text Callout Editor */}
          {textDraft && (
            <div
              className="absolute z-30"
              style={{
                left: `${Math.min(82, Math.max(2, textDraft.x * 100))}%`,
                top: `${Math.min(85, Math.max(2, textDraft.y * 100))}%`,
              }}
            >
              <input
                autoFocus
                type="text"
                value={textDraft.value}
                placeholder="Note…"
                onChange={(e) => setTextDraft({ ...textDraft, value: e.target.value })}
                onKeyDown={(e) => {
                  e.stopPropagation();
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    commitTextDraft();
                  } else if (e.key === 'Escape') {
                    e.preventDefault();
                    setTextDraft(null);
                  }
                }}
                onBlur={commitTextDraft}
                className="h-[28px] min-w-[140px] px-2.5 rounded-lg bg-[#0f0f12]/95 border border-rose-500 text-white text-[12px] font-medium shadow-xl outline-none select-text"
              />
            </div>
          )}

          {/* Floating Bottom Annotation & Capture-to-Chat Toolbar */}
          {!isBlank && !activeTab?.error && (
            <div className="absolute bottom-3 left-1/2 -translate-x-1/2 z-30 flex items-center gap-1 px-2 py-1 rounded-full bg-[#111114]/90 backdrop-blur-md border border-white/15 shadow-[0_12px_32px_rgba(0,0,0,0.65)]">
              <button
                type="button"
                onClick={() => setTool('interact')}
                title="Interact"
                className={`h-[26px] px-2 rounded-full flex items-center gap-1 text-[11px] font-medium transition-colors cursor-pointer ${
                  tool === 'interact' ? 'bg-white/15 text-white' : 'text-white/60 hover:text-white hover:bg-white/10'
                }`}
              >
                <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
                  <path d="M3 2l9.5 6.2-4.4 1.1-2.1 4.2L3 2z" />
                </svg>
                <span className="hidden sm:inline">Interact</span>
              </button>

              <button
                type="button"
                onClick={() => setTool('draw')}
                title="Freehand draw"
                className={`h-[26px] px-2 rounded-full flex items-center gap-1 text-[11px] font-medium transition-colors cursor-pointer ${
                  tool === 'draw' ? 'bg-rose-500/25 text-rose-200 ring-1 ring-rose-400/50' : 'text-white/60 hover:text-white hover:bg-white/10'
                }`}
              >
                <MaterialIcon name="edit" size={12} />
                <span className="hidden sm:inline">Draw</span>
              </button>

              <button
                type="button"
                onClick={() => setTool('rect')}
                title="Rectangle highlight"
                className={`h-[26px] px-2 rounded-full flex items-center gap-1 text-[11px] font-medium transition-colors cursor-pointer ${
                  tool === 'rect' ? 'bg-rose-500/25 text-rose-200 ring-1 ring-rose-400/50' : 'text-white/60 hover:text-white hover:bg-white/10'
                }`}
              >
                <Square size={11} strokeWidth={2} />
                <span className="hidden sm:inline">Rect</span>
              </button>

              <button
                type="button"
                onClick={() => setTool('text')}
                title="Text callout"
                className={`h-[26px] px-2 rounded-full flex items-center gap-1 text-[11px] font-medium transition-colors cursor-pointer ${
                  tool === 'text' ? 'bg-rose-500/25 text-rose-200 ring-1 ring-rose-400/50' : 'text-white/60 hover:text-white hover:bg-white/10'
                }`}
              >
                <span className="font-serif font-bold text-[12px] leading-none">T</span>
                <span className="hidden sm:inline">Text</span>
              </button>

              {(hasAnnotations || tool !== 'interact') && (
                <button
                  type="button"
                  onClick={clearAnnotations}
                  title="Clear annotations & exit"
                  aria-label="Clear annotations"
                  className="w-[26px] h-[26px] rounded-full flex items-center justify-center text-white/60 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
                >
                  {hasAnnotations ? <Trash2 size={12} /> : <X size={12} />}
                </button>
              )}

              <div className="w-px h-4 bg-white/15 mx-0.5" />

              <button
                type="button"
                disabled={isCapturing}
                onClick={() => void handleSendToChat()}
                title="Capture screenshot & attach to chat"
                className="h-[26px] px-2.5 rounded-full bg-white text-black hover:bg-white/90 disabled:opacity-50 text-[11px] font-semibold flex items-center gap-1.5 transition-all active:scale-95 cursor-pointer"
              >
                <Send size={11} strokeWidth={2.2} />
                <span>{isCapturing ? 'Capturing…' : 'Send to Chat'}</span>
              </button>
            </div>
          )}
        </div>
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
