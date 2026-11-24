import React, { Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { motion, AnimatePresence, type Variants } from 'motion/react';
import { useWallpaper } from '../sidebar/useWallpaper';
import { useProjects } from '../orchestrator/useProjects';
import { useSpawner } from '../orchestrator/useSpawner';
import { useAttachments } from '../common/useAttachments';
import { useHistory } from '../history/useHistory';
import { useUsage } from '../sidebar/useUsage';
import { useServers } from '../sidebar/useServers';
import { useClaudeImport } from '../history/useClaudeImport';
import { useDefaultModels } from '../sidebar/useDefaultModels';
import { useAppUpdate } from '../sidebar/useAppUpdate';
import { useTheme } from '../../themes';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { AgentWindow, AgentBrowserView, AgentCardMode } from '../agent-session';
import { collectThreadChanges } from '../agent-session/threadChanges';
import { useGit } from '../git';
import { NavRail } from '../layout/NavRail';
import { HomeStats } from '../layout/HomeStats';
import { GlassInput } from '../layout/GlassInput';
import { parseSwitchNotice } from '../agent-session/NoticeDivider';
import { DynamicIsland, DynamicIslandNotification, extractChatHistoryItems, renderChatIcon } from '../common/DynamicIsland';
import { TitleBar } from '../common/TitleBar';
import { GlassSquircle } from '../../ui/glass/GlassSquircle';
import { useWebBookmarks, type WebBookmark } from '../agent-session/useWebBookmarks';
import { BookmarkModal } from '../agent-session/BookmarkModal';
import { GetUserPreference, GitWebURL, RespondToQuestion, SetUserPreference } from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import { playTaskComplete } from '../../sound';
import { domain } from '../../../wailsjs/go/models';

const SettingsWindow = React.lazy(() => import('../sidebar/SettingsWindow'));

function dataUrlToFile(dataUrl: string, filename: string): File {
  const normalized = dataUrl.startsWith('data:') ? dataUrl : `data:image/png;base64,${dataUrl}`;
  const commaIdx = normalized.indexOf(',');
  const header = commaIdx >= 0 ? normalized.slice(0, commaIdx) : 'data:image/png;base64';
  const base64 = commaIdx >= 0 ? normalized.slice(commaIdx + 1) : normalized;
  const mimeMatch = header.match(/data:([^;]+)/);
  const mime = mimeMatch ? mimeMatch[1] : 'image/png';
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return new File([bytes], filename, { type: mime });
}

const chatSlideVariants: Variants = {
  enter: (dir: number) => ({
    x: dir > 0 ? 80 : dir < 0 ? -80 : 0,
    opacity: 0,
    scale: 0.99,
    transition: {
      x: { type: 'spring' as const, stiffness: 380, damping: 30, mass: 0.8 },
      opacity: { duration: 0.2, ease: [0.16, 1, 0.3, 1] as const },
      scale: { duration: 0.2, ease: [0.16, 1, 0.3, 1] as const },
    },
  }),
  center: {
    x: 0,
    opacity: 1,
    scale: 1,
    transition: {
      x: { type: 'spring' as const, stiffness: 380, damping: 30, mass: 0.8 },
      opacity: { duration: 0.2, ease: [0.16, 1, 0.3, 1] as const },
      scale: { duration: 0.2, ease: [0.16, 1, 0.3, 1] as const },
    },
  },
  exit: (dir: number) => ({
    x: dir > 0 ? -80 : dir < 0 ? 80 : 0,
    opacity: 0,
    scale: 0.99,
    transition: {
      x: { duration: 0.14, ease: [0.32, 0.72, 0, 1] as const },
      opacity: { duration: 0.12, ease: 'easeOut' as const },
      scale: { duration: 0.14, ease: [0.32, 0.72, 0, 1] as const },
    },
  }),
};

export interface SceneProps {
  children?: React.ReactNode;
}

export const Scene: React.FC<SceneProps> = () => {
  const wallpaper = useWallpaper();
  const projects = useProjects();
  const spawner = useSpawner();
  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';

  // Panels & Triggers
  const [isSettingsOpen, setIsSettingsOpen] = useState(false);
  const [isIslandUsageOpen, setIsIslandUsageOpen] = useState(false);
  const [projectsTrigger, setProjectsTrigger] = useState(0);
  const [historyTrigger, setHistoryTrigger] = useState(0);

  const defaultModels = useDefaultModels(isSettingsOpen);
  const appUpdate = useAppUpdate();
  const history = useHistory(true);
  const usage = useUsage(isIslandUsageOpen || isSettingsOpen);
  const runningServers = useServers(true);
  const git = useGit(true);
  const claudeImport = useClaudeImport();
  const activeTasks = spawner.tasks;

  // View state: 'home' (blank chat screen) or 'chat' (active fullscreen chat)
  const [activeView, setActiveView] = useState<'home' | 'chat'>('home');
  const [activeCardIndex, setActiveCardIndex] = useState(0);
  const [direction, setDirection] = useState(0);
  const [cardMode, setCardMode] = useState<AgentCardMode>('chat');
  const [isBrowserSplit, setIsBrowserSplit] = useState(true);
  const [browserSplitRatio, setBrowserSplitRatio] = useState(0.38);
  const [browserTarget, setBrowserTarget] = useState<{ url: string; nonce: number } | null>(null);

  // Dynamic Island Notifications
  const [islandNotification, setIslandNotification] = useState<DynamicIslandNotification | null>(null);
  const notificationTimerRef = useRef<number | null>(null);

  // Active task object
  const activeTask = activeTasks[activeCardIndex] ?? null;
  const activeTaskThreadIdRef = useRef<string | undefined>(activeTask?.threadId);
  activeTaskThreadIdRef.current = activeTask?.threadId;

  // Auto-open split browser pane when an agent opens or interacts with the embedded browser
  useEffect(() => {
    const off = EventsOn('browser:agent_tab_opened', (payload: { threadId?: string }) => {
      const currentTid = activeTaskThreadIdRef.current;
      if (!payload?.threadId || !currentTid || payload.threadId !== currentTid) return;
      setIsBrowserSplit(true);
      setCardMode('browser');
      setActiveView('chat');
    });
    return () => {
      if (typeof off === 'function') off();
    };
  }, []);
  const activeAttachmentScope = activeView === 'chat' && activeTask ? activeTask.threadId : '__home__';
  const attachments = useAttachments(activeAttachmentScope);

  // Web Bookmarks (Universal + Project scoped)
  const activeProjectName = activeTask?.projectName || projects.active?.name;
  const {
    visibleBookmarks,
    addBookmark,
    updateBookmark,
    deleteBookmark,
  } = useWebBookmarks(activeProjectName);

  const [isBookmarkModalOpen, setIsBookmarkModalOpen] = useState(false);
  const [editingBookmark, setEditingBookmark] = useState<WebBookmark | null>(null);

  const handleBookmarkClick = useCallback((bm: WebBookmark) => {
    setBrowserTarget({ url: bm.url, nonce: Date.now() });
    setCardMode('browser');
    if (activeTasks.length > 0) {
      setActiveView('chat');
    }
  }, [activeTasks.length]);

  const handleBookmarkEdit = useCallback((bm: WebBookmark) => {
    setEditingBookmark(bm);
    setIsBookmarkModalOpen(true);
  }, []);

  const handleAddBookmark = useCallback(() => {
    setEditingBookmark({
      id: '',
      title: '',
      url: browserTarget?.url && browserTarget.url !== 'about:blank' ? browserTarget.url : 'https://',
      project: activeProjectName || null,
      createdAt: 0,
    });
    setIsBookmarkModalOpen(true);
  }, [browserTarget?.url, activeProjectName]);

  const handleSaveBookmark = useCallback((data: { title: string; url: string; project?: string | null }) => {
    if (editingBookmark?.id) {
      updateBookmark(editingBookmark.id, data);
    } else {
      addBookmark(data);
    }
  }, [editingBookmark?.id, updateBookmark, addBookmark]);

  const handleDeleteBookmark = useCallback(() => {
    if (editingBookmark?.id) {
      deleteBookmark(editingBookmark.id);
    }
  }, [editingBookmark?.id, deleteBookmark]);

  const handleGitClick = useCallback(async () => {
    const targetPath = activeTask?.projectPath || projects.active?.path;
    if (targetPath) {
      try {
        const webUrl = await GitWebURL(targetPath);
        if (webUrl) {
          setBrowserTarget({ url: webUrl, nonce: Date.now() });
          setCardMode('browser');
          setActiveView('chat');
          return;
        }
      } catch (err) {
        console.warn('Could not resolve GitWebURL:', err);
      }
    }
    setCardMode('changes');
    setActiveView('chat');
  }, [activeTask?.projectPath, projects.active?.path]);

  // Map running dev servers back to an agent
  const getTaskServers = useCallback(
    (threadId?: string) => {
      if (!threadId) return [];
      const allGroups = runningServers.groups || [];
      const matching = allGroups.find((g) => g.threadId && g.threadId === threadId);
      if (matching && matching.servers && matching.servers.length > 0) {
        const valid = matching.servers.filter((s) => !s.agent && s.port > 0);
        if (valid.length > 0) return valid;
      }
      if (activeTasks.length !== 1) return [];
      return allGroups
        .filter((g) => !g.threadId)
        .flatMap((g) => g.servers || [])
        .filter((s) => !s.agent && s.port > 0);
    },
    [runningServers.groups, activeTasks.length],
  );

  // Mode availability for NavRail
  const streamBlocks = activeTask?.blocks ?? [];
  const currentTaskServers = getTaskServers(activeTask?.threadId);
  const hasServers = currentTaskServers.length > 0;
  const threadChanges = useMemo(() => collectThreadChanges(streamBlocks), [streamBlocks]);
  const hasChanges = threadChanges.length > 0;

  const handleAttachImage = useCallback(
    async (dataUrl: string, filename = `screenshot-${Date.now()}.png`) => {
      if (!dataUrl) return;
      const file = dataUrlToFile(dataUrl, filename);
      await attachments.accept([file]);
      setCardMode((prev) => (prev === 'browser' && !isBrowserSplit ? 'chat' : prev));
      setActiveView('chat');
    },
    [attachments, isBrowserSplit],
  );

  const activeTaskModel = useMemo(() => {
    if (!activeTask) return undefined;
    if (activeTask.blocks) {
      for (let i = activeTask.blocks.length - 1; i >= 0; i--) {
        const b = activeTask.blocks[i];
        if (b.type === 'notice' && b.label) {
          const parsed = parseSwitchNotice(b.label);
          if (parsed?.toModel) return parsed.toModel;
        }
        if ((b as any).model) {
          return (b as any).model;
        }
      }
    }
    const models = activeTask.models;
    if (models && models.length > 0) return models[models.length - 1];
    return activeTask.model;
  }, [activeTask]);

  const activeTaskAccount = useMemo(() => {
    if (!activeTask) return undefined;
    return activeTask.account;
  }, [activeTask]);

  const activeTaskDriver = useMemo(() => {
    if (!activeTask) return undefined;
    if (activeTask.blocks) {
      for (let i = activeTask.blocks.length - 1; i >= 0; i--) {
        const b = activeTask.blocks[i];
        if (b.type === 'notice' && b.label) {
          const parsed = parseSwitchNotice(b.label);
          if (parsed?.toDriver) return parsed.toDriver;
        }
        if ((b as any).driver) {
          return (b as any).driver;
        }
      }
    }
    const drivers = activeTask.drivers;
    if (drivers && drivers.length > 0) return drivers[drivers.length - 1];
    return activeTask.driver;
  }, [activeTask]);

  // Initial provider discovery and live listener
  useEffect(() => {
    void useOrchestratorStore.getState().loadProviders();
    const off = EventsOn('providers:changed', () => {
      void useOrchestratorStore.getState().loadProviders(true);
    });
    return () => {
      off();
    };
  }, []);

  // Play completion sound and surface notification in Dynamic Island
  useEffect(() => {
    const off = EventsOn(
      'session:notification',
      (notif: { type: string; title: string; message: string; threadId?: string }) => {
        if (notificationTimerRef.current) {
          window.clearTimeout(notificationTimerRef.current);
        }
        if (notif.type === 'success' && useOrchestratorStore.getState().interfaceSounds) {
          playTaskComplete();
        }
        setIslandNotification({
          id: `${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
          timestamp: Date.now(),
          title: notif.title || 'Completed',
          subtitle: notif.message,
          type: notif.type === 'alert' ? 'alert' : 'success',
          threadId: notif.threadId,
        });
        notificationTimerRef.current = window.setTimeout(() => {
          setIslandNotification(null);
        }, 5000);
      },
    );
    return () => {
      off();
    };
  }, []);

  // Startup restore of open chat tabs (when "Reopen open chats on startup" is enabled)
  const restoredOnBootRef = useRef(false);
  const isRestoringBootRef = useRef(false);
  const [bootRestoreDone, setBootRestoreDone] = useState(false);
  const bootRestoreDoneRef = useRef(false);
  bootRestoreDoneRef.current = bootRestoreDone;

  useEffect(() => {
    if (restoredOnBootRef.current || !history.hasLoaded) return;
    restoredOnBootRef.current = true;
    void (async () => {
      isRestoringBootRef.current = true;
      try {
        const enabled = await GetUserPreference('restore_open_chats');
        if (enabled !== 'true') return;
        const raw = await GetUserPreference('open_chats_state');
        if (!raw) return;
        const parsed = JSON.parse(raw) as {
          version?: number;
          activeView?: 'home' | 'chat';
          activeThreadId?: string;
          tabs?: Array<{ workspaceId: string; threadId: string }>;
        };
        if (!parsed || parsed.version !== 1 || !Array.isArray(parsed.tabs) || parsed.tabs.length === 0) {
          return;
        }
        const validItems = extractChatHistoryItems(history.entries, undefined, { archived: 'exclude' });
        const validSet = new Set(validItems.map((item) => `${item.workspaceId}::${item.threadId}`));
        const validTabs = parsed.tabs.filter(
          (tab) => tab?.workspaceId && tab?.threadId && validSet.has(`${tab.workspaceId}::${tab.threadId}`),
        );
        if (validTabs.length === 0) return;

        let targetIndex = -1;
        for (const tab of validTabs) {
          const idx = await spawner.openHistorySession(tab.workspaceId, tab.threadId);
          if (idx >= 0 && tab.threadId === parsed.activeThreadId) {
            targetIndex = idx;
          }
        }
        setActiveCardIndex(targetIndex >= 0 ? targetIndex : 0);
        setActiveView(parsed.activeView === 'chat' ? 'chat' : 'home');
      } catch {
        // Ignore malformed saved state
      } finally {
        isRestoringBootRef.current = false;
        setBootRestoreDone(true);
      }
    })();
  }, [history.hasLoaded, history.entries, spawner.openHistorySession]);

  const openChatsStateJson = useMemo(
    () =>
      JSON.stringify({
        version: 1,
        activeView,
        activeThreadId: activeTask?.threadId || '',
        tabs: activeTasks
          .filter((t) => !t.isDraft && t.workspaceId)
          .map((t) => ({ workspaceId: t.workspaceId, threadId: t.threadId })),
      }),
    [activeView, activeTask?.threadId, activeTasks],
  );
  const latestOpenChatsJsonRef = useRef(openChatsStateJson);
  latestOpenChatsJsonRef.current = openChatsStateJson;

  useEffect(() => {
    if (!bootRestoreDone) return;
    const timer = window.setTimeout(() => {
      void SetUserPreference('open_chats_state', latestOpenChatsJsonRef.current);
    }, 400);
    return () => window.clearTimeout(timer);
  }, [bootRestoreDone, openChatsStateJson]);

  useEffect(() => {
    const flush = () => {
      if (!bootRestoreDoneRef.current) return;
      void SetUserPreference('open_chats_state', latestOpenChatsJsonRef.current);
    };
    window.addEventListener('beforeunload', flush);
    return () => window.removeEventListener('beforeunload', flush);
  }, []);

  // When a new agent is spawned, automatically navigate to it in full-bleed chat
  const spawnedCount = activeTasks.length;
  const prevSpawnedCount = useRef(spawnedCount);
  useEffect(() => {
    if (!isRestoringBootRef.current && spawnedCount > prevSpawnedCount.current) {
      setActiveCardIndex(spawnedCount - 1);
      setActiveView('chat');
    }
    prevSpawnedCount.current = spawnedCount;
  }, [spawnedCount]);

  // Keyboard navigation:
  // - Ctrl + ArrowLeft: previous agent session (or return to home)
  // - Ctrl + ArrowRight: next agent session (or enter active chat from home)
  // - Ctrl + N: new blank chat / home
  // - Escape: close settings
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && (e.key === 'n' || e.key === 'N')) {
        e.preventDefault();
        setDirection(-1);
        setActiveView('home');
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'ArrowRight') {
        e.preventDefault();
        if (activeView === 'chat' && activeTask && activeCardIndex < activeTasks.length - 1) {
          spawner.moveTask(activeTask.threadId, activeCardIndex + 1);
          setActiveCardIndex((prev) => Math.min(activeTasks.length - 1, prev + 1));
        }
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'ArrowLeft') {
        e.preventDefault();
        if (activeView === 'chat' && activeTask && activeCardIndex > 0) {
          spawner.moveTask(activeTask.threadId, activeCardIndex - 1);
          setActiveCardIndex((prev) => Math.max(0, prev - 1));
        }
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.key === 'ArrowRight') {
        e.preventDefault();
        if (activeView === 'home' && activeTasks.length > 0) {
          setDirection(1);
          setActiveView('chat');
        } else if (activeView === 'chat' && activeCardIndex < activeTasks.length - 1) {
          setDirection(1);
          setActiveCardIndex((prev) => prev + 1);
        }
        return;
      }

      if ((e.ctrlKey || e.metaKey) && e.key === 'ArrowLeft') {
        e.preventDefault();
        if (activeView === 'chat' && activeCardIndex > 0) {
          setDirection(-1);
          setActiveCardIndex((prev) => prev - 1);
        }
        return;
      }

      if (e.key === 'Escape') {
        if (isSettingsOpen) {
          setIsSettingsOpen(false);
          return;
        }
        if (activeView === 'home' && activeTasks.length > 0) {
          setDirection(1);
          setActiveView('chat');
          return;
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [activeTasks.length, activeCardIndex, activeView, activeTask, spawner.moveTask]);

  // Account label helper
  const providers = useOrchestratorStore((state) => state.providers);
  const accountLabel = useCallback(
    (driver?: string, account?: string) => {
      const accounts = providers.find((p) => p.id === driver)?.accounts ?? [];
      if (accounts.length < 2) return undefined;
      return accounts.find((entry) => entry.id === (account || 'default'))?.name;
    },
    [providers],
  );

  // Submit prompt from GlassInput (Home view or Chat view)
  const handleSubmitPrompt = async (promptText: string, modelId?: string) => {
    const trimmed = promptText.trim();
    const refs = attachments.toRefs();
    if (!trimmed && refs.length === 0) return;

    if (activeView === 'chat' && activeTask) {
      attachments.release();
      await spawner.sendWithModel(activeTask.threadId, trimmed, refs, modelId);
      return;
    }

    const prevCount = activeTasks.length;
    const threadId = await spawner.spawnAgent(
      trimmed,
      undefined,
      modelId,
      projects.active ? { name: projects.active.name, path: projects.active.path } : undefined,
      refs,
    );
    attachments.release();
    if (threadId) {
      setDirection(1);
      setActiveCardIndex(prevCount);
      setActiveView('chat');
    }
  };

  const handleCloseTask = (threadId: string) => {
    spawner.close(threadId);
    if (activeTasks.length <= 1) {
      setDirection(-1);
      setActiveView('home');
      setActiveCardIndex(0);
    } else if (activeCardIndex >= activeTasks.length - 1) {
      setDirection(-1);
      setActiveCardIndex(Math.max(0, activeCardIndex - 1));
    }
  };

  const handleOpenHistorySession = async (workspaceId: string, threadId?: string) => {
    const targetIndex = await spawner.openHistorySession(workspaceId, threadId);
    if (targetIndex >= 0) {
      setDirection(targetIndex >= activeCardIndex ? 1 : -1);
      setActiveCardIndex(targetIndex);
      setActiveView('chat');
    }
  };

  const isTaskWorking = useCallback((task?: (typeof activeTasks)[0] | null) => {
    if (!task) return false;
    if (task.isBusy !== undefined) return task.isBusy;
    return Boolean(
      task.blocks?.some(
        (b) =>
          (b.type === 'thinking' && b.isThinking) ||
          (b.type === 'tool_bash' && b.status === 'running') ||
          (b.type === 'tool_search' && b.isSearching) ||
          (b.type === 'tool_edit' && (b as any).status === 'running'),
      ),
    );
  }, []);

  const activeTaskWorking = isTaskWorking(activeTask);

  const getTaskDriverAndModel = useCallback((task?: (typeof activeTasks)[0] | null) => {
    if (!task) return { driver: undefined, model: undefined };
    let driver = task.driver;
    let model = task.model;
    if (task.drivers && task.drivers.length > 0) driver = task.drivers[task.drivers.length - 1];
    if (task.models && task.models.length > 0) model = task.models[task.models.length - 1];
    if (task.blocks) {
      for (let i = task.blocks.length - 1; i >= 0; i--) {
        const b = task.blocks[i];
        if (b.type === 'notice' && b.label) {
          const parsed = parseSwitchNotice(b.label);
          if (parsed?.toDriver) driver = parsed.toDriver;
          if (parsed?.toModel) model = parsed.toModel;
          if (parsed?.toDriver || parsed?.toModel) break;
        }
      }
    }
    return { driver, model };
  }, []);

  const leftTarget = useMemo(() => {
    if (activeView === 'chat' && activeCardIndex > 0) {
      const neighbor = activeTasks[activeCardIndex - 1];
      const { driver, model } = getTaskDriverAndModel(neighbor);
      return {
        title: neighbor?.title || 'Agent',
        driver,
        model,
        isBusy: isTaskWorking(neighbor),
        subtitle: 'Ctrl + ←',
        onClick: () => {
          setDirection(-1);
          setActiveCardIndex((prev) => Math.max(0, prev - 1));
        },
      };
    }
    return null;
  }, [activeView, activeCardIndex, activeTasks, isTaskWorking, getTaskDriverAndModel]);

  const rightTarget = useMemo(() => {
    if (activeView === 'home' && activeTasks.length > 0) {
      const neighbor = activeTasks[activeCardIndex] || activeTasks[0];
      const { driver, model } = getTaskDriverAndModel(neighbor);
      return {
        title: neighbor?.title || 'Back to Chat',
        driver,
        model,
        isBusy: isTaskWorking(neighbor),
        subtitle: 'Ctrl + →',
        onClick: () => {
          setDirection(1);
          setActiveView('chat');
        },
      };
    } else if (activeView === 'chat' && activeCardIndex < activeTasks.length - 1) {
      const neighbor = activeTasks[activeCardIndex + 1];
      const { driver, model } = getTaskDriverAndModel(neighbor);
      return {
        title: neighbor?.title || 'Agent',
        driver,
        model,
        isBusy: isTaskWorking(neighbor),
        subtitle: 'Ctrl + →',
        onClick: () => {
          setDirection(1);
          setActiveCardIndex((prev) => Math.min(activeTasks.length - 1, prev + 1));
        },
      };
    }
    return null;
  }, [activeView, activeCardIndex, activeTasks, isTaskWorking, getTaskDriverAndModel]);

  return (
    <div
      className={`relative w-screen h-screen overflow-hidden flex select-none ${
        isLight ? 'bg-[#e6e6eb] text-black' : 'bg-[#141414] text-white'
      }`}
      style={{
        backgroundColor: isLight ? '#e6e6eb' : '#141414',
      }}
    >
      {/* Left Navigation Rail (width 36px, #141414) */}
      <NavRail
        activeView={activeView}
        onHomeClick={() => {
          setDirection(-1);
          setCardMode('chat');
          setActiveView('home');
        }}
        onProjectsClick={() => setProjectsTrigger((p) => p + 1)}
        onSettingsClick={() => setIsSettingsOpen((prev) => !prev)}
        isSettingsOpen={isSettingsOpen}
        hasAppUpdate={Boolean(appUpdate.status?.available)}
        appUpdateVersion={appUpdate.status?.latestVersion}
        appUpdate={appUpdate}
        isLight={isLight}
        cardMode={cardMode}
        onModeSelect={(mode) => {
          setCardMode(mode);
          if (mode === 'chat') {
            if (activeTasks.length > 0) {
              setActiveView('chat');
            } else {
              setActiveView('home');
            }
          } else if (activeTasks.length > 0) {
            setActiveView('chat');
          }
        }}
        onGitClick={handleGitClick}
        hasPreview={true}
        hasServers={hasServers}
        hasChanges={hasChanges}
        bookmarks={visibleBookmarks}
        currentBrowserUrl={browserTarget?.url}
        onBookmarkClick={handleBookmarkClick}
        onBookmarkEdit={handleBookmarkEdit}
        onAddBookmark={handleAddBookmark}
      />

      {/* Main Workspace Area with reverse radius corner against #141414 sidebar */}
      <div
        className={`relative flex-1 h-full overflow-hidden flex flex-col z-10 rounded-tl-[12px] rounded-bl-[12px] ${
          isLight ? 'bg-[#f4f4f7]' : 'bg-[#070709]'
        }`}
        style={{
          backgroundColor: isLight ? '#f4f4f7' : '#070709',
        }}
      >
        {/* Subtle Wallpaper Layer with 180deg linear alpha mask: 0% at top to 8% at bottom over #070709 */}
        {wallpaper.selected && (
          <div
            className="absolute inset-0 pointer-events-none z-0 overflow-hidden transition-opacity duration-700"
            style={{
              backgroundImage: `url(${wallpaper.selected.url})`,
              backgroundSize: 'cover',
              backgroundPosition: 'center',
              maskImage:
                'linear-gradient(180deg, rgba(0, 0, 0, 0) 0%, rgba(0, 0, 0, 0.08) 100%)',
              WebkitMaskImage:
                'linear-gradient(180deg, rgba(0, 0, 0, 0) 0%, rgba(0, 0, 0, 0.08) 100%)',
            }}
          />
        )}
        {/* Top Window TitleBar with Drag Handle & Traffic Lights (Minimise / Maximise / Close) */}
        <TitleBar>
          <DynamicIsland
            cardMode={activeView === 'chat' || cardMode === 'browser' ? cardMode : 'chat'}
            projects={projects}
            activeTaskProjectName={activeTask?.projectName}
            activeTaskBranch={activeTask?.branch}
            usageReport={usage.report}
            usageError={usage.error}
            activeTasks={spawner.allActiveTasks}
            openTasks={activeTasks}
            activeThreadId={activeView === 'chat' ? activeTask?.threadId : undefined}
            onSelectOpenTask={(threadId) => {
              const idx = activeTasks.findIndex((t) => t.threadId === threadId);
              if (idx >= 0) {
                setDirection(idx >= activeCardIndex ? 1 : -1);
                setActiveCardIndex(idx);
                setActiveView('chat');
                setCardMode('chat');
              }
            }}
            onReorderOpenTasks={(orderedIds) => {
              const curId = activeTask?.threadId;
              spawner.reorderTasks(orderedIds);
              if (curId) {
                const nextIdx = orderedIds.indexOf(curId);
                if (nextIdx >= 0) setActiveCardIndex(nextIdx);
              }
            }}
            onCloseOpenTask={handleCloseTask}
            notification={islandNotification}
            onDismissNotification={() => {
              setIslandNotification(null);
            }}
            historyEntries={history.entries}
            activeWorkspaceId={spawner.workspaceId}
            onOpenHistory={handleOpenHistorySession}
            onDeleteHistory={history.remove}
            onSetChatArchived={(wsId, tId, arch) => {
              void history.setArchived(wsId, tId, arch);
            }}
            onMoveChat={(payload, proj) => {
              void spawner.moveChat(payload, proj).then(() => history.refresh());
            }}
            onRenameChat={(threadId, title) =>
              spawner.renameChat(threadId, title).then(() => history.refresh())
            }
            onRefreshHistory={history.refresh}
            onUsageOpenChange={setIsIslandUsageOpen}
            openProjectsTrigger={projectsTrigger}
            openHistoryTrigger={historyTrigger}
            claudeSessions={claudeImport.sessions}
            claudeLoading={claudeImport.isLoading}
            claudeLoadingMore={claudeImport.isLoadingMore}
            claudeExhausted={claudeImport.exhausted}
            claudeImporting={claudeImport.importingPath}
            claudeError={claudeImport.error}
            onRefreshClaude={claudeImport.list}
            onLoadMoreClaude={claudeImport.loadMore}
            onImportClaudeSession={async (filePath) => {
              const imported = await claudeImport.importOne(filePath);
              if (!imported?.workspaceId) return;
              const targetIdx = await spawner.openHistorySession(imported.workspaceId, imported.threadId);
              if (targetIdx >= 0) {
                setDirection(targetIdx >= activeCardIndex ? 1 : -1);
                setActiveCardIndex(targetIdx);
                setActiveView('chat');
              }
              void history.refresh();
            }}
          />
        </TitleBar>

        {/* View Content */}
        <div className="relative flex-1 w-full h-full overflow-hidden">
          <AnimatePresence mode="wait" custom={direction} initial={false}>
            {cardMode === 'browser' && (!activeTask || activeView === 'home') ? (
              /* Standalone Browser View (works with 0 chats or from home) */
              <motion.div
                key="standalone-browser"
                custom={direction}
                variants={chatSlideVariants}
                initial="enter"
                animate="center"
                exit="exit"
                className="relative w-full h-full overflow-hidden"
              >
                <Suspense fallback={null}>
                  <AgentBrowserView
                    threadId="global"
                    detectedServers={runningServers.groups?.flatMap((g) => g.servers || []).filter((s) => !s.agent && s.port > 0) || []}
                    isFocused={true}
                    onFocusCard={() => {}}
                    onCloseBrowser={() => {
                      setCardMode('chat');
                      setActiveView('home');
                    }}
                    onAttachImage={handleAttachImage}
                    targetUrl={browserTarget?.url}
                    targetUrlNonce={browserTarget?.nonce}
                    projectName={activeProjectName}
                    projectPath={projects.active?.path}
                    className="w-full h-full"
                  />
                </Suspense>
              </motion.div>
            ) : activeView === 'home' || !activeTask ? (
              /* Blank Chat / Home View */
              <motion.div
                key="home-view"
                custom={direction}
                variants={chatSlideVariants}
                initial="enter"
                animate="center"
                exit="exit"
                className="relative w-full h-full flex flex-col items-center justify-center pb-28 px-6 overflow-y-auto"
              >
                {/* Centered Stats & Activity Heatmap */}
                <HomeStats isLight={isLight} />
              </motion.div>
            ) : (
              /* Fullscreen Active Chat View */
              <motion.div
                key={activeTask.threadId}
                custom={direction}
                variants={chatSlideVariants}
                initial="enter"
                animate="center"
                exit="exit"
                className="relative w-full h-full overflow-hidden"
              >
                <Suspense fallback={null}>
                  <AgentWindow
                    key={activeTask.threadId}
                    id={activeTask.threadId}
                    title={activeTask.title}
                    subtitle={activeTask.branch}
                    modelId={activeTask.model}
                    driver={activeTask.driver}
                    status={activeTaskWorking ? 'working' : 'idle'}
                    workStartedAt={activeTask.workStartedAt}
                    lastTurnMs={activeTask.lastTurnMs}
                    contextUsage={activeTask.context}
                    cardMode={cardMode}
                    onCardModeChange={setCardMode}
                    isBrowserSplit={isBrowserSplit}
                    onBrowserSplitChange={setIsBrowserSplit}
                    browserSplitRatio={browserSplitRatio}
                    onBrowserSplitRatioChange={setBrowserSplitRatio}
                    detectedServers={currentTaskServers}
                    onStopServer={runningServers.stop}
                    git={git}
                    streamBlocks={activeTask.blocks}
                    hasEarlier={Boolean(activeTask.hasEarlier)}
                    loadingEarlier={Boolean(activeTask.loadingEarlier)}
                    onLoadEarlier={() => spawner.loadEarlier(activeTask.threadId)}
                    onSendMessage={(msg: string, mId: string, files?: domain.FileRef[]) =>
                      spawner.sendWithModel(activeTask.threadId, msg, files, mId)
                    }
                    onInterrupt={() => spawner.interrupt(activeTask.threadId)}
                    onRename={(title) => spawner.renameChat(activeTask.threadId, title)}
                    onAttachImage={handleAttachImage}
                    onAnswerQuestion={async (blockId, answers) => {
                      const qId = blockId.replace(/^question-/, '');
                      await RespondToQuestion(activeTask.threadId, qId, answers);
                    }}
                    onSkipQuestion={async (blockId) => {
                      const qId = blockId.replace(/^question-/, '');
                      await RespondToQuestion(activeTask.threadId, qId, []);
                    }}
                    onDeleteTurn={(turnId) => spawner.deleteTurn(activeTask.threadId, turnId)}
                    onClose={() => handleCloseTask(activeTask.threadId)}
                    projectName={activeTask.projectName}
                    projectPath={activeTask.projectPath}
                    isPreviousChat={Boolean(activeTask.isHistory || activeTask.isLive === false)}
                    accountName={accountLabel(activeTask.driver, activeTask.account)}
                    browserTargetUrl={browserTarget?.url}
                    browserTargetNonce={browserTarget?.nonce}
                    isFullscreen={true}
                    isFocused={true}
                    className="w-full h-full"
                  />
                </Suspense>
              </motion.div>
            )}
          </AnimatePresence>
        </div>

        {/* Bottom Left Navigation Indicator */}
        {leftTarget && cardMode !== 'browser' && cardMode !== 'terminal' && (
          <button
            type="button"
            onClick={leftTarget.onClick}
            title={leftTarget.title}
            aria-label={leftTarget.title}
            className="fixed bottom-7 left-[64px] z-30 pointer-events-auto cursor-pointer transition-transform duration-150 active:scale-95 group hidden md:flex"
          >
            <GlassSquircle
              radius={18}
              fill={isLight ? 'rgba(255, 255, 255, 0.72)' : 'linear-gradient(135deg, #0B0A09 0%, #191616 100%)'}
              frost={16}
              border={{
                strokeWidth: 1,
                align: 'inside',
                gradient: isLight
                  ? 'linear-gradient(135deg, rgba(0, 0, 0, 0.22) 0%, rgba(0, 0, 0, 0.05) 25%, rgba(0, 0, 0, 0) 45%, rgba(0, 0, 0, 0) 55%, rgba(0, 0, 0, 0.04) 75%, rgba(0, 0, 0, 0.16) 100%)'
                  : 'linear-gradient(135deg, rgba(255, 255, 255, 0.40) 0%, rgba(255, 255, 255, 0.08) 25%, rgba(255, 255, 255, 0) 45%, rgba(255, 255, 255, 0) 55%, rgba(255, 255, 255, 0.06) 75%, rgba(255, 255, 255, 0.28) 100%)',
              }}
              style={{
                boxShadow: isLight
                  ? '0 6px 20px rgba(0, 0, 0, 0.08)'
                  : '0 8px 24px rgba(0, 0, 0, 0.45)',
              }}
              className="inline-flex items-center gap-2.5 px-3.5 py-2 min-h-[46px] w-fit max-w-[260px]"
            >
              <div className="shrink-0 flex items-center justify-center w-[18px] h-[18px]">
                {renderChatIcon(leftTarget.driver, leftTarget.model, isLight, 'w-[15px] h-[15px]')}
              </div>
              <div className="flex flex-col min-w-0 text-left">
                <span className="truncate text-[12.5px] font-medium leading-tight text-white/90 group-hover:text-white transition-colors">
                  {leftTarget.title}
                </span>
                <span className="flex items-center gap-2 text-[10.5px] text-white/50 leading-tight mt-1">
                  {leftTarget.isBusy !== undefined && (
                    <span
                      className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                        leftTarget.isBusy
                          ? 'bg-emerald-400 animate-pulse shadow-[0_0_6px_rgba(52,211,153,0.8)]'
                          : 'bg-white/30'
                      }`}
                    />
                  )}
                  <span>{leftTarget.subtitle}</span>
                </span>
              </div>
            </GlassSquircle>
          </button>
        )}

        {/* Bottom Right Navigation Indicator */}
        {rightTarget && cardMode !== 'browser' && cardMode !== 'terminal' && (
          <button
            type="button"
            onClick={rightTarget.onClick}
            title={rightTarget.title}
            aria-label={rightTarget.title}
            className="fixed bottom-7 right-6 z-30 pointer-events-auto cursor-pointer transition-transform duration-150 active:scale-95 group hidden md:flex"
          >
            <GlassSquircle
              radius={18}
              fill={isLight ? 'rgba(255, 255, 255, 0.72)' : 'linear-gradient(135deg, #0B0A09 0%, #191616 100%)'}
              frost={16}
              border={{
                strokeWidth: 1,
                align: 'inside',
                gradient: isLight
                  ? 'linear-gradient(135deg, rgba(0, 0, 0, 0.22) 0%, rgba(0, 0, 0, 0.05) 25%, rgba(0, 0, 0, 0) 45%, rgba(0, 0, 0, 0) 55%, rgba(0, 0, 0, 0.04) 75%, rgba(0, 0, 0, 0.16) 100%)'
                  : 'linear-gradient(135deg, rgba(255, 255, 255, 0.40) 0%, rgba(255, 255, 255, 0.08) 25%, rgba(255, 255, 255, 0) 45%, rgba(255, 255, 255, 0) 55%, rgba(255, 255, 255, 0.06) 75%, rgba(255, 255, 255, 0.28) 100%)',
              }}
              style={{
                boxShadow: isLight
                  ? '0 6px 20px rgba(0, 0, 0, 0.08)'
                  : '0 8px 24px rgba(0, 0, 0, 0.45)',
              }}
              className="inline-flex items-center gap-2.5 px-3.5 py-2 min-h-[46px] w-fit max-w-[260px]"
            >
              <div className="shrink-0 flex items-center justify-center w-[18px] h-[18px]">
                {renderChatIcon(rightTarget.driver, rightTarget.model, isLight, 'w-[15px] h-[15px]')}
              </div>
              <div className="flex flex-col min-w-0 text-left">
                <span className="truncate text-[12.5px] font-medium leading-tight text-white/90 group-hover:text-white transition-colors">
                  {rightTarget.title}
                </span>
                <span className="flex items-center gap-2 text-[10.5px] text-white/50 leading-tight mt-1">
                  <span>{rightTarget.subtitle}</span>
                  {rightTarget.isBusy !== undefined && (
                    <span
                      className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                        rightTarget.isBusy
                          ? 'bg-emerald-400 animate-pulse shadow-[0_0_6px_rgba(52,211,153,0.8)]'
                          : 'bg-white/30'
                      }`}
                    />
                  )}
                </span>
              </div>
            </GlassSquircle>
          </button>
        )}

        {/* Floating Bottom Liquid Glass Input (Home and Chat, or Split Browser left pane) */}
        {(cardMode !== 'browser' || (activeView === 'chat' && isBrowserSplit)) && cardMode !== 'terminal' && (
          <div
            className="absolute bottom-6 left-1/2 -translate-x-1/2 w-full max-w-[640px] px-4 z-40 pointer-events-auto"
            style={
              activeView === 'chat' && cardMode === 'browser' && isBrowserSplit
                ? {
                    left: `${browserSplitRatio * 50}%`,
                    maxWidth: `min(640px, calc(${browserSplitRatio * 100}% - 24px))`,
                  }
                : undefined
            }
          >
            <GlassInput
              onSubmit={handleSubmitPrompt}
              onInterrupt={
                activeView === 'chat' && activeTaskWorking
                  ? () => spawner.interrupt(activeTask.threadId)
                  : undefined
              }
              onNewChat={
                activeView === 'chat'
                  ? () => {
                      setDirection(-1);
                      setActiveView('home');
                    }
                  : undefined
              }
              isBusy={activeView === 'chat' && activeTaskWorking}
              attachments={attachments}
              isLight={isLight}
              projects={activeView === 'home' ? projects : undefined}
              activeChatKey={activeView === 'chat' ? (activeTask?.threadId || String(activeCardIndex)) : '__home__'}
              activeChatModel={activeView === 'chat' ? activeTaskModel : undefined}
              activeChatDriver={activeView === 'chat' ? activeTaskDriver : undefined}
              activeChatAccount={activeView === 'chat' ? activeTaskAccount : undefined}
            />
          </div>
        )}
      </div>

      {/* Web Bookmark Modal */}
      {isBookmarkModalOpen && (
        <BookmarkModal
          key={editingBookmark?.id || 'new-bookmark'}
          isOpen={isBookmarkModalOpen}
          onClose={() => {
            setIsBookmarkModalOpen(false);
            setEditingBookmark(null);
          }}
          onSave={handleSaveBookmark}
          onDelete={editingBookmark?.id ? handleDeleteBookmark : undefined}
          initialData={editingBookmark || undefined}
          currentProjectName={activeProjectName}
          availableProjects={projects.projects.map((p) => p.name).filter(Boolean)}
          isEditing={Boolean(editingBookmark?.id)}
        />
      )}

      {/* Settings Modal (Direct Full Center Modal) */}
      {isSettingsOpen && (
        <Suspense fallback={null}>
          <SettingsWindow
            open={isSettingsOpen}
            onClose={() => setIsSettingsOpen(false)}
            wallpaper={wallpaper}
            defaultModels={defaultModels}
            appUpdate={appUpdate}
          />
        </Suspense>
      )}
    </div>
  );
};
