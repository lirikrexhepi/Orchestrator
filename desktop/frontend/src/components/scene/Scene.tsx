import React, { Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import {
  OrchestratorInput,
  useProjects,
  useSpawner,
  useCoordinator,
  useClaudeUpdate,
  OrchestratorCard,
  useOrchestratorStore,
} from '../orchestrator';
import { AgentWindow } from '../agent-session';
import { useAttachments } from '../common/useAttachments';
import { requestPinToBottom } from '../common/usePinnedScroll';
import { TitleBar } from '../common/TitleBar';
import { DynamicIsland, DynamicIslandNotification } from '../common/DynamicIsland';
import { useGit } from '../git';
import { useClaudeImport, useHistory } from '../history';
import {
  Sidebar,
  SidebarPanel,
  useDefaultModels,
  useServers,
  useUsage,
  useWallpaper,
} from '../sidebar';
import type { QueuedMessage } from './QueuedMessages';
import { LiquidGlass } from '../../liquid-glass';
import { useTheme } from '../../themes';
import { MutateAgentTask, RespondToQuestion } from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import { session } from '../../../wailsjs/go/models';
import { playTaskComplete } from '../../sound';
import { SubagentBlockData } from '../agent-session/types';

export interface SceneProps {
  children?: React.ReactNode;
}

const SCENE_BG_BASE: React.CSSProperties = {
  backgroundSize: 'cover',
  backgroundPosition: 'center',
  backgroundRepeat: 'no-repeat',
  transform: 'translateZ(0)',
  backfaceVisibility: 'hidden',
};

const SettingsPanel = React.lazy(() => import('../sidebar/SettingsPanel'));
const SettingsWindow = React.lazy(() => import('../sidebar/SettingsWindow'));



interface GridSlot {
  x: number;
  y: number;
  scale: number;
}

/**
 * Calculates exact Exposé 3x3 grid slot positions and uniform scale for N cards.
 * Uses 3 columns (3x3 grid matrix) for multi-card overview.
 * Coordinates (x, y) are centered around the stage center (0, 0).
 */
function computeGridSlots(count: number, stageW: number, stageH: number): GridSlot[] {
  if (count <= 0) return [];

  const cols = count === 1 ? 1 : 3;
  const rows = Math.max(1, Math.ceil(count / cols));
  const gapX = 22;
  const gapY = 22;

  const BASE_W = 680;
  const BASE_H = 740;

  const cellMaxW = (stageW - gapX * (cols - 1)) / cols;
  const cellMaxH = (stageH - gapY * (rows - 1)) / rows;

  const scaleW = cellMaxW / BASE_W;
  const scaleH = cellMaxH / BASE_H;
  // Uniform scale capped for sharpness
  const scale = count === 1 ? 0.76 : Math.min(scaleW, scaleH, 0.56);

  const cardW = BASE_W * scale;
  const cardH = BASE_H * scale;

  const totalGridW = cols * cardW + (cols - 1) * gapX;
  const totalGridH = rows * cardH + (rows - 1) * gapY;

  // Offset up slightly from center to keep comfortable clearance from the bottom Omnibar
  const startY = -totalGridH / 2 + cardH / 2 - 24;

  const slots: GridSlot[] = [];
  for (let i = 0; i < count; i++) {
    const row = Math.floor(i / cols);
    const itemsInThisRow = row === rows - 1 && count % cols !== 0 ? count % cols : cols;
    const colInRow = i % cols;
    const rowGridW = itemsInThisRow * cardW + (itemsInThisRow - 1) * gapX;
    const rowStartX = -rowGridW / 2 + cardW / 2;

    const x = rowStartX + colInRow * (cardW + gapX);
    const y = startY + row * (cardH + gapY);
    slots.push({
      x: Math.round(x),
      y: Math.round(y),
      scale: Number(scale.toFixed(3)),
    });
  }
  return slots;
}

const DECK_SPRING = 'linear(0, 0.019, 0.0678, 0.1365, 0.217, 0.303, 0.3899, 0.4744, 0.5541, 0.6274, 0.6936, 0.7522, 0.8033, 0.8471, 0.8842, 0.915, 0.9402, 0.9605, 0.9765, 0.9889, 0.9982, 1.0049, 1.0096, 1.0127, 1.0144, 1.0151, 1.0151, 1.0145, 1.0136, 1.0124, 1.0112, 1.0098, 1.0085, 1.0073, 1.0061, 1.0051, 1.0041, 1.0033, 1.0026, 1.002, 1)';
const DECK_MS = 640;

export const Scene: React.FC<SceneProps> = ({ children }) => {
  // Apple Dynamic Island Notification State
  const [islandNotification, setIslandNotification] = useState<DynamicIslandNotification | null>(null);
  const notificationTimerRef = useRef<number | null>(null);

  const triggerIslandNotification = useCallback(
    (notif: Omit<DynamicIslandNotification, 'id' | 'timestamp'>) => {
      if (notificationTimerRef.current) {
        window.clearTimeout(notificationTimerRef.current);
      }
      if (notif.type === 'success' && useOrchestratorStore.getState().interfaceSounds) {
        playTaskComplete();
      }
      const fullNotif: DynamicIslandNotification = {
        id: `${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
        timestamp: Date.now(),
        ...notif,
      };
      setIslandNotification(fullNotif);
      notificationTimerRef.current = window.setTimeout(() => {
        setIslandNotification(null);
      }, 5000);
    },
    [],
  );

  // CLI release watcher: one toast per published version, with a one-click
  // update. Dismissing remembers the version so it does not nag again.
  const claudeUpdate = useClaudeUpdate();
  const notifiedUpdate = useRef<string>('');
  useEffect(() => {
    const status = claudeUpdate.status;
    if (!status?.available || !status.latest) return;
    if (status.latest === claudeUpdate.dismissed || notifiedUpdate.current === status.latest) return;
    notifiedUpdate.current = status.latest;
    triggerIslandNotification({
      title: `Claude Code ${status.latest} available`,
      subtitle: `${status.installed} installed`,
      type: 'info',
      actionLabel: 'Update',
      onAction: () => {
        claudeUpdate.dismiss();
        void claudeUpdate.startUpdate();
      },
    });
  }, [claudeUpdate, triggerIslandNotification]);

  const spawner = useSpawner({
    onBackgroundComplete: (task) => {
      triggerIslandNotification({
        title: 'Agent completed task',
        subtitle: task.title || 'Task finished',
        type: 'success',
        threadId: task.threadId,
      });
    },
  });

  const coordinator = useCoordinator({
    // The Orchestrator backend is source of truth for launches: it executes
    // coordinator plans itself and announces them over orchestrator:spawned.
    // The reply handler only surfaces the plan card (status/override), never
    // auto-confirms, so a plan cannot launch twice.
    onReply: async (text) => {
      void spawner.inspect(text);
    },
  });

  // Adopt backend-launched agents into the deck and focus the overview.
  const adoptSpawned = spawner.adoptSpawned;
  useEffect(() => {
    const off = EventsOn('orchestrator:spawned', (result: session.SpawnResult) => {
      if (!result?.tasks || result.tasks.length === 0) return;
      adoptSpawned(result);
      setViewMode('grid');
    });
    return () => {
      off();
    };
  }, [adoptSpawned]);

  // View mode: 'deck' (focused 1-card carousel), 'grid' (Exposé multi-agent overview), or 'orchestrator' (Stratosphere lead workspace)
  const [viewMode, setViewMode] = useState<'deck' | 'grid' | 'orchestrator'>('deck');
  const [activeCardIndex, setActiveCardIndex] = useState(0);

  const handleNewDraft = useCallback(() => {
    const blank = spawner.tasks.findIndex((t) => t.isDraft && t.blocks.length === 0);
    if (blank >= 0) {
      setActiveCardIndex(blank);
    } else {
      spawner.openDraft();
      setActiveCardIndex(spawner.tasks.length);
    }
    setViewMode('deck');
    requestAnimationFrame(() => {
      const textarea = document.querySelector('textarea');
      if (textarea) textarea.focus();
    });
  }, [spawner]);

  // Dynamic viewport tracking for 1:1 hardware Exposé positioning
  const [windowSize, setWindowSize] = useState(() => ({
    width: typeof window !== 'undefined' ? window.innerWidth : 1440,
    height: typeof window !== 'undefined' ? window.innerHeight : 900,
  }));

  // Animation lock state: true during deck/grid or slide transitions (480ms)
  const [isAnimating, setIsAnimating] = useState(false);
  const animTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    setIsAnimating(true);
    if (animTimerRef.current) clearTimeout(animTimerRef.current);
    animTimerRef.current = setTimeout(() => {
      setIsAnimating(false);
    }, DECK_MS);
    return () => {
      if (animTimerRef.current) clearTimeout(animTimerRef.current);
    };
  }, [viewMode, activeCardIndex]);

  useEffect(() => {
    let frame = 0;
    const onResize = () => {
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        setWindowSize((prev) =>
          prev.width === window.innerWidth && prev.height === window.innerHeight
            ? prev
            : { width: window.innerWidth, height: window.innerHeight },
        );
      });
    };
    window.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('resize', onResize);
      if (frame) cancelAnimationFrame(frame);
    };
  }, []);

  // Card mode: 'chat', 'tasklist', 'browser', 'servers', or 'changes' (per agent card)
  const [cardModes, setCardModes] = useState<Record<string, 'chat' | 'tasklist' | 'browser' | 'servers' | 'changes'>>({});

  // User-customized deck card dimensions per threadId (persisted, stepped in 25px jumps via top grab handle)
  const [cardDimensionsMap, setCardDimensionsMap] = useState<Record<string, { width: number; height: number }>>(() => {
    try {
      const saved = localStorage.getItem('orchestrator_card_dimensions_map');
      if (saved) {
        const parsed = JSON.parse(saved);
        if (typeof parsed === 'object' && parsed !== null) {
          return parsed;
        }
      }
    } catch {}
    return {};
  });
  const [browserDimensionsMap, setBrowserDimensionsMap] = useState<Record<string, { width: number; height: number }>>({});
  const [isResizingCard, setIsResizingCard] = useState(false);
  const [fullscreenId, setFullscreenId] = useState<string | null>(null);
  const [browserImmersive, setBrowserImmersive] = useState(false);
  const immersiveActive = browserImmersive && Boolean(fullscreenId);

  useEffect(() => {
    try {
      localStorage.setItem('orchestrator_card_dimensions_map', JSON.stringify(cardDimensionsMap));
    } catch {}
  }, [cardDimensionsMap]);

  const [activePanel, setActivePanel] = useState<SidebarPanel | null>(null);
  // Keep usage fresh so Dynamic Island displays live quota meters
  const usage = useUsage(true);
  const git = useGit(true);
  // Always poll servers so cards know about running dev servers in real-time
  const runningServers = useServers(true);
  const wallpaper = useWallpaper();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsWindowMounted, setSettingsWindowMounted] = useState(false);
  const defaultModels = useDefaultModels(activePanel === 'settings' || settingsOpen);

  const historyState = useHistory(true);
  const claudeImport = useClaudeImport();
  const prefetchClaudeChats = claudeImport.list;
  useEffect(() => {
    const idle = (window as unknown as { requestIdleCallback?: (cb: () => void, opts?: { timeout: number }) => number }).requestIdleCallback;
    const timer = window.setTimeout(() => {
      if (idle) idle(() => void prefetchClaudeChats(), { timeout: 4000 });
      else void prefetchClaudeChats();
    }, 2500);
    return () => window.clearTimeout(timer);
  }, [prefetchClaudeChats]);

  const endSessionRef = useRef<(() => Promise<void>) | null>(null);
  endSessionRef.current = historyState.projectSwitched;
  const projects = useProjects(useCallback(() => endSessionRef.current?.(), []));
  const composerFiles = useAttachments();

  useEffect(() => {
    if (projects.active?.path) {
      useOrchestratorStore.getState().setAccountProject(projects.active.path, projects.active.accounts ?? {});
    }
  }, [projects.active]);

  const backgroundStyle = useMemo<React.CSSProperties>(
    () =>
      wallpaper.selected
        ? { ...SCENE_BG_BASE, backgroundImage: `url(${wallpaper.selected.url})` }
        : SCENE_BG_BASE,
    [wallpaper.selected],
  );

  // Map running dev servers back to an agent
  const getTaskServers = useCallback(
    (threadId?: string) => {
      if (!threadId) return [];
      const allGroups = runningServers.groups || [];
      const matching = allGroups.find((g) => g.threadId && g.threadId === threadId);
      if (matching && matching.servers?.length > 0) {
        const valid = matching.servers.filter((s) => !s.agent && s.port > 0);
        if (valid.length > 0) return valid;
      }
      // Unattributed servers are only offered when there is a single agent,
      // where they cannot belong to anyone else; otherwise every card would
      // show every server.
      if (spawner.tasks.length !== 1) return [];
      return allGroups
        .filter((g) => !g.threadId)
        .flatMap((g) => g.servers || [])
        .filter((s) => !s.agent && s.port > 0);
    },
    [runningServers.groups, spawner.tasks.length],
  );

  // Unified list of active tasks in the deck (live agents and opened history sessions)
  const activeTasks = spawner.tasks;

  // 3D Curved Launch Animation from Omnibar when spawning the first agent:
  // When transitioning from 0 to 1 active agent, the new card lifts off the Omnibar
  // along a smooth 3D curved arc into the center deck slot.
  const [prevActiveTasks, setPrevActiveTasks] = useState(activeTasks);
  const [launchingThreadId, setLaunchingThreadId] = useState<string | null>(null);
  const [isExitingEmptyState, setIsExitingEmptyState] = useState(false);

  if (prevActiveTasks !== activeTasks) {
    setPrevActiveTasks(activeTasks);
    if (prevActiveTasks.length === 0 && activeTasks.length > 0) {
      setLaunchingThreadId(activeTasks[0].threadId);
      setIsExitingEmptyState(true);
    } else if (activeTasks.length === 0) {
      setLaunchingThreadId(null);
      setIsExitingEmptyState(false);
    }
  }

  useEffect(() => {
    if (launchingThreadId) {
      const timer = setTimeout(() => {
        setLaunchingThreadId(null);
      }, 350);
      return () => clearTimeout(timer);
    }
  }, [launchingThreadId]);

  useEffect(() => {
    if (isExitingEmptyState) {
      const timer = setTimeout(() => {
        setIsExitingEmptyState(false);
      }, 350);
      return () => clearTimeout(timer);
    }
  }, [isExitingEmptyState]);

  // Pre-calculate Exposé 2D layout slots for persistent GPU card transformation
  const gridSlots = useMemo(() => {
    const stageW = Math.min(windowSize.width - 120, 1380);
    const stageH = Math.min(windowSize.height - 200, 800);
    return computeGridSlots(activeTasks.length, stageW, stageH);
  }, [activeTasks.length, windowSize.width, windowSize.height]);

  const selectPanel = useCallback(
    (panel: SidebarPanel) => {
      setActivePanel((current) => (current === panel ? null : panel));
    },
    [],
  );

  const closePanel = useCallback(() => setActivePanel(null), []);

  const handleOpenHistory = useCallback(
    async (workspaceId: string, threadId?: string) => {
      // 1. If this specific chat/task is already in the deck, focus it directly
      const rootId = threadId?.includes('-cont-') ? threadId.split('-cont-')[0] : threadId;
      if (rootId) {
        const matchingIndex = spawner.tasks.findIndex(
          (t) => t.threadId === rootId || t.threadId === threadId,
        );
        if (matchingIndex !== -1) {
          setActiveCardIndex(matchingIndex);
          setViewMode('deck');
          closePanel();
          return;
        }
      } else {
        const matchingIndex = spawner.tasks.findIndex((t) => t.workspaceId === workspaceId);
        if (matchingIndex !== -1) {
          setActiveCardIndex(matchingIndex);
          setViewMode('deck');
          closePanel();
          return;
        }
      }

      // 2. Otherwise open ONLY this 1 past chat directly into the active deck
      const targetIndex = await spawner.openHistorySession(workspaceId, threadId);
      if (targetIndex >= 0) {
        setActiveCardIndex(targetIndex);
      }
      setViewMode('deck');
      closePanel();
    },
    [spawner, closePanel],
  );

  // Clamp activeCardIndex within valid range
  useEffect(() => {
    if (activeTasks.length > 0 && activeCardIndex >= activeTasks.length) {
      setActiveCardIndex(Math.max(0, activeTasks.length - 1));
    }
  }, [activeTasks.length, activeCardIndex]);

  // Dynamic bottom input measurement & Proximity-based collision avoidance:
  // We measure the live height of the Omnibar. Instead of reacting prematurely on every
  // initial newline when there is still plenty of room, we calculate the actual clearance
  // between the card's natural bottom and the top of the Omnibar. Only when the expanding
  // input approaches the card within a comfortable breathing buffer (44px) does the card
  // smoothly elevate and flex its height.
  const inputWrapperRef = useRef<HTMLDivElement>(null);
  const [inputHeight, setInputHeight] = useState(52);

  useEffect(() => {
    const el = inputWrapperRef.current;
    if (!el) return;

    const measure = () => {
      setInputHeight(el.offsetHeight);
    };

    measure();

    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    document.documentElement.style.setProperty('--input-dock-h', `${inputHeight + 52}px`);
  }, [inputHeight]);

  // Compute natural bottom of the active center card
  const activeCenterTask = activeTasks[activeCardIndex];
  const activeCenterCardMode =
    (activeCenterTask && cardModes[activeCenterTask.threadId]) || 'chat';
  const activeCenterSize =
    (activeCenterTask && cardDimensionsMap[activeCenterTask.threadId]) || {
      width: 680,
      height: 690,
    };

  const activeCenterBrowserSize = activeCenterTask ? browserDimensionsMap[activeCenterTask.threadId] : undefined;

  const centerModeSpecs = {
    chat: { yBase: -28, baseMaxHeight: activeCenterSize.height, heightDeduction: 210 },
    tasklist: { yBase: -30, baseMaxHeight: 720, heightDeduction: 205 },
    browser: activeCenterBrowserSize
      ? { yBase: -32, baseMaxHeight: Math.min(activeCenterBrowserSize.height, windowSize.height - 190), heightDeduction: 0 }
      : { yBase: -32, baseMaxHeight: 730, heightDeduction: 200 },
    servers: { yBase: -30, baseMaxHeight: 720, heightDeduction: 205 },
    changes: { yBase: -34, baseMaxHeight: 770, heightDeduction: 180 },
  }[activeCenterCardMode] || { yBase: -28, baseMaxHeight: activeCenterSize.height, heightDeduction: 210 };

  const naturalCardHeight = Math.min(
    centerModeSpecs.baseMaxHeight,
    Math.max(400, windowSize.height - centerModeSpecs.heightDeduction),
  );
  const naturalCenterY = windowSize.height / 2 + centerModeSpecs.yBase;
  const naturalCardBottom = naturalCenterY + naturalCardHeight / 2;

  // The Omnibar container is positioned at bottom: 20px (bottom-5)
  const inputTop = windowSize.height - 20 - inputHeight;

  // Clearance between natural resting card bottom and top of the Omnibar.
  // We maintain a comfortable 44px breathing buffer. The card stays completely
  // at its natural resting size and position until the input gets closer than 44px.
  const MIN_CLEARANCE_BUFFER = 44;
  const clearance = inputTop - naturalCardBottom;
  const proximityDeficit = MIN_CLEARANCE_BUFFER - clearance;

  const pushUpOffset =
    proximityDeficit > 0 ? Math.min(130, Math.round(proximityDeficit)) : 0;

  const centerCardHeight = (() => {
    const base = centerModeSpecs.baseMaxHeight;
    const wanted =
      activeCenterCardMode === 'chat' || activeCenterBrowserSize
        ? base
        : windowSize.height - centerModeSpecs.heightDeduction;
    const capped = Math.min(wanted, Math.max(420, base - Math.round(pushUpOffset * 0.4)), windowSize.height - 190);
    return Math.max(440, capped);
  })();
  const centerCardTop = windowSize.height / 2 + centerModeSpecs.yBase - pushUpOffset - centerCardHeight / 2;
  const [islandHovered, setIslandHovered] = useState(false);
  const islandDocked =
    viewMode === 'deck' &&
    !fullscreenId &&
    activeTasks.length > 0 &&
    centerCardTop < 44 &&
    !islandHovered &&
    !islandNotification;

  // When new agents are spawned, automatically focus the new card
  const spawnedCount = spawner.tasks.length;
  const previousSpawnedCount = useRef(spawnedCount);
  useEffect(() => {
    if (spawnedCount > previousSpawnedCount.current) {
      setActiveCardIndex(spawnedCount - 1);
      setViewMode((current) => (current === 'grid' ? 'grid' : 'deck'));
    }
    previousSpawnedCount.current = spawnedCount;
  }, [spawnedCount]);

  // Keyboard navigation:
  // - Ctrl + ArrowRight: slide to next agent
  // - Ctrl + ArrowLeft: slide to previous agent
  // - Ctrl + ArrowUp: enter Grid overview mode (Mission Control style)
  // - Ctrl + ArrowDown: enter Deck focused mode
  // - Ctrl + G / Cmd + G: toggle Grid overview mode
  // - Ctrl + Space: toggle Grid overview mode
  // - Ctrl + N / Cmd + N: open a new blank chat tab
  // - Escape: exit Grid mode to Deck mode
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const isInputFocused =
        document.activeElement instanceof HTMLInputElement ||
        document.activeElement instanceof HTMLTextAreaElement;

      if ((e.ctrlKey || e.metaKey) && (e.key === 'n' || e.key === 'N')) {
        e.preventDefault();
        handleNewDraft();
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.key === 'ArrowRight') {
        e.preventDefault();
        setActiveCardIndex((prev) => Math.min(activeTasks.length - 1, prev + 1));
        setViewMode('deck');
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.key === 'ArrowLeft') {
        e.preventDefault();
        setActiveCardIndex((prev) => Math.max(0, prev - 1));
        setViewMode('deck');
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.key === 'ArrowUp') {
        e.preventDefault();
        setViewMode((prev) => (prev === 'deck' ? 'grid' : 'orchestrator'));
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.key === 'ArrowDown') {
        e.preventDefault();
        setViewMode((prev) => (prev === 'orchestrator' ? 'grid' : 'deck'));
        return;
      }

      if ((e.ctrlKey || e.metaKey) && (e.key === 'g' || e.key === 'G')) {
        e.preventDefault();
        setViewMode((prev) => (prev === 'deck' ? 'grid' : 'deck'));
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.code === 'Space') {
        e.preventDefault();
        setViewMode((prev) => (prev === 'deck' ? 'grid' : 'deck'));
        return;
      }

      if (e.key === 'Escape' && fullscreenId) {
        if (browserImmersive) {
          setBrowserImmersive(false);
        } else {
          setFullscreenId(null);
        }
        return;
      }

      if (e.key === 'Escape' && !isInputFocused) {
        if (activePanel) {
          setActivePanel(null);
          return;
        }
        if (viewMode === 'orchestrator') {
          setViewMode('grid');
        } else if (viewMode === 'grid') {
          setViewMode('deck');
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [activeTasks.length, viewMode, activePanel, fullscreenId, browserImmersive, handleNewDraft]);

  useEffect(() => {
    if (viewMode !== 'deck') setFullscreenId(null);
  }, [viewMode]);

  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';
  const isGlass = currentTheme.id === 'glass' || currentTheme.id === 'refractive-glass';

  const activeTask = activeTasks[activeCardIndex];
  const providers = useOrchestratorStore((state) => state.providers);
  const accountLabel = useCallback(
    (driver?: string, account?: string) => {
      const accounts = providers.find((provider) => provider.id === driver)?.accounts ?? [];
      if (accounts.length < 2) return undefined;
      return accounts.find((entry) => entry.id === (account || 'default'))?.name;
    },
    [providers],
  );
  const isCurrentBusy =
    viewMode === 'orchestrator' ? coordinator.isBusy : Boolean(activeTask?.isBusy);


  const chatsFullscreen = useOrchestratorStore((state) => state.chatsFullscreen);
  const [fullscreenSettled, setFullscreenSettled] = useState(false);
  const inFullscreen = Boolean(fullscreenId);
  useEffect(() => {
    if (!fullscreenId) setBrowserImmersive(false);
  }, [fullscreenId]);
  useEffect(() => {
    setFullscreenSettled(false);
    if (!inFullscreen) return;
    const timer = window.setTimeout(() => setFullscreenSettled(true), 480);
    return () => window.clearTimeout(timer);
  }, [inFullscreen]);

  useEffect(() => {
    if (!chatsFullscreen || viewMode !== 'deck') return;
    const current = activeTasks[activeCardIndex];
    if (current) setFullscreenId(current.threadId);
  }, [chatsFullscreen, viewMode, activeCardIndex, activeTasks.length]);

  useEffect(() => {
    if (!fullscreenId || viewMode !== 'deck') return;
    const current = activeTasks[activeCardIndex];
    if (current && current.threadId !== fullscreenId) setFullscreenId(current.threadId);
  }, [activeCardIndex, activeTasks, fullscreenId, viewMode]);
  // Synchronize bottom omnibar model picker with active card/chat
  useEffect(() => {
    if (viewMode === 'deck' && activeTask?.model) {
      useOrchestratorStore.getState().selectModelSilently(activeTask.model);
    }
  }, [activeCardIndex, activeTask?.model, viewMode]);

  useEffect(() => {
    const focused = viewMode === 'deck' && activeTask?.driver ? activeTask : undefined;
    useOrchestratorStore.getState().focusSession(
      focused
        ? { threadId: focused.threadId, providerId: focused.driver || '', account: focused.account || 'default' }
        : null,
    );
  }, [viewMode, activeTask?.threadId, activeTask?.driver, activeTask?.account]);

  // Queued messages per agent thread ID
  useEffect(() => {
    const keep = viewMode === 'deck' ? activeTasks[activeCardIndex]?.threadId : undefined;
    setCardModes((prev) => {
      let changed = false;
      const next = { ...prev };
      for (const id of Object.keys(prev)) {
        if (id !== keep && prev[id] !== 'chat') {
          next[id] = 'chat';
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [viewMode, activeCardIndex, activeTasks]);

  const [queuedMessagesByThread, setQueuedMessagesByThread] = useState<Record<string, QueuedMessage[]>>({});
  const currentQueuedMessages = activeTask ? queuedMessagesByThread[activeTask.threadId] || [] : [];

  // When an agent finishes its turn, automatically pop and dispatch the next queued message
  const prevBusyMap = useRef<Record<string, boolean>>({});
  // Threads the user just stopped: their queue is held rather than fired.
  const interruptedThreads = useRef<Set<string>>(new Set());
  useEffect(() => {
    for (const task of activeTasks) {
      const wasBusy = prevBusyMap.current[task.threadId];
      const isNowBusy = task.isBusy;
      if (wasBusy && !isNowBusy) {
        // Turn just completed!
        triggerIslandNotification({
          title: 'Task Completed',
          subtitle: task.title,
          type: 'success',
          threadId: task.threadId,
        });

        const queue = queuedMessagesByThread[task.threadId];
        const wasInterrupted = interruptedThreads.current.delete(task.threadId);
        if (queue && queue.length > 0 && !wasInterrupted) {
          const [nextMsg, ...remaining] = queue;
          setQueuedMessagesByThread((prev) => ({
            ...prev,
            [task.threadId]: remaining,
          }));

          // Sent exactly as typed: the queue delays a message, it does not rewrite it.
          void spawner.sendWithModel(task.threadId, nextMsg.text, nextMsg.files, nextMsg.modelId);
        }
      }
      prevBusyMap.current[task.threadId] = isNowBusy;
    }
  }, [activeTasks, queuedMessagesByThread, spawner]);

  const handleSubmit = useCallback(
    async (text: string, modelId: string) => {
      const clean = text.trim();
      const hasFiles = composerFiles.items.length > 0;
      if (!clean && !hasFiles) return;

      const files = composerFiles.toRefs();
      if (files.length > 0) composerFiles.release();

      if (viewMode === 'grid' || viewMode === 'orchestrator') {
        // When in Grid or Orchestrator mode:
        // Always send directly to coordinator — never intercept or auto-spawn
        setViewMode('orchestrator');
        void coordinator.send(clean, files);
        requestPinToBottom();
        return;
      }

      // Deck mode: a blank draft tab resolves into a real agent on first send.
      // Project/CLI come from the input bar at send time, not at tab creation.
      if (activeTask?.isDraft) {
        const promptText = clean.replace(/^@new\s*/i, '').trim();
        if (!promptText && files.length === 0) return;
        await spawner.spawnAgent(
          promptText,
          undefined,
          modelId,
          projects.active
            ? { name: projects.active.name, path: projects.active.path }
            : undefined,
          files,
          activeTask.threadId,
        );
        setViewMode('deck');
        requestPinToBottom();
        return;
      }

      // Deck mode with no tabs yet: first message starts the first agent.
      if (activeTasks.length === 0) {
        const promptText = clean.replace(/^@new\s*/i, '').trim();
        if (!promptText && files.length === 0) return;
        const threadId = await spawner.spawnAgent(
          promptText,
          undefined,
          modelId,
          projects.active
            ? { name: projects.active.name, path: projects.active.path }
            : undefined,
          files,
        );
        if (threadId) {
          setViewMode('deck');
        }
        requestPinToBottom();
        return;
      }

      // Explicit "@new ..." in a live chat still forks a fresh agent.
      if (clean.startsWith('@new ') || clean === '@new') {
        const promptText = clean.replace(/^@new\s*/i, '').trim();
        if (!promptText && files.length === 0) return;
        await spawner.spawnAgent(
          promptText,
          undefined,
          modelId,
          projects.active
            ? { name: projects.active.name, path: projects.active.path }
            : undefined,
          files,
        );
        setViewMode('deck');
        requestPinToBottom();
        return;
      }

      // Normal message to active agent in deck
      if (activeTask) {
        if (activeTask.isBusy) {
          // Agent is currently busy! Add to queue
          const newQueuedItem: QueuedMessage = {
            id: `queue-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
            threadId: activeTask.threadId,
            text: clean,
            modelId,
            files,
            createdAt: Date.now(),
          };

          setQueuedMessagesByThread((prev) => ({
            ...prev,
            [activeTask.threadId]: [...(prev[activeTask.threadId] || []), newQueuedItem],
          }));

          // IMMEDIATELY ADD TO THE AGENT'S TASKLIST PANEL!
          void MutateAgentTask(activeTask.threadId, 'add', '', clean);
        } else {
          void spawner.sendWithModel(activeTask.threadId, clean, files, modelId);
        }
        requestPinToBottom();
      }
    },
    [activeTasks, activeTask, composerFiles, spawner, coordinator, viewMode, projects.active],
  );

  const handleSkillTest = useCallback(
    async (text: string, modelId: string, skills: string[]) => {
      const clean = text.trim();
      const files = composerFiles.toRefs();
      if (!clean && files.length === 0) return;
      if (files.length > 0) composerFiles.release();
      const threadId = await spawner.spawnSkillTest(
        clean,
        files,
        skills,
        modelId,
        projects.active ? { name: projects.active.name, path: projects.active.path } : undefined,
      );
      if (threadId) setViewMode('grid');
      requestPinToBottom();
    },
    [composerFiles, spawner, projects.active],
  );

  const handleSendNow = useCallback(
    (id: string) => {
      if (!activeTask) return;
      const queue = queuedMessagesByThread[activeTask.threadId] || [];
      const item = queue.find((q) => q.id === id);
      if (!item) return;

      setQueuedMessagesByThread((prev) => ({
        ...prev,
        [activeTask.threadId]: (prev[activeTask.threadId] || []).filter((q) => q.id !== id),
      }));

      void spawner.sendWithModel(activeTask.threadId, item.text, item.files, item.modelId);
    },
    [activeTask, queuedMessagesByThread, spawner],
  );

  const handleEdit = useCallback(
    (id: string) => {
      if (!activeTask) return;
      const queue = queuedMessagesByThread[activeTask.threadId] || [];
      const item = queue.find((q) => q.id === id);
      if (!item) return;

      setQueuedMessagesByThread((prev) => ({
        ...prev,
        [activeTask.threadId]: (prev[activeTask.threadId] || []).filter((q) => q.id !== id),
      }));

      useOrchestratorStore.getState().setMessageText(item.text);
      const textarea = document.querySelector('textarea');
      if (textarea) {
        textarea.focus();
      }
    },
    [activeTask, queuedMessagesByThread],
  );

  const handleDelete = useCallback(
    (id: string) => {
      if (!activeTask) return;
      setQueuedMessagesByThread((prev) => ({
        ...prev,
        [activeTask.threadId]: (prev[activeTask.threadId] || []).filter((q) => q.id !== id),
      }));
    },
    [activeTask],
  );

  const handleInterrupt = useCallback(() => {
    if (viewMode === 'orchestrator') {
      void coordinator.interrupt();
    } else if (activeTask) {
      interruptedThreads.current.add(activeTask.threadId);
      void spawner.interrupt(activeTask.threadId);
    }
  }, [viewMode, coordinator, activeTask, spawner]);

  // Spatial morphing anchor calculations for Orchestrator card
  const activeSlot = gridSlots[activeCardIndex] || { x: 0, y: 0, scale: 0.7 };
  const activeGridY = activeSlot.y - Math.round(pushUpOffset * 0.4);
  const activeCenterMode = cardModes[activeTasks[activeCardIndex]?.threadId] || 'chat';
  const activeBaseW =
    activeCenterMode === 'changes'
      ? Math.min(1240, windowSize.width - 80)
      : activeCenterMode === 'browser'
        ? Math.min(1100, windowSize.width - 120)
        : activeCenterMode === 'tasklist'
          ? Math.min(780, windowSize.width - 120)
          : 680;
  const activeCardGridScale = Number((activeSlot.scale * (680 / activeBaseW)).toFixed(3));

  const orchestratorTransform =
    viewMode === 'orchestrator'
      ? `translate3d(0, ${-28 - pushUpOffset}px, 0) scale(1)`
      : viewMode === 'grid' && activeTasks.length > 0
        ? `translate3d(${activeSlot.x}px, ${activeGridY}px, 0) scale(${activeCardGridScale})`
        : `translate3d(0, ${-28 - pushUpOffset}px, 0) scale(0.95)`;

  return (
    <div className="relative w-screen h-screen overflow-hidden select-none bg-black flex flex-col items-center">
      {/* Level 0: Top Window TitleBar with Apple Dynamic Island */}
      <TitleBar>
        <div
          aria-hidden="true"
          onMouseEnter={() => setIslandHovered(true)}
          onMouseLeave={() => setIslandHovered(false)}
          className="absolute left-1/2 -translate-x-1/2 -top-3 w-[340px] h-3"
        />
        <div
          onMouseEnter={() => setIslandHovered(true)}
          onMouseLeave={() => setIslandHovered(false)}
          style={{
            transform: islandDocked ? 'translateY(-64px)' : 'translateY(0)',
            opacity: immersiveActive || islandDocked ? 0 : 1,
            pointerEvents: immersiveActive ? 'none' : undefined,
            transition: 'transform 380ms cubic-bezier(0.32, 0.72, 0, 1), opacity 200ms ease',
            paddingBottom: islandDocked ? 0 : 6,
          }}
        >
        <DynamicIsland
          projects={projects}
          activeTaskProjectName={activeTask?.projectName}
          activeTaskBranch={activeTask?.branch}
          viewMode={viewMode}
          usageReport={usage.report}
          usageError={usage.error}
          activeTasks={spawner.allActiveTasks}
          notification={islandNotification}
          onDismissNotification={() => {
            if (islandNotification?.actionLabel) claudeUpdate.dismiss();
            setIslandNotification(null);
          }}
          historyEntries={historyState.entries}
          activeWorkspaceId={spawner.workspaceId}
          onOpenHistory={handleOpenHistory}
          onDeleteHistory={historyState.remove}
          onSetChatArchived={(workspaceId, threadId, archived) => {
            void historyState.setArchived(workspaceId, threadId, archived);
          }}
          onMoveChat={(payload, project) => {
            void spawner.moveChat(payload, project).then(() => historyState.refresh());
          }}
          onRefreshHistory={historyState.refresh}
          onImportClaudeSession={(filePath) => {
            void (async () => {
              const workspaceId = await claudeImport.importOne(filePath);
              if (!workspaceId) return;
              await historyState.refresh();
              await handleOpenHistory(workspaceId);
            })();
          }}
          onRefreshClaude={() => {
            void claudeImport.list();
          }}
          claudeSessions={claudeImport.sessions}
          claudeLoading={claudeImport.isLoading}
          claudeLoadingMore={claudeImport.isLoadingMore}
          claudeExhausted={claudeImport.exhausted}
          onLoadMoreClaude={(providers) => {
            void claudeImport.loadMore(providers);
          }}
          claudeImporting={claudeImport.isImporting}
          claudeError={claudeImport.error}
          onNewChat={() => {
            void (async () => {
              await historyState.newChat();
              spawner.clear();
            })();
          }}
        />
        </div>
      </TitleBar>

      {/* Wallpaper Layer */}
      <div className="absolute inset-0 z-0 pointer-events-none" style={backgroundStyle} />

      {/* Agent Sessions Layer */}
      {activeTasks.length > 0 && (
        <div
          className={`absolute inset-0 z-20 flex items-center justify-center overflow-hidden ${
            viewMode === 'grid' || viewMode === 'orchestrator' ? 'pointer-events-auto cursor-default' : 'pointer-events-none'
          }`}
          onClick={(e) => {
            // Clicking backdrop in orchestrator mode returns to grid; in grid returns to deck
            if (viewMode === 'orchestrator' && e.target === e.currentTarget) {
              setViewMode('grid');
            } else if (viewMode === 'grid' && e.target === e.currentTarget) {
              setViewMode('deck');
            }
          }}
        >
          <div
            className="relative w-full h-full flex items-center justify-center"
            onClick={(e) => {
              if (viewMode === 'orchestrator' && e.target === e.currentTarget) {
                setViewMode('grid');
              } else if (viewMode === 'grid' && e.target === e.currentTarget) {
                setViewMode('deck');
              }
            }}
          >
            {activeTasks.map((task, index) => {
              const offset = index - activeCardIndex;
              const isCenter = offset === 0;
              const isPeekingLeft = offset === -1;
              const isPeekingRight = offset === 1;

              const currentCardMode = cardModes[task.threadId] || (task.cardKey ? cardModes[task.cardKey] : undefined) || 'chat';
              const activeCenterMode = cardModes[activeTasks[activeCardIndex]?.threadId] || 'chat';

              const activeCenterTask = activeTasks[activeCardIndex];
              const activeCenterSize = (activeCenterTask && cardDimensionsMap[activeCenterTask.threadId]) || { width: 680, height: 690 };
              const thisCardSize = cardDimensionsMap[task.threadId] || (task.cardKey ? cardDimensionsMap[task.cardKey] : undefined) || { width: 680, height: 690 };
              const sizedBrowser = currentCardMode === 'browser' ? browserDimensionsMap[task.threadId] : undefined;
              const centerSizedBrowser = activeCenterMode === 'browser' && activeCenterTask ? browserDimensionsMap[activeCenterTask.threadId] : undefined;

              let transform = 'translate3d(0, -28px, 0) scale(1)';
              let opacity = 1;
              let zIndex = 20;
              let isInteractive = true;

              const MODE_HALF_WIDTH: Record<string, number> = {
                changes: 620,
                browser: sizedBrowser ? Math.round(sizedBrowser.width / 2) : 550,
                tasklist: 390,
                chat: Math.round(thisCardSize.width / 2),
              };

              const centerHalf = activeCenterMode === 'chat'
                ? Math.round(activeCenterSize.width / 2)
                : centerSizedBrowser
                  ? Math.round(centerSizedBrowser.width / 2)
                  : (MODE_HALF_WIDTH[activeCenterMode] || 340);
              const cardHalf = currentCardMode === 'chat' ? Math.round(thisCardSize.width / 2) : (MODE_HALF_WIDTH[currentCardMode] || 340);
              // Precise offset so adjacent card peeks neatly with a consistent gap,
              // regardless of whether center or side card is 680px, 950px, 1100px, or 1240px:
              const sideScale = cardHalf > 460 ? 0.86 : 0.92;
              const peekX = Math.round(centerHalf + cardHalf * sideScale + 32);

              if (viewMode === 'deck') {
                const yBase =
                  currentCardMode === 'changes'
                    ? -34
                    : currentCardMode === 'browser'
                    ? -32
                    : currentCardMode === 'tasklist'
                    ? -30
                    : -28;

                if (isCenter) {
                  const yOffset = yBase - pushUpOffset;
                  transform = `translate3d(0, ${yOffset}px, 0) scale(1)`;
                  opacity = 1;
                  zIndex = 30;
                  isInteractive = true;
                } else if (isPeekingLeft) {
                  transform = `translate3d(-${peekX}px, ${yBase - pushUpOffset}px, 0) scale(${sideScale})`;
                  opacity = 0.6;
                  zIndex = 20;
                  isInteractive = true;
                } else if (isPeekingRight) {
                  transform = `translate3d(${peekX}px, ${yBase - pushUpOffset}px, 0) scale(${sideScale})`;
                  opacity = 0.6;
                  zIndex = 20;
                  isInteractive = true;
                } else {
                  const farX = Math.round(peekX * 1.45);
                  transform = `translate3d(${offset < 0 ? -farX : farX}px, ${yBase - pushUpOffset}px, 0) scale(0.8)`;
                  opacity = 0;
                  zIndex = 10;
                  isInteractive = false;
                }
              } else {
                // Exposé Grid Mode: all cards smoothly glide into their 2D grid slots
                const slot = gridSlots[index] || { x: 0, y: 0, scale: 0.7 };
                const gridY = slot.y - Math.round(pushUpOffset * 0.4);
                // Proportional scale so cards (e.g. 1100px browser) fit their Exposé slot perfectly
                // purely via GPU affine matrix without any DOM width/height reflow!
                const baseW =
                  currentCardMode === 'changes'
                    ? Math.min(1240, windowSize.width - 80)
                    : currentCardMode === 'browser'
                      ? (sizedBrowser ? sizedBrowser.width : Math.min(1100, windowSize.width - 120))
                      : currentCardMode === 'tasklist'
                        ? Math.min(780, windowSize.width - 120)
                        : thisCardSize.width;
                const cardGridScale = Number((slot.scale * (680 / baseW)).toFixed(3));

                if (viewMode === 'orchestrator') {
                  // Shape-morphing convergence: cards smoothly slide inward to center and merge into orchestrator card
                  const isMain = index === activeCardIndex;
                  transform = `translate3d(0px, ${-28 - pushUpOffset}px, 0) scale(${isMain ? 1 : 0.94})`;
                  opacity = 0;
                  zIndex = isMain ? 25 : 15;
                  isInteractive = false;
                } else {
                  transform = `translate3d(${slot.x}px, ${gridY}px, 0) scale(${cardGridScale})`;
                  opacity = 1;
                  zIndex = index === activeCardIndex ? 25 : 20;
                  isInteractive = true;
                }
              }

              const fullscreenDeck = viewMode === 'deck' && Boolean(fullscreenId);
              const isFullscreen = fullscreenDeck && Math.abs(offset) <= 1;
              if (isFullscreen) {
                transform = offset === 0 && fullscreenSettled ? 'none' : `translateX(calc(${offset} * 100vw))`;
                zIndex = isCenter ? 35 : 34;
                opacity = isCenter || fullscreenSettled ? 1 : 0;
                isInteractive = isCenter;
              } else if (fullscreenDeck) {
                opacity = 0;
                isInteractive = false;
              }

              // Card physical dimensions stay strictly consistent based on cardMode whether in Deck or Exposé Grid.
              // This guarantees moving between Deck and Grid or sliding cards is handled 100% via GPU compositor affine transforms
              // (translate3d + scale) with zero DOM layout reflows and zero canvas/iframe resizing!
              const cardWidth =
                currentCardMode === 'changes'
                  ? 'min(1240px, calc(100vw - 80px))'
                  : currentCardMode === 'browser'
                    ? (sizedBrowser ? `${sizedBrowser.width}px` : 'min(1100px, calc(100vw - 120px))')
                    : currentCardMode === 'tasklist'
                      ? 'min(780px, calc(100vw - 120px))'
                      : `${thisCardSize.width}px`;

              const cardHeight =
                currentCardMode === 'changes'
                  ? `calc(100vh - ${180 + Math.round(pushUpOffset * 0.45)}px)`
                  : currentCardMode === 'browser'
                    ? (sizedBrowser ? `${sizedBrowser.height}px` : `calc(100vh - ${200 + Math.round(pushUpOffset * 0.45)}px)`)
                    : currentCardMode === 'tasklist'
                      ? `calc(100vh - ${205 + Math.round(pushUpOffset * 0.45)}px)`
                      : `${thisCardSize.height}px`;

              const baseMaxHeight =
                currentCardMode === 'changes'
                  ? 770
                  : currentCardMode === 'browser'
                    ? (sizedBrowser ? sizedBrowser.height : 730)
                    : currentCardMode === 'tasklist'
                      ? 720
                      : thisCardSize.height;
              const cardMaxHeight = `${Math.max(420, baseMaxHeight - Math.round(pushUpOffset * 0.4))}px`;

              const isLaunching = isCenter && task.threadId === launchingThreadId;
              const cardYBase =
                currentCardMode === 'changes'
                  ? -34
                  : currentCardMode === 'browser'
                    ? -32
                    : currentCardMode === 'tasklist'
                      ? -30
                      : -28;
              const cardYOffset = cardYBase - pushUpOffset;

              return (
                <div
                  key={task.cardKey || task.threadId}
                  onClick={(e) => {
                    e.stopPropagation();
                    if (viewMode === 'grid') {
                      setActiveCardIndex(index);
                      setViewMode('deck');
                    } else if (!isCenter) {
                      setActiveCardIndex(index);
                    }
                  }}
                  className={`absolute ${fullscreenDeck && fullscreenSettled ? '' : 'will-change-transform'} ${
                    isInteractive ? 'pointer-events-auto' : 'pointer-events-none'
                  } ${
                    viewMode === 'grid'
                      ? 'cursor-pointer hover:opacity-90'
                      : !isCenter
                        ? 'cursor-pointer hover:opacity-80'
                        : ''
                  }`}
                  style={{
                    width: isFullscreen ? '100vw' : cardWidth,
                    maxWidth: isFullscreen ? '100vw' : sizedBrowser ? 'calc(100vw - 60px)' : 'calc(100vw - 120px)',
                    height: isFullscreen ? '100vh' : cardHeight,
                    maxHeight: isFullscreen
                      ? '100vh'
                      : `min(${cardMaxHeight}, calc(100vh - 190px))`,
                    minHeight: isFullscreen ? 0 : '440px',
                    transform,
                    opacity,
                    zIndex,
                    transformOrigin: 'center center',
                    transformStyle: 'flat',
                    contentVisibility: viewMode === 'deck' && Math.abs(offset) >= 3 ? 'hidden' : undefined,
                    transition: isResizingCard
                      ? 'transform 120ms cubic-bezier(0.16, 1, 0.3, 1), opacity 180ms ease, width 120ms cubic-bezier(0.16, 1, 0.3, 1), height 120ms cubic-bezier(0.16, 1, 0.3, 1), max-height 120ms cubic-bezier(0.16, 1, 0.3, 1)'
                      : `transform ${DECK_MS}ms ${DECK_SPRING}, opacity 300ms cubic-bezier(0.22, 1, 0.36, 1), width 420ms ${DECK_SPRING}, height 420ms ${DECK_SPRING}, max-height 420ms ${DECK_SPRING}`,
                  } as React.CSSProperties}
                >
                  <div
                    onAnimationEnd={() => {
                      if (isLaunching) {
                        setLaunchingThreadId(null);
                      }
                    }}
                    className={`w-full h-full ${isLaunching ? 'animate-genie-morph' : ''}`}
                    style={{
                      transformStyle: 'flat',
                      transformOrigin: '50% 100%',
                    }}
                  >
                    <Suspense fallback={null}>
                    <AgentWindow
                    id={task.threadId}
                    title={task.isDraft ? 'New chat' : task.title}
                    subtitle={task.branch}
                    status={task.isBusy ? 'working' : 'idle'}
                    workStartedAt={task.workStartedAt}
                    lastTurnMs={task.lastTurnMs}
                    contextUsage={task.context}
                    modelId={task.model}
                    driver={task.driver}
                    drivers={(task as any).drivers}
                    models={(task as any).models}
                    streamBlocks={task.blocks}
                    isDraft={task.isDraft}
                    isFocused={viewMode === 'deck' ? isCenter : false}
                    isAnimating={currentCardMode === 'browser' ? isAnimating : false}
                    mode={viewMode === 'deck' ? 'deck' : 'grid'}
                    cardMode={currentCardMode}
                    onCardModeChange={(m) => {
                      if (task.isDraft && m !== 'chat') return;
                      setCardModes((prev) => ({ ...prev, [task.threadId]: m }));
                    }}
                    detectedServers={getTaskServers(task.threadId)}
                    onStopServer={runningServers.stop}
                    git={git}
                    onFocus={() => {
                      setActiveCardIndex(index);
                      if (viewMode === 'grid' || viewMode === 'orchestrator') {
                        setViewMode('deck');
                      }
                    }}
                    onSendMessage={(text, model, files) => spawner.sendWithModel(task.threadId, text, files, model)}
                    onInterrupt={() => spawner.interrupt(task.threadId)}
                    onRename={(name) => spawner.renameChat(task.threadId, name)}
                    onAnswerQuestion={async (blockId, answers) => {
                      const qId = blockId.replace(/^question-/, '');
                      try {
                        await RespondToQuestion(task.threadId, qId, answers);
                      } catch (err) {
                        console.error('Failed to answer question:', err);
                      }
                    }}
                    onDeleteTurn={(turnId) => spawner.deleteTurn(task.threadId, turnId)}
                    onSkipQuestion={async (blockId) => {
                      const qId = blockId.replace(/^question-/, '');
                      try {
                        await RespondToQuestion(task.threadId, qId, []);
                      } catch (err) {
                        console.error('Failed to skip question:', err);
                      }
                    }}
                    onClose={() => spawner.close(task.threadId)}
                    onResize={(w, h) => {
                      const setMap = currentCardMode === 'browser' ? setBrowserDimensionsMap : setCardDimensionsMap;
                      setMap((prev) => ({
                        ...prev,
                        [task.threadId]: { width: w, height: h },
                      }));
                    }}
                    projectName={task.projectName}
                    accountName={accountLabel(task.driver, task.account)}
                    isFullscreen={isFullscreen}
                    onEnterFullscreen={() => setFullscreenId(task.threadId)}
                    onExitFullscreen={() => setFullscreenId(null)}
                    isBrowserImmersive={isFullscreen && browserImmersive}
                    onBrowserImmersiveChange={(immersive) => {
                      if (immersive) setFullscreenId(task.threadId);
                      setBrowserImmersive(immersive);
                    }}
                    onResizeStart={() => setIsResizingCard(true)}
                    onResizeEnd={() => setIsResizingCard(false)}
                    cardWidth={sizedBrowser ? sizedBrowser.width : thisCardSize.width}
                    cardHeight={sizedBrowser ? sizedBrowser.height : thisCardSize.height}
                    className="w-full h-full"
                    />
                    </Suspense>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {fullscreenId && viewMode === 'deck' && !immersiveActive && (
        <>
          {([-1, 1] as const).map((dir) => {
            const neighbor = activeTasks[activeCardIndex + dir];
            if (!neighbor) return null;
            return (
              <button
                key={dir}
                type="button"
                onClick={() => setActiveCardIndex(activeCardIndex + dir)}
                title={neighbor.title}
                className={`absolute bottom-6 z-40 pointer-events-auto flex items-center gap-2.5 max-w-[240px] px-3 py-2 rounded-2xl font-(family-name:--app-font) tracking-tight cursor-pointer transition-all duration-150 active:scale-95 ${
                  dir < 0 ? 'left-8' : 'right-12 flex-row-reverse text-right'
                } ${isLight ? 'text-black/40 hover:text-black/80 hover:bg-black/[0.06]' : 'text-white/35 hover:text-white/80 hover:bg-white/[0.08]'}`}
              >
                <span className="text-[20px] leading-none">{dir < 0 ? '←' : '→'}</span>
                <span className="flex flex-col min-w-0 gap-0.5">
                  <span className="truncate text-[13px] font-medium leading-tight">{neighbor.title || 'Agent'}</span>
                  <span className={`flex items-center gap-1.5 text-[10.5px] leading-tight ${dir > 0 ? 'flex-row-reverse' : ''}`}>
                    <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${neighbor.isBusy ? 'bg-emerald-400 animate-pulse shadow-[0_0_6px_rgba(52,211,153,0.8)]' : isLight ? 'bg-black/25' : 'bg-white/30'}`} />
                    <span className="opacity-70">{neighbor.isBusy ? 'Working' : 'Finished'}</span>
                    <span className="opacity-50">· {dir < 0 ? 'Ctrl + ←' : 'Ctrl + →'}</span>
                  </span>
                </span>
              </button>
            );
          })}
        </>
      )}

      {/* Level 3: Orchestrator Workspace Layer (Shape-Morphing) */}
      <div
        className={`absolute inset-0 z-30 flex items-center justify-center will-change-transform ${
          viewMode === 'orchestrator' ? 'pointer-events-auto' : 'pointer-events-none invisible'
        }`}
        style={{
          transform: orchestratorTransform,
          opacity: viewMode === 'orchestrator' ? 1 : 0,
          pointerEvents: viewMode === 'orchestrator' ? 'auto' : 'none',
          visibility: viewMode === 'orchestrator' ? 'visible' : 'hidden',
          transition:
            'transform 480ms cubic-bezier(0.25, 1, 0.5, 1), opacity 380ms cubic-bezier(0.25, 1, 0.5, 1), visibility 380ms',
        }}
        onClick={(e) => {
          if (e.target === e.currentTarget) {
            setViewMode(activeTasks.length > 0 ? 'grid' : 'deck');
          }
        }}
      >
        <div className={viewMode === 'orchestrator' ? 'pointer-events-auto' : 'pointer-events-none'}>
          <OrchestratorCard
            blocks={coordinator.blocks}
            isBusy={coordinator.isBusy}
            error={coordinator.error}
            canUseWorktree={spawner.canUseWorktree}
            isActive={viewMode === 'orchestrator'}
            launchedKeys={spawner.launchedKeys}
            onConfirmPlan={(tasks, useWorktree, modelIds) => {
              void spawner.confirmTasks(
                tasks,
                useWorktree,
                modelIds,
                projects.active ? { name: projects.active.name, path: projects.active.path } : undefined,
              );
              setViewMode('grid');
            }}
            onDismissPlan={spawner.dismiss}
          />
        </div>
      </div>

      {/* Bottom Omnibar & Deck Navigation Area */}
      <div
        className="absolute bottom-5 left-1/2 -translate-x-1/2 z-40 flex flex-col items-center gap-2.5 w-full max-w-[760px] px-4 pointer-events-none transition-opacity duration-200"
        style={immersiveActive ? { opacity: 0, visibility: 'hidden' } : undefined}
      >

        {/* Unified Omnibar Input Bar */}
        <div ref={inputWrapperRef} className="pointer-events-none w-full flex flex-col items-center gap-2">
          <div className="pointer-events-auto w-full flex justify-center">
          <OrchestratorInput
            queue={
              currentQueuedMessages.length > 0
                ? { items: currentQueuedMessages, onSendNow: handleSendNow, onEdit: handleEdit, onDelete: handleDelete }
                : undefined
            }
            onSubmit={handleSubmit}
            onSkillTest={handleSkillTest}
            onInterrupt={handleInterrupt}
            isBusy={isCurrentBusy}
            targetTitle={activeTask?.title}
            commandDriver={viewMode === 'deck' ? activeTask?.driver : undefined}
            hasActiveAgent={activeTasks.length > 0}
            viewMode={viewMode}
            onToggleViewMode={() => setViewMode((prev) => (prev === 'deck' ? 'grid' : 'deck'))}
            onNewAgent={handleNewDraft}
            isDraft={Boolean(activeTask?.isDraft)}
            projects={projects}
            attachments={composerFiles}
          />
          </div>
        </div>
      </div>

      {/* Sidebar Rail */}
      <div className="absolute left-5 top-1/2 -translate-y-1/2 z-40 pointer-events-auto">
        <Sidebar
          activePanel={activePanel}
          onSelect={selectPanel}
        />
      </div>

      {/* Click-outside backdrop to dismiss active floating panel */}
      {activePanel && (
        <div
          onClick={closePanel}
          className="fixed inset-0 z-35 pointer-events-auto"
        />
      )}

      {/* Sidebar Floating Panels with Fluid Dimension Morphing */}
      <AnimatePresence mode="wait">
        {activePanel === 'settings' && (
          <motion.div
            key="panel-settings"
            initial={{
              width: 52,
              height: 38,
              borderRadius: 19,
              y: 0,
              opacity: 0,
            }}
            animate={{
              width: 380,
              height: 470,
              borderRadius: 24,
              y: 0,
              opacity: 1,
            }}
            exit={{
              width: 52,
              height: 38,
              borderRadius: 19,
              y: 0,
              opacity: 0,
              transition: {
                width: { type: 'spring', stiffness: 460, damping: 34, mass: 0.8 },
                height: { type: 'spring', stiffness: 460, damping: 34, mass: 0.8 },
                borderRadius: { type: 'spring', stiffness: 460, damping: 34, mass: 0.8 },
                opacity: { duration: 0.14, ease: 'easeIn' },
              },
            }}
            transition={{
              width: { type: 'spring', stiffness: 440, damping: 30, mass: 0.8 },
              height: { type: 'spring', stiffness: 440, damping: 30, mass: 0.8 },
              borderRadius: { type: 'spring', stiffness: 440, damping: 30, mass: 0.8 },
              opacity: { duration: 0.16, ease: [0.16, 1, 0.3, 1] },
            }}
            style={{
              transformOrigin: 'left center',
              backgroundColor: isLight
                ? 'rgba(255, 255, 255, 0.94)'
                : isGlass
                ? 'rgba(10, 14, 26, 0.55)'
                : 'rgba(5, 5, 5, 0.94)',
              borderColor: isLight
                ? 'rgba(0, 0, 0, 0.10)'
                : isGlass
                ? 'rgba(255, 255, 255, 0.22)'
                : 'rgba(255, 255, 255, 0.10)',
              boxShadow: isLight
                ? '0 24px 50px rgba(0, 0, 0, 0.14), 0 4px 16px rgba(0, 0, 0, 0.08), inset 0 0.5px 0.5px rgba(255, 255, 255, 0.8)'
                : isGlass
                ? '0 24px 64px rgba(0, 0, 0, 0.50), 0 4px 20px rgba(0, 0, 0, 0.35), inset 0 1px 1px rgba(255, 255, 255, 0.35)'
                : '0 24px 54px rgba(0, 0, 0, 0.85), 0 4px 16px rgba(0, 0, 0, 0.6), inset 0 0.5px 0.5px rgba(255, 255, 255, 0.12)',
              backdropFilter: isGlass ? 'blur(16px) saturate(150%)' : undefined,
              WebkitBackdropFilter: isGlass ? 'blur(16px) saturate(150%)' : undefined,
            }}
            className="absolute left-[86px] top-1/2 -translate-y-1/2 z-40 pointer-events-auto overflow-hidden border select-none"
          >
            <motion.div
              initial={{
                opacity: 0,
                scale: 0.95,
                filter: 'blur(5px)',
              }}
              animate={{
                opacity: 1,
                scale: 1,
                filter: 'blur(0px)',
              }}
              exit={{
                opacity: 0,
                scale: 0.95,
                filter: 'blur(3px)',
                transition: { duration: 0.12, ease: 'easeIn' },
              }}
              transition={{
                duration: 0.24,
                delay: 0.08,
                ease: [0.16, 1, 0.3, 1],
              }}
              style={{ transformOrigin: 'left center' }}
              className="w-[380px] h-[470px] flex flex-col shrink-0"
            >
              <Suspense fallback={null}>
              <SettingsPanel
                wallpaper={wallpaper}
                defaultModels={defaultModels}
                onClose={closePanel}
                onOpenAll={() => {
                  closePanel();
                  setSettingsWindowMounted(true);
                  setSettingsOpen(true);
                }}
              />
              </Suspense>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>



      {settingsWindowMounted && (
        <Suspense fallback={null}>
      <SettingsWindow
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        wallpaper={wallpaper}
        defaultModels={defaultModels}
      />
        </Suspense>
      )}

      {/* Background Canvas Layer */}
      <div className="absolute inset-0 pointer-events-none z-10">
        {children}
      </div>
    </div>
  );
};

export default Scene;
