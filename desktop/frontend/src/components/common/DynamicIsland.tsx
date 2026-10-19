import { ChevronLeft, ChevronRight, MaterialIcon, RefreshCw } from './icons';
import React, { useState, useRef, useEffect, useLayoutEffect, useMemo } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { ProjectsState, Project } from '../orchestrator/useProjects';
import { DynamicIslandWaveform } from './DynamicIslandWaveform';
import { ClaudeChatsView, ProjectChatsView, ProjectsView, islandTone } from './IslandLists';
import { history, session, claudeimport } from '../../../wailsjs/go/models';
import type { ChatDragPayload } from './chatDrag';
import claudeLogo from '../../assets/logo/claude-icon-logo.png';
import antigravityLogo from '../../assets/logo/antigravity-icon-logo.png';
import { useTheme } from '../../themes';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { ProjectGlyph } from '../orchestrator/ProjectGlyph';
import { providerIcon } from '../orchestrator/providerIcons';

export interface DynamicIslandNotification {
  id: string;
  title: string;
  subtitle?: string;
  type?: 'success' | 'alert' | 'info';
  threadId?: string;
  timestamp: number;
  /** Optional one-click action rendered beside the dismiss button. */
  actionLabel?: string;
  onAction?: () => void;
}

export interface DynamicIslandProps {
  projects?: ProjectsState;
  activeTaskProjectName?: string;
  activeTaskBranch?: string;
  viewMode?: 'deck' | 'grid' | 'orchestrator';
  usageReport?: session.UsageReport | null;
  usageError?: string | null;
  activeTasks?: Array<{
    threadId: string;
    title: string;
    branch?: string;
    model?: string;
    isBusy?: boolean;
    projectName?: string;
  }>;
  notification?: DynamicIslandNotification | null;
  onDismissNotification?: () => void;
  historyEntries?: history.Meta[];
  activeWorkspaceId?: string | null;
  onOpenHistory?: (workspaceId: string, threadId?: string) => void;
  onDeleteHistory?: (workspaceId: string, threadId?: string) => void;
  onSetChatArchived?: (workspaceId: string, threadId: string, archived: boolean) => void;
  onMoveChat?: (payload: ChatDragPayload, project: Project) => void;
  onTerminateAgent?: (threadId: string) => void;
  onRefreshHistory?: () => void;
  onNewChat?: () => void;
  /** Native Claude Code sessions for the inline list. */
  claudeSessions?: claudeimport.ExternalSession[];
  claudeLoading?: boolean;
  claudeLoadingMore?: boolean;
  claudeExhausted?: Record<'claude' | 'antigravity' | 'opencode', boolean>;
  onLoadMoreClaude?: (providers: Array<'claude' | 'antigravity' | 'opencode'>) => void;
  claudeImporting?: boolean;
  claudeError?: string | null;
  onRefreshClaude?: () => void;
  /** Imports the session invisibly and opens it. */
  onImportClaudeSession?: (filePath: string) => void;
  className?: string;
}

// Case and slash insensitive path matching
function isSamePath(p1?: string, p2?: string): boolean {
  if (!p1 || !p2) return false;
  const n1 = p1.replace(/\\/g, '/').toLowerCase().replace(/\/+$/, '');
  const n2 = p2.replace(/\\/g, '/').toLowerCase().replace(/\/+$/, '');
  return n1 === n2;
}

// Check if a workspace history item belongs to or relates to the active project
function matchesProject(meta: history.Meta, projectPath?: string): boolean {
  if (!projectPath) return true;
  // If the workspace has no cwd recorded (e.g. legacy/unbound session),
  // NEVER hide it! Hiding it causes chats to appear missing to the user.
  if (!meta.workspace?.cwd) return true;

  if (isSamePath(meta.workspace.cwd, projectPath)) return true;

  const n1 = meta.workspace.cwd.replace(/\\/g, '/').toLowerCase().replace(/\/+$/, '');
  const n2 = projectPath.replace(/\\/g, '/').toLowerCase().replace(/\/+$/, '');
  // Match parent or child folder relationships
  if (n1.startsWith(n2 + '/') || n2.startsWith(n1 + '/')) return true;

  // Check task worktrees
  if (meta.tasks && meta.tasks.length > 0) {
    for (const t of meta.tasks) {
      if (t.worktree?.path) {
        const wt = t.worktree.path.replace(/\\/g, '/').toLowerCase();
        if (wt.includes(n2) || isSamePath(t.worktree.path, projectPath)) return true;
      }
    }
  }

  return false;
}

// Relative time formatting for chat sessions
function relativeTime(at: number): string {
  if (!at) return '';
  const timestamp = at < 1e11 ? at * 1000 : at;
  const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000));
  if (seconds < 60) return 'just now';
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  if (days < 7) return `${days}d ago`;
  return new Date(timestamp).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

export interface ChatHistoryItem {
  id: string;
  workspaceId: string;
  threadId: string;
  title: string;
  model?: string;
  driver?: string;
  drivers?: string[];
  models?: string[];
  updatedAt: number;
  archived?: boolean;
}

export interface ChatHistoryGroup {
  label: string;
  chats: ChatHistoryItem[];
}

export function groupChatHistoryItems(items: ChatHistoryItem[]): ChatHistoryGroup[] {
  if (!items || items.length === 0) return [];

  const now = new Date();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const oneDayMs = 24 * 60 * 60 * 1000;
  const startOfYesterday = startOfToday - oneDayMs;
  const sevenDaysAgo = startOfToday - 6 * oneDayMs;
  const fourteenDaysAgo = startOfToday - 13 * oneDayMs;
  const thirtyDaysAgo = startOfToday - 29 * oneDayMs;

  const buckets: {
    today: ChatHistoryItem[];
    yesterday: ChatHistoryItem[];
    previous7Days: ChatHistoryItem[];
    lastWeek: ChatHistoryItem[];
    previous30Days: ChatHistoryItem[];
    older: ChatHistoryItem[];
  } = {
    today: [],
    yesterday: [],
    previous7Days: [],
    lastWeek: [],
    previous30Days: [],
    older: [],
  };

  for (const item of items) {
    const rawTime = item.updatedAt || 0;
    const time = rawTime < 1e11 ? rawTime * 1000 : rawTime;

    if (time >= startOfToday) {
      buckets.today.push(item);
    } else if (time >= startOfYesterday) {
      buckets.yesterday.push(item);
    } else if (time >= sevenDaysAgo) {
      buckets.previous7Days.push(item);
    } else if (time >= fourteenDaysAgo) {
      buckets.lastWeek.push(item);
    } else if (time >= thirtyDaysAgo) {
      buckets.previous30Days.push(item);
    } else {
      buckets.older.push(item);
    }
  }

  const groups: ChatHistoryGroup[] = [];
  if (buckets.today.length > 0) groups.push({ label: 'Today', chats: buckets.today });
  if (buckets.yesterday.length > 0) groups.push({ label: 'Yesterday', chats: buckets.yesterday });
  if (buckets.previous7Days.length > 0) groups.push({ label: 'Previous 7 Days', chats: buckets.previous7Days });
  if (buckets.lastWeek.length > 0) groups.push({ label: 'Last Week', chats: buckets.lastWeek });
  if (buckets.previous30Days.length > 0) groups.push({ label: 'Previous 30 Days', chats: buckets.previous30Days });
  if (buckets.older.length > 0) groups.push({ label: 'Older', chats: buckets.older });

  return groups;
}

export function extractChatHistoryItems(
  entries: history.Meta[],
  projectPath?: string,
  options: { archived?: 'exclude' | 'only' | 'include' } = {},
): ChatHistoryItem[] {
  const mode = options.archived ?? 'exclude';
  const items: ChatHistoryItem[] = [];
  const metas = projectPath
    ? (entries || []).filter((h) => matchesProject(h, projectPath))
    : (entries || []);

  for (const meta of metas) {
    const wid = meta.workspace?.id || '';
    const tasks = meta.tasks || [];

    // Group tasks by root thread ID to merge continuations
    const grouped = new Map<string, any[]>();
    const order: string[] = [];
    for (const t of tasks) {
      const rootId = t.threadId?.includes('-cont-') ? t.threadId.split('-cont-')[0] : t.threadId;
      if (!grouped.has(rootId)) {
        grouped.set(rootId, []);
        order.push(rootId);
      }
      grouped.get(rootId)!.push(t);
    }

    if (order.length === 0) {
      // Coordinator session only
      items.push({
        id: wid || `coord-${meta.coordinatorThreadId}`,
        workspaceId: wid,
        threadId: meta.coordinatorThreadId || `coordinator-${wid}`,
        title: meta.workspace?.title || meta.workspace?.prompt?.slice(0, 40) || 'Session',
        model: 'Orchestrator',
        driver: 'orchestrator',
        drivers: ['orchestrator'],
        models: ['Orchestrator'],
        updatedAt: meta.workspace?.updatedAt || meta.workspace?.createdAt || 0,
        archived: Boolean(meta.workspace?.archived),
      });
    } else {
      for (const rootId of order) {
        const groupTasks = grouped.get(rootId)!;
        const primary = groupTasks[0];
        const latest = groupTasks[groupTasks.length - 1];

        const drivers: string[] = [];
        const models: string[] = [];
        for (const gt of groupTasks) {
          if (Array.isArray(gt.drivers)) {
            for (const d of gt.drivers) if (d && !drivers.includes(d)) drivers.push(d);
          } else if (gt.driver && !drivers.includes(gt.driver)) {
            drivers.push(gt.driver);
          }
          if (Array.isArray(gt.models)) {
            for (const m of gt.models) if (m && !models.includes(m)) models.push(m);
          } else if (gt.model && !models.includes(gt.model)) {
            models.push(gt.model);
          }
        }

        // Fallback: check if switch notice is mentioned in prompt or summary
        if (drivers.length <= 1) {
          const scanText = `${primary.summary || ''} ${primary.prompt || ''} ${meta.workspace?.prompt || ''}`;
          if (scanText.includes('Switched from ')) {
            const match = scanText.match(/Switched from ([a-zA-Z0-9_\-]+)(?:: ([^t]+?))? to ([a-zA-Z0-9_\-]+)(?:: (.+?))?(?:\n|$)/);
            if (match) {
              const d1 = match[1].toLowerCase();
              const m1 = match[2]?.trim();
              const d2 = match[3].toLowerCase();
              const m2 = match[4]?.trim();
              if (d1 && !drivers.includes(d1)) drivers.unshift(d1);
              if (d2 && !drivers.includes(d2)) drivers.push(d2);
              if (m1 && !models.includes(m1)) models.unshift(m1);
              if (m2 && !models.includes(m2)) models.push(m2);
            }
          }
        }

        items.push({
          id: rootId,
          workspaceId: wid,
          threadId: rootId,
          title:
            primary.title ||
            meta.workspace?.title ||
            primary.prompt?.slice(0, 40) ||
            meta.workspace?.prompt?.slice(0, 40) ||
            'Agent',
          model: latest.model || primary.model,
          driver: latest.driver || primary.driver,
          drivers,
          models,
          updatedAt:
            Math.max(0, ...groupTasks.map((gt: any) => gt.updatedAt || 0)) ||
            meta.workspace?.updatedAt ||
            meta.workspace?.createdAt ||
            0,
          archived: Boolean(meta.workspace?.archived) || groupTasks.every((gt: any) => gt.archived),
        });
      }
    }
  }

  return items
    .filter((item) => (mode === 'include' ? true : mode === 'only' ? item.archived : !item.archived))
    .sort((a, b) => b.updatedAt - a.updatedAt);
}

export function formatModelBadge(model?: string, driver?: string): string {
  if (!model && !driver) return 'Agent';
  const known = model ? useOrchestratorStore.getState().models.find((entry) => entry.id === model)?.name : undefined;
  if (known) return known;
  const m = (model || '').toLowerCase();
  if (m.includes('gemini-3.8-flash')) return 'Gemini 3.8 Flash';
  if (m.includes('gemini-2.5-flash')) return 'Gemini 2.5 Flash';
  if (m.includes('gemini-2.5-pro')) return 'Gemini 2.5 Pro';
  if (m.includes('claude-3-5-sonnet') || m.includes('claude-3.5-sonnet')) return 'Claude 3.5 Sonnet';
  if (m.includes('claude-3-7-sonnet') || m.includes('claude-3.7-sonnet')) return 'Claude 3.7 Sonnet';
  if (m.includes('claude-3-opus')) return 'Claude 3 Opus';
  if (m.includes('muse-spark')) return 'Muse Spark 1.3';
  if (m.includes('gpt-4o')) return 'GPT-4o';
  if (m.includes('o3-mini')) return 'o3-mini';
  if (m.includes('o1')) return 'o1';
  if (model) {
    const parts = model.split('/');
    return parts[parts.length - 1];
  }
  return driver || 'Agent';
}

export function formatChatSubtitle(chat: ChatHistoryItem): string {
  const time = relativeTime(chat.updatedAt);
  const distinctModels = Array.from(new Set((chat.models || []).filter(Boolean)));
  if (distinctModels.length > 1) {
    const first = formatModelBadge(distinctModels[0], chat.drivers?.[0]);
    const last = formatModelBadge(distinctModels[distinctModels.length - 1], chat.drivers?.[chat.drivers.length - 1]);
    return `${first} → ${last} · ${time}`;
  }
  return `${formatModelBadge(chat.model, chat.driver)} · ${time}`;
}

export function renderChatIcon(driver?: string, model?: string, isLight?: boolean, sizeClass: string = 'w-[13px] h-[13px]') {
  const d = (driver || '').toLowerCase();
  const m = (model || '').toLowerCase();
  if (d === 'claude' || m.includes('claude')) {
    return <img src={claudeLogo} alt="Claude" className={`${sizeClass} object-contain shrink-0`} />;
  }
  if (d === 'opencode' || m.includes('opencode') || m.includes('muse')) {
    return (
      <img
        src={providerIcon('opencode', isLight)}
        alt="OpenCode"
        className={`${sizeClass} object-contain rounded-[2px] shrink-0`}
      />
    );
  }
  if (d === 'antigravity' || m.includes('gemini') || m.includes('antigravity')) {
    return <img src={antigravityLogo} alt="Antigravity" className={`${sizeClass} object-contain shrink-0`} />;
  }
  if (d === 'codex' || m.includes('gpt') || m.includes('openai')) {
    return <OpenAISwirlIcon className={`${sizeClass} shrink-0 ${isLight ? 'text-black' : 'text-white'}`} />;
  }
  return (
    <MaterialIcon name="chat" className="text-[14px] text-white/60 shrink-0"/>
  );
}

export function renderOverlappingChatIcons(chat: ChatHistoryItem, isLight?: boolean) {
  const drivers = chat.drivers || (chat.driver ? [chat.driver] : []);
  const models = chat.models || (chat.model ? [chat.model] : []);

  const distinctDrivers = Array.from(new Set(drivers.filter(Boolean)));
  const distinctModels = Array.from(new Set(models.filter(Boolean)));
  const hasMultiple = distinctDrivers.length > 1 || distinctModels.length > 1;

  if (!hasMultiple) {
    return (
      <div className="w-[20px] h-[20px] flex items-center justify-center shrink-0">
        {renderChatIcon(chat.driver, chat.model, isLight, 'w-[13px] h-[13px]')}
      </div>
    );
  }

  // Pick first and latest distinct models/drivers
  const firstDriver = distinctDrivers[0] || chat.driver;
  const firstModel = distinctModels[0] || chat.model;
  const lastDriver = distinctDrivers[distinctDrivers.length - 1] || chat.driver;
  const lastModel = distinctModels[distinctModels.length - 1] || chat.model;

  return (
    <div
      className="flex items-center -space-x-1.5 shrink-0 w-[24px] select-none"
      title={`Models used: ${distinctModels.map((m) => formatModelBadge(m)).join(' → ')}`}
    >
      <div
        className={`relative z-10 w-[15px] h-[15px] rounded-full flex items-center justify-center ${
          isLight
            ? 'bg-white ring-[1.5px] ring-white shadow-xs'
            : 'bg-[#18181b] ring-[1.5px] ring-[#1c1c1e] shadow-xs'
        }`}
      >
        {renderChatIcon(firstDriver, firstModel, isLight, 'w-[9.5px] h-[9.5px]')}
      </div>
      <div
        className={`relative z-20 w-[15px] h-[15px] rounded-full flex items-center justify-center ${
          isLight
            ? 'bg-white ring-[1.5px] ring-white shadow-xs'
            : 'bg-[#222226] ring-[1.5px] ring-[#1c1c1e] shadow-xs'
        }`}
      >
        {renderChatIcon(lastDriver, lastModel, isLight, 'w-[9.5px] h-[9.5px]')}
      </div>
    </div>
  );
}

// Resets timer label
function formatResets(resetsAtSeconds: number): string {
  if (!resetsAtSeconds) return '';
  const minutes = Math.round((resetsAtSeconds * 1000 - Date.now()) / 60_000);
  if (minutes <= 0) return 'resets soon';
  if (minutes < 60) return `Resets in ${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  if (hours < 24) {
    return remMinutes > 0 ? `Resets in ${hours}h ${remMinutes}m` : `Resets in ${hours}h`;
  }
  const date = new Date(resetsAtSeconds * 1000);
  const dayName = date.toLocaleDateString('en-US', { weekday: 'short' });
  const timeStr = date.toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' });
  return `Resets ${dayName} ${timeStr}`;
}

function compactCount(value: number): string {
  if (!value) return '0';
  if (value < 1000) return `${value}`;
  if (value < 1_000_000) {
    const v = value / 1000;
    return `${v >= 100 ? Math.round(v) : v.toFixed(1).replace(/\.0$/, '')}K`;
  }
  const v = value / 1_000_000;
  return `${v >= 100 ? Math.round(v) : v.toFixed(1).replace(/\.0$/, '')}M`;
}

// Clean OpenAI swirl icon
const OpenAISwirlIcon: React.FC<{ className?: string }> = ({ className = 'w-3 h-3' }) => (
  <svg viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path d="M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1683a.071.071 0 0 1 .038.052v5.5826a4.5045 4.5045 0 0 1-4.4945 4.4947zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1683a.0757.0757 0 0 1-.071 0l-4.8303-2.7866A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.6669zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.4097 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1636a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813v6.7227zm1.1444-2.8298l2.5484-1.4678 2.5484 1.4678v2.9356l-2.5484 1.4678-2.5484-1.4678z" />
  </svg>
);

export const DynamicIsland: React.FC<DynamicIslandProps> = ({
  projects,
  activeTaskProjectName,
  activeTaskBranch,
  viewMode = 'deck',
  usageReport,
  activeTasks = [],
  notification,
  onDismissNotification,
  historyEntries = [],
  activeWorkspaceId,
  onOpenHistory,
  onSetChatArchived,
  onMoveChat,
  onRefreshHistory,
  onNewChat,
  claudeSessions = [],
  claudeLoading,
  claudeLoadingMore,
  claudeExhausted,
  onLoadMoreClaude,
  claudeImporting,
  claudeError,
  onRefreshClaude,
  onImportClaudeSession,
  className = '',
}) => {
  // Mode: 'idle' (compact), 'projects' (expanded project picker), 'project-chats' (project's past chats), 'claude-chats' (native Claude Code chats), 'usage' (expanded usage card)
  const [mode, setMode] = useState<'idle' | 'projects' | 'project-chats' | 'claude-chats' | 'usage'>('idle');
  const [selectedProject, setSelectedProject] = useState<Project | null>(null);
  const islandRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const [contentHeight, setContentHeight] = useState(0);

  useLayoutEffect(() => {
    const node = contentRef.current;
    if (!node) return;
    const update = () => setContentHeight(Math.ceil(node.getBoundingClientRect().height));
    update();
    const observer = new ResizeObserver(update);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (mode === 'projects' || mode === 'project-chats') {
      onRefreshHistory?.();
    }
  }, [mode, onRefreshHistory]);

  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';
  const isGlass = currentTheme.id === 'glass';
  const tone = useMemo(() => islandTone(isLight), [isLight]);
  const selectedProviderId = useOrchestratorStore((s) => s.selectedProviderId);
  const selectedModelId = useOrchestratorStore((s) => s.selectedModelId);

  // Active task and model detection
  const activeTask = activeTasks[0];
  const anyBusyTask = useMemo(() => activeTasks.find((t) => t.isBusy), [activeTasks]);

  // Project name
  const displayedProjectName =
    viewMode === 'deck' && activeTaskProjectName
      ? activeTaskProjectName
      : projects?.active?.name ||
        (projects && projects.projects.length > 0 ? projects.projects[0].name : 'Select Project');

  // Best-effort id for the favicon lookup; empty means the folder glyph stays.
  const displayedProjectId =
    viewMode === 'deck' && activeTaskProjectName
      ? (projects?.projects.find((p) => p.name === activeTaskProjectName)?.id ?? '')
      : (projects?.active?.id ?? '');

  // Only chats the user opened and wrote in: agent-spawned runs and AI side
  // tasks never appear here.
  const handClaudeChats = useMemo(() => (claudeSessions ?? []).filter((s) => !s.agentRun), [claudeSessions]);

  const recentClaudeChats = useMemo(
    () => [...handClaudeChats].sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0)).slice(0, 3),
    [handClaudeChats],
  );

  const chatCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const project of projects?.projects ?? []) {
      counts[project.id] = extractChatHistoryItems(historyEntries || [], project.path).length;
    }
    return counts;
  }, [projects?.projects, historyEntries]);

  const runningThreadIds = useMemo(() => {
    const ids = new Set<string>();
    for (const task of activeTasks) {
      if (!task.isBusy) continue;
      ids.add(task.threadId);
      const root = task.threadId.split('-cont-')[0];
      ids.add(root);
    }
    return ids;
  }, [activeTasks]);

  // Identify model vendor from selectedProviderId, selectedModelId, activeTask or usageReport
  type UsageDriverName = 'claude' | 'codex' | 'antigravity' | 'opencode';
  const autoDriverName = useMemo<UsageDriverName>(() => {
    if (selectedProviderId === 'claude') return 'claude';
    if (selectedProviderId === 'codex') return 'codex';
    if (selectedProviderId === 'antigravity') return 'antigravity';
    if (selectedProviderId === 'opencode') return 'opencode';

    const m = (selectedModelId || activeTask?.model || '').toLowerCase();
    if (m.includes('claude')) return 'claude';
    if (m.includes('gpt') || m.includes('codex') || m.includes('openai')) return 'codex';
    if (m.includes('gemini') || m.includes('antigravity')) return 'antigravity';
    if (m.includes('opencode')) return 'opencode';

    // Fallback based on drivers in report
    const available = usageReport?.drivers?.map((d) => d.driver) || [];
    if (available.includes('claude')) return 'claude';
    if (available.includes('codex')) return 'codex';
    if (available.includes('antigravity')) return 'antigravity';
    if (available.includes('opencode')) return 'opencode';
    return 'claude';
  }, [selectedProviderId, selectedModelId, activeTask?.model, usageReport]);

  const [viewedDriver, setViewedDriver] = useState<UsageDriverName | null>(null);
  const [viewedAccounts, setViewedAccounts] = useState<Record<string, string>>({});
  const driverName = viewedDriver ?? autoDriverName;

  useEffect(() => {
    if (mode !== 'usage') {
      setViewedDriver(null);
      setViewedAccounts({});
    }
  }, [mode]);

  const usageDrivers = useMemo<UsageDriverName[]>(() => {
    const order: UsageDriverName[] = ['claude', 'codex', 'antigravity', 'opencode'];
    const present = new Set((usageReport?.drivers ?? []).map((d) => d.driver));
    const list = order.filter((name) => present.has(name));
    return list.includes(autoDriverName) ? list : [autoDriverName, ...list];
  }, [usageReport, autoDriverName]);

  const stepDriver = (delta: number) => {
    const index = usageDrivers.indexOf(driverName);
    const next = usageDrivers[(index + delta + usageDrivers.length) % usageDrivers.length];
    setViewedDriver(next);
  };

  const storeShownAccount = useOrchestratorStore((state) => state.shownAccount(driverName));
  const shownAccount = viewedAccounts[driverName] ?? storeShownAccount;
  const driverAccounts = useMemo(() => {
    const entries = usageReport?.drivers?.filter((d) => d.driver === driverName) ?? [];
    return entries.map((d) => ({ id: d.account || 'default', name: d.accountName || d.account || 'Default' }));
  }, [usageReport, driverName]);

  const stepAccount = () => {
    if (driverAccounts.length < 2) return;
    const index = driverAccounts.findIndex((account) => account.id === shownAccount);
    const next = driverAccounts[(index + 1) % driverAccounts.length];
    setViewedAccounts((prev) => ({ ...prev, [driverName]: next.id }));
  };
  const shownAccountName = driverAccounts.find((account) => account.id === shownAccount)?.name;

  const driverData = useMemo(() => {
    const entries = usageReport?.drivers?.filter((d) => d.driver === driverName) ?? [];
    return (
      entries.find((d) => (d.account || 'default') === shownAccount) ||
      entries.find((d) => (d.account || 'default') === 'default') ||
      entries[0]
    );
  }, [usageReport, driverName, shownAccount]);

  // Session limit (five_hour window)
  const sessionLimit = useMemo(() => {
    return (
      driverData?.limits?.find((l) => l.window === 'five_hour') ||
      driverData?.limits?.[0] ||
      null
    );
  }, [driverData]);

  // All models / weekly limit (seven_day window)
  const weeklyLimit = useMemo(() => {
    return (
      driverData?.limits?.find(
        (l) => l.window.startsWith('seven_day') && l.window !== sessionLimit?.window,
      ) ||
      driverData?.limits?.[1] ||
      null
    );
  }, [driverData, sessionLimit]);

  const sessionUsedPercent = typeof sessionLimit?.usedPercent === 'number' ? sessionLimit.usedPercent : 0;
  const weeklyUsedPercent = typeof weeklyLimit?.usedPercent === 'number' ? weeklyLimit.usedPercent : 0;
  const hasQuota = sessionLimit !== null || weeklyLimit !== null;

  // Theme color for selected model (inspired by user Image 2)
  const modelTheme = useMemo(() => {
    switch (driverName) {
      case 'claude':
        return {
          name: 'Claude',
          ringColor: '#f97316', // Coral/Orange from Image 2
          barColor: '#f97316',
          icon: <img src={claudeLogo} alt="Claude" className="w-[12px] h-[12px] object-contain" />,
        };
      case 'codex':
        return {
          name: 'OpenAI',
          ringColor: '#10b981', // Teal from Image 2
          barColor: '#10b981',
          icon: <OpenAISwirlIcon className={`w-[12px] h-[12px] ${isLight ? 'text-[#1d1d1f]' : 'text-white'}`} />,
        };
      case 'opencode':
        return {
          name: 'OpenCode',
          ringColor: '#3b82f6',
          barColor: '#3b82f6',
          icon: (
            <img
              src={providerIcon('opencode', isLight)}
              alt="OpenCode"
              className="w-[12px] h-[12px] object-contain rounded-[2px]"
            />
          ),
        };
      case 'antigravity':
      default:
        return {
          name: 'Antigravity',
          ringColor: '#eab308', // Amber/Yellow from Image 2
          barColor: '#eab308',
          icon: <img src={antigravityLogo} alt="Antigravity" className="w-[12px] h-[12px] object-contain" />,
        };
    }
  }, [driverName, isLight]);

  // Close on outside click or Escape
  useEffect(() => {
    if (mode === 'idle') return;

    const handlePointerDown = (e: MouseEvent) => {
      if (islandRef.current && !islandRef.current.contains(e.target as Node)) {
        setMode('idle');
      }
    };

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setMode('idle');
      }
    };

    window.addEventListener('mousedown', handlePointerDown);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('mousedown', handlePointerDown);
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [mode]);

  // Spring physics config
  const springTransition = {
    type: 'spring' as const,
    stiffness: 440,
    damping: 32,
    mass: 0.8,
  };

  const isListMode = mode === 'project-chats' || mode === 'claude-chats' || mode === 'projects';
  const targetWidth =
    mode === 'project-chats' || mode === 'claude-chats'
      ? 372
      : mode === 'projects'
      ? 348
      : mode === 'usage'
      ? 315
      : notification
      ? 320
      : 275;
  const targetHeight = isListMode ? Math.max(contentHeight, 60) : mode === 'usage' ? 180 : 36;
  const targetRadius = mode === 'idle' ? 18 : 26;
  const viewMotion = {
    initial: { opacity: 0, y: -6 },
    animate: { opacity: 1, y: 0 },
    exit: { opacity: 0, y: -4, transition: { duration: 0.1 } },
    transition: { duration: 0.2, ease: [0.16, 1, 0.3, 1] as const },
  };

  // SVG Ring values for 20px circle (r=8 -> circum=50.26)
  const ringCircumference = 50.26;
  const ringOffset = ringCircumference - (ringCircumference * Math.min(100, sessionUsedPercent)) / 100;

  return (
    <motion.div
      ref={islandRef}
      initial={false}
      animate={{
        width: targetWidth,
        height: targetHeight,
        borderRadius: targetRadius,
      }}
      transition={springTransition}
      className={`relative z-[70] overflow-hidden select-none pointer-events-auto ${className}`}
      style={{
        ['--wails-draggable' as string]: 'no-drag',
        transformOrigin: 'top center',
        backgroundColor: isLight
          ? 'rgba(255, 255, 255, 0.94)'
          : isGlass
          ? 'rgba(10, 14, 26, 0.65)'
          : 'rgba(3, 3, 3, 0.95)',
        border: isLight
          ? '1px solid rgba(0, 0, 0, 0.10)'
          : isGlass
          ? '1px solid rgba(255, 255, 255, 0.22)'
          : '1px solid rgba(255, 255, 255, 0.08)',
        backdropFilter: isGlass ? 'blur(16px) saturate(150%)' : undefined,
        WebkitBackdropFilter: isGlass ? 'blur(16px) saturate(150%)' : undefined,
        boxShadow: isLight
          ? '0 12px 32px rgba(0, 0, 0, 0.12), 0 4px 12px rgba(0, 0, 0, 0.06)'
          : isGlass
          ? '0 16px 40px rgba(0, 0, 0, 0.5), 0 4px 12px rgba(0, 0, 0, 0.3)'
          : '0 16px 40px rgba(0, 0, 0, 0.85), 0 4px 12px rgba(0, 0, 0, 0.6)',
      }}
    >
      <div ref={contentRef} className="w-full" style={{ height: isListMode ? 'auto' : '100%' }}>
      <AnimatePresence mode="wait" initial={false}>
        {/* ===================================================================
            1. COMPACT IDLE STATE (Notification or Two clickable zones)
            =================================================================== */}
        {mode === 'idle' && notification && (
          <motion.div
            key="compact-notification"
            initial={{ opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 4 }}
            transition={{ duration: 0.15 }}
            onClick={() => {
              if (notification.threadId) {
                onOpenHistory?.('', notification.threadId);
              }
              onDismissNotification?.();
            }}
            className="flex items-center justify-between w-full h-full px-2.5 cursor-pointer"
            title="Click to view agent session"
          >
            <div className="flex items-center gap-2 min-w-0">
              <span className="w-2 h-2 rounded-full bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,1)] shrink-0 animate-pulse" />
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="text-[12px] font-semibold text-emerald-300 tracking-tight shrink-0 font-(family-name:--app-font)">
                  {notification.title}
                </span>
                {notification.subtitle && (
                  <span className="text-[11px] text-white/60 truncate font-(family-name:--app-font)">
                    · {notification.subtitle}
                  </span>
                )}
              </div>
            </div>
            <div className="flex items-center shrink-0">
              {notification.onAction && notification.actionLabel && (
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    notification.onAction?.();
                  }}
                  className="h-[22px] px-2.5 mr-1 rounded-full bg-[#007AFF] hover:bg-[#0A84FF] text-white text-[11px] font-medium font-(family-name:--app-font) active:scale-95 transition-all cursor-pointer"
                >
                  {notification.actionLabel}
                </button>
              )}
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onDismissNotification?.();
                }}
                className="w-4 h-4 rounded-full flex items-center justify-center text-white/40 hover:text-white transition-colors ml-1 cursor-pointer"
              >
                <MaterialIcon name="close" className="text-[13px]"/>
              </button>
            </div>
          </motion.div>
        )}

        {mode === 'idle' && !notification && (
          <motion.div
            key="compact"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.12 }}
            className="flex items-center justify-between w-full h-full px-2"
          >
            {/* Left Clickable Zone: Project Button */}
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                setMode('projects');
              }}
              className={`flex items-center gap-2 h-[28px] px-2 rounded-full ${
                isLight ? 'hover:bg-black/[0.06]' : 'hover:bg-white/[0.08]'
              } active:scale-95 transition-all cursor-pointer min-w-0`}
              title={`Project: ${displayedProjectName}\nClick to switch`}
            >
              <ProjectGlyph
                projectId={displayedProjectId}
                size={15}
                glyph={
                  <MaterialIcon name="folder" className="text-[15px] text-white/60 shrink-0"/>
                }
              />
              <span className="text-[12.5px] font-medium text-white tracking-tight truncate max-w-[110px]">
                {displayedProjectName}
              </span>
              {activeTaskBranch && viewMode === 'deck' && (
                <span className="text-[10.5px] font-mono text-white/40 truncate max-w-[60px] hidden sm:inline">
                  · {activeTaskBranch}
                </span>
              )}
            </button>

            {/* Middle Working Waveform (if active) */}
            {anyBusyTask && (
              <div className="flex items-center px-1">
                <DynamicIslandWaveform />
              </div>
            )}

            {/* Right Clickable Zone: Model Usage Ring Button */}
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                setMode('usage');
              }}
              className={`flex items-center gap-1.5 h-[28px] px-2 rounded-full ${
                isLight ? 'hover:bg-black/[0.06]' : 'hover:bg-white/[0.08]'
              } active:scale-95 transition-all cursor-pointer shrink-0`}
              title={`${modelTheme.name} Usage: ${sessionUsedPercent}%\nClick for details`}
            >
              <div className="relative w-[20px] h-[20px] flex items-center justify-center shrink-0">
                {/* SVG Circular Ring */}
                <svg className="absolute inset-0 w-full h-full -rotate-90" viewBox="0 0 20 20">
                  <circle
                    cx="10"
                    cy="10"
                    r="8"
                    fill="none"
                    stroke={isLight ? 'rgba(0, 0, 0, 0.14)' : 'rgba(255, 255, 255, 0.18)'}
                    strokeWidth="2"
                  />
                  <circle
                    cx="10"
                    cy="10"
                    r="8"
                    fill="none"
                    stroke={modelTheme.ringColor}
                    strokeWidth="2"
                    strokeDasharray={ringCircumference}
                    strokeDashoffset={ringOffset}
                    strokeLinecap="round"
                    className="transition-[stroke-dashoffset] duration-300"
                  />
                </svg>
                {/* Center model icon */}
                <div className="flex items-center justify-center">
                  {modelTheme.icon}
                </div>
              </div>

              <span className="text-[11px] font-semibold text-white/90 tabular-nums font-(family-name:--app-font)">
                {sessionUsedPercent}%
              </span>
            </button>
          </motion.div>
        )}

        {mode === 'projects' && (
          <motion.div key="projects-view" {...viewMotion} className="w-full">
            <ProjectsView
              tone={tone}
              projects={projects}
              chatCounts={chatCounts}
              claudeCount={claudeExhausted && Object.values(claudeExhausted).every(Boolean) ? handClaudeChats.length : 0}
              recentClaude={recentClaudeChats}
              relativeTime={relativeTime}
              onOpenClaudeSession={(chat) => {
                onImportClaudeSession?.(chat.filePath);
                setMode('idle');
              }}
              onClose={() => setMode('idle')}
              onPicked={() => setMode('idle')}
              onOpenProjectChats={(project) => {
                setSelectedProject(project);
                setMode('project-chats');
              }}
              onOpenClaude={() => {
                onRefreshClaude?.();
                setMode('claude-chats');
              }}
              onMoveChat={onMoveChat}
            />
          </motion.div>
        )}

        {mode === 'project-chats' && selectedProject && (
          <motion.div key="project-chats-view" {...viewMotion} className="w-full">
            <ProjectChatsView
              tone={tone}
              project={selectedProject}
              groups={groupChatHistoryItems(extractChatHistoryItems(historyEntries || [], selectedProject.path))}
              archived={extractChatHistoryItems(historyEntries || [], selectedProject.path, { archived: 'only' })}
              runningThreadIds={runningThreadIds}
              renderIcon={(chat) => renderOverlappingChatIcons(chat, isLight)}
              subtitle={formatChatSubtitle}
              onBack={() => setMode('projects')}
              onClose={() => setMode('idle')}
              onNewChat={() => {
                void (async () => {
                  if (selectedProject.id !== projects?.active?.id) {
                    await projects?.select(selectedProject.id);
                  }
                  onNewChat?.();
                })();
                setMode('idle');
              }}
              onOpen={(chat) => {
                if (selectedProject.id !== projects?.active?.id) {
                  void projects?.select(selectedProject.id);
                }
                if (chat.archived) onSetChatArchived?.(chat.workspaceId, chat.threadId, false);
                onOpenHistory?.(chat.workspaceId, chat.threadId);
                setMode('idle');
              }}
              onSetArchived={(chat, archived) => onSetChatArchived?.(chat.workspaceId, chat.threadId, archived)}
            />
          </motion.div>
        )}

        {mode === 'claude-chats' && (
          <motion.div key="claude-chats-view" {...viewMotion} className="w-full">
            <ClaudeChatsView
              tone={tone}
              sessions={handClaudeChats}
              loading={claudeLoading}
              loadingMore={claudeLoadingMore}
              exhausted={claudeExhausted}
              onLoadMore={onLoadMoreClaude}
              importing={claudeImporting}
              error={claudeError}
              relativeTime={relativeTime}
              onBack={() => setMode('projects')}
              onClose={() => setMode('idle')}
              onOpen={(session) => {
                onImportClaudeSession?.(session.filePath);
                setMode('idle');
              }}
            />
          </motion.div>
        )}

        {/* ===================================================================
            3. EXPANDED USAGE CARD (Inspired directly by user Image 2)
            =================================================================== */}
        {mode === 'usage' && (
          <motion.div
            key="usage-view"
            initial={{ opacity: 0, scale: 0.96 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.96 }}
            transition={{ duration: 0.15 }}
            className="flex flex-col w-full h-full p-4 justify-between"
          >
            <div className="flex items-center justify-between pb-1">
              <div className="flex items-center gap-1 min-w-0">
                {usageDrivers.length > 1 && (
                <button
                  type="button"
                  title="Previous provider"
                  aria-label="Previous provider"
                  onClick={() => stepDriver(-1)}
                  className={`w-5 h-5 rounded-full ${
                    isLight ? 'hover:bg-black/10 text-black/40 hover:text-black' : 'hover:bg-white/10 text-white/40 hover:text-white'
                  } active:scale-90 flex items-center justify-center transition-all cursor-pointer`}
                >
                  <ChevronLeft size={13} strokeWidth={2} />
                </button>
                )}
                <div className="w-5 h-5 flex items-center justify-center shrink-0">{modelTheme.icon}</div>
                <span className="text-[13px] font-semibold text-white font-(family-name:--app-font) tracking-tight truncate">
                  {modelTheme.name} Usage
                </span>
                {usageDrivers.length > 1 && (
                <button
                  type="button"
                  title="Next provider"
                  aria-label="Next provider"
                  onClick={() => stepDriver(1)}
                  className={`w-5 h-5 rounded-full ${
                    isLight ? 'hover:bg-black/10 text-black/40 hover:text-black' : 'hover:bg-white/10 text-white/40 hover:text-white'
                  } active:scale-90 flex items-center justify-center transition-all cursor-pointer`}
                >
                  <ChevronRight size={13} strokeWidth={2} />
                </button>
                )}
                {driverAccounts.length > 1 && (
                  <button
                    type="button"
                    title="Switch account"
                    onClick={stepAccount}
                    className={`ml-1 h-[20px] pl-1.5 pr-1 rounded-full flex items-center gap-1 text-[10px] font-medium font-(family-name:--app-font) tracking-tight shrink-0 active:scale-95 transition-all cursor-pointer ${
                      isLight ? 'bg-black/[0.06] text-black/60 hover:bg-black/10' : 'bg-white/[0.08] text-white/60 hover:bg-white/[0.14]'
                    }`}
                  >
                    <span className="max-w-[64px] truncate">{shownAccountName}</span>
                    <RefreshCw size={10} strokeWidth={2} />
                  </button>
                )}
              </div>
              <button
                type="button"
                onClick={() => setMode('idle')}
                className={`w-5 h-5 rounded-full ${
                  isLight ? 'hover:bg-black/10 text-black/40 hover:text-black' : 'hover:bg-white/10 text-white/40 hover:text-white'
                } active:scale-90 flex items-center justify-center transition-all cursor-pointer`}
              >
                <MaterialIcon name="close" className="text-[14px]"/>
              </button>
            </div>

            {!hasQuota ? (
              <div className="flex flex-col gap-1.5 my-1">
                <div className="flex items-center justify-between text-[11px] font-(family-name:--app-font)">
                  <span className="text-white/60">This run</span>
                </div>
                <div className="flex items-center gap-3 text-[11px] font-(family-name:--app-font) text-white/80 tabular-nums">
                  <span>↓ {compactCount(driverData?.inputTokens ?? 0)} in</span>
                  <span>↑ {compactCount(driverData?.outputTokens ?? 0)} out</span>
                  {(driverData?.costUsd ?? 0) > 0 && (
                    <span>${(driverData?.costUsd ?? 0).toFixed(2)}</span>
                  )}
                </div>
                <span className="text-[10px] text-white/45 font-(family-name:--app-font)">
                  No quota — sign in to OpenCode Go for limits
                </span>
              </div>
            ) : (
              <>
                {/* Current Session Limit (Matching Image 2) */}
                <div className="flex flex-col gap-1.5 my-1">
                  <div className="flex items-center justify-between text-[11px] font-(family-name:--app-font)">
                    <span className="text-white/60">Current session</span>
                    <span className="text-[#8E8E93] text-[10px]">
                      {sessionLimit?.resetsAt ? formatResets(sessionLimit.resetsAt) : ''}
                    </span>
                  </div>

                  {/* Progress bar matching Image 2 */}
                  <div className={`h-[4px] rounded-full ${isLight ? 'bg-black/[0.08]' : 'bg-white/[0.12]'} overflow-hidden`}>
                    <div
                      className="h-full rounded-full transition-all duration-300"
                      style={{
                        width: `${Math.min(100, Math.max(sessionUsedPercent > 0 ? 3 : 0, sessionUsedPercent))}%`,
                        backgroundColor: modelTheme.barColor,
                      }}
                    />
                  </div>

                  <span className="text-[10px] text-white/80 font-medium font-(family-name:--app-font)">
                    {sessionUsedPercent}% Used
                  </span>
                </div>

                {/* All Models / Weekly Limit (Matching Image 2) */}
                <div className="flex flex-col gap-1.5">
                  <div className="flex items-center justify-between text-[11px] font-(family-name:--app-font)">
                    <span className="text-white/60">All models</span>
                    <span className="text-[#8E8E93] text-[10px]">
                      {weeklyLimit?.resetsAt ? formatResets(weeklyLimit.resetsAt) : ''}
                    </span>
                  </div>

                  {/* Progress bar matching Image 2 */}
                  <div className={`h-[4px] rounded-full ${isLight ? 'bg-black/[0.08]' : 'bg-white/[0.12]'} overflow-hidden`}>
                    <div
                      className="h-full rounded-full transition-all duration-300"
                      style={{
                        width: `${Math.min(100, Math.max(weeklyUsedPercent > 0 ? 3 : 0, weeklyUsedPercent))}%`,
                        backgroundColor: '#10b981', // Green from Image 2
                      }}
                    />
                  </div>

                  <span className="text-[10px] text-white/80 font-medium font-(family-name:--app-font)">
                    {weeklyUsedPercent}% Used
                  </span>
                </div>
              </>
            )}
          </motion.div>
        )}
      </AnimatePresence>
      </div>
    </motion.div>
  );
};

export default DynamicIsland;
