import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { onRuntimeEvents } from '../agent-session/runtimeEvents';
import {
  ActiveProject,
  InterruptTurn,
  IsGitRepo,
  LoadHistory,
  LoadHistoryTail,
  OrchestratorLatest,
  OrchestratorMessageAgent,
  ParseTasks,
  ResumeHistoryThread,
  SendTurn,
  DeleteTurn,
  MoveChat,
  RenameChat,
  SpawnFromImported,
  SpawnTasks,
  StartSkillTest,
  StopSession,
  SwitchTaskAccountWithOptions,
  UpdateTaskModel,
} from '../../../wailsjs/go/main/App';
import { domain, history, main, session } from '../../../wailsjs/go/models';
import { AgentStreamBlock } from '../agent-session';
import type { ChatDragPayload } from '../common/chatDrag';
import { reduceEvent, RuntimeEvent, userBlock } from '../agent-session/eventReducer';
import { ContextUsage, latestContext, nextContext } from '../agent-session/contextUsage';
import { useOrchestratorStore } from './useOrchestratorStore';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import { toModelOptions } from './orchestratorData';

export interface PendingPlan {
  tasks: session.TaskRequest[];
  canUseWorktree: boolean;
}

export interface SpawnedTask {
  threadId: string;
  cardKey: string;
  title: string;
  branch?: string;
  model?: string;
  driver?: string;
  account?: string;
  drivers?: string[];
  models?: string[];
  blocks: AgentStreamBlock[];
  isBusy: boolean;
  workspaceId?: string;
  isLive?: boolean;
  /** Frontend-only blank tab. No CLI is running until the first message sends. */
  isDraft?: boolean;
  /** Set on tasks replayed from an imported outside chat (e.g. "claude-code"). */
  importedFrom?: string;
  projectName?: string;
  projectPath?: string;
  /** Server timestamp of the running turn's start; set while the agent works. */
  workStartedAt?: number;
  /** Duration of the most recent finished turn, in milliseconds. */
  lastTurnMs?: number;
  context?: ContextUsage;
  hasEarlier?: boolean;
  loadingEarlier?: boolean;
}

export interface Spawner {
  plan: PendingPlan | null;
  tasks: SpawnedTask[];
  backgroundTasks: SpawnedTask[];
  allActiveTasks: SpawnedTask[];
  error: string | null;
  workspaceId: string | null;
  canUseWorktree: boolean;
  /** Plan keys The Orchestrator already launched backend-side (status, not confirm). */
  launchedKeys: string[];
  /** Detects a delegation plan in an orchestrator reply. */
  inspect: (text: string) => Promise<void>;
  confirm: (useWorktree: boolean, modelIds?: string[]) => Promise<void>;
  confirmTasks: (
    tasks: { title: string; prompt: string; cwd?: string; action?: string; targetThreadId?: string }[],
    useWorktree: boolean,
    modelIds?: string[],
    project?: { name: string; path: string },
  ) => Promise<void>;
  /** Adopts agents The Orchestrator launched itself into the deck. */
  adoptSpawned: (result: session.SpawnResult) => void;
  dismiss: () => void;
  send: (threadId: string, text: string, files?: domain.FileRef[]) => Promise<void>;
  sendWithModel: (
    threadId: string,
    text: string,
    files?: domain.FileRef[],
    modelId?: string,
  ) => Promise<string | undefined>;
  interrupt: (threadId: string) => Promise<void>;
  close: (threadId: string) => Promise<void>;
  terminate: (threadId: string) => Promise<void>;
  /** Directly spawns a fresh agent with its own clean context */
  spawnAgent: (
    prompt: string,
    title?: string,
    modelId?: string,
    project?: { name: string; path: string },
    files?: domain.FileRef[],
    replaceDraftId?: string,
  ) => Promise<string | undefined>;
  /** Opens a blank local tab. Project/CLI are decided on first send. */
  openDraft: () => string;
  spawnSkillTest: (
    prompt: string,
    files: domain.FileRef[],
    skills: string[],
    modelId?: string,
    project?: { name: string; path: string },
  ) => Promise<string | undefined>;
  /** Opens a specific task from a past session directly into the active deck (always 1 chat) */
  openHistorySession: (workspaceId: string, threadId?: string) => Promise<number>;
  loadEarlier: (threadId: string) => Promise<void>;
  /** Drops every window after the backend has stopped their sessions. */
  clear: () => void;
  deleteTurn: (threadId: string, turnId: string) => Promise<void>;
  moveChat: (payload: ChatDragPayload, project: { name: string; path: string }) => Promise<void>;
  renameChat: (threadId: string, title: string) => Promise<void>;
}

export interface UseSpawnerOptions {
  onBackgroundComplete?: (task: SpawnedTask) => void;
}

function replay(events: RuntimeEvent[] | undefined): AgentStreamBlock[] {
  if (!events?.length) return [];
  return events.reduce<AgentStreamBlock[]>((blocks, event) => reduceEvent(blocks, event), []);
}

function threadBlocks(
  meta: history.Meta,
  rawTranscripts: Record<string, RuntimeEvent[]>,
  threadId: string,
): AgentStreamBlock[] {
  const rawTasks = meta.tasks ? [...meta.tasks] : [];
  const { tasks: loadedTasks, transcripts } = normalizeSessionTasks(rawTasks, rawTranscripts);
  const t = loadedTasks.find((item) => item.threadId === threadId) as any;
  const replayed: AgentStreamBlock[] = (t && t.blocks) || replay(transcripts[threadId]);
  const firstNonNotice = replayed.find((b) => b.type !== 'notice');
  const prompt = t?.prompt || meta.workspace?.prompt;
  return firstNonNotice?.type !== 'user' && prompt
    ? [
        userBlock(prompt, `orch-prompt-${threadId}`, [], t?.createdAt || meta.workspace?.createdAt || Date.now()),
        ...replayed,
      ]
    : replayed;
}

/** Merges any split continuation tasks into their root conversation thread */
export function normalizeSessionTasks(
  tasks: any[],
  transcripts: Record<string, RuntimeEvent[]>,
): { tasks: any[]; transcripts: Record<string, RuntimeEvent[]> } {
  const mergedTranscripts = { ...transcripts };
  const taskMap = new Map<string, any>();
  const order: string[] = [];

  // Merge any orphaned continuation transcript files into their root thread
  for (const tKey of Object.keys(transcripts || {})) {
    if (tKey.includes('-cont-')) {
      const rootKey = tKey.split('-cont-')[0];
      const contEvents = transcripts[tKey] || [];
      if (contEvents.length > 0) {
        mergedTranscripts[rootKey] = [...(mergedTranscripts[rootKey] || []), ...contEvents];
      }
    }
  }

  for (const t of tasks || []) {
    const rootId = t.threadId?.includes('-cont-')
      ? t.threadId.split('-cont-')[0]
      : t.threadId;

    if (!taskMap.has(rootId)) {
      const drivers = Array.isArray(t.drivers) ? [...t.drivers] : t.driver ? [t.driver] : [];
      const models = Array.isArray(t.models) ? [...t.models] : t.model ? [t.model] : [];
      taskMap.set(rootId, { ...t, threadId: rootId, drivers, models });
      order.push(rootId);
    } else {
      const primary = taskMap.get(rootId)!;
      if (!primary.drivers) primary.drivers = primary.driver ? [primary.driver] : [];
      if (t.driver && !primary.drivers.includes(t.driver)) primary.drivers.push(t.driver);
      if (Array.isArray(t.drivers)) {
        for (const d of t.drivers) if (d && !primary.drivers.includes(d)) primary.drivers.push(d);
      }

      if (!primary.models) primary.models = primary.model ? [primary.model] : [];
      if (t.model && !primary.models.includes(t.model)) primary.models.push(t.model);
      if (Array.isArray(t.models)) {
        for (const m of t.models) if (m && !primary.models.includes(m)) primary.models.push(m);
      }

      if (t.driver) primary.driver = t.driver;
      if (t.model) primary.model = t.model;
      if (t.options) primary.options = t.options;
      if (t.state) primary.state = t.state;
      if (t.updatedAt && t.updatedAt > (primary.updatedAt || 0)) {
        primary.updatedAt = t.updatedAt;
      }
      const contEvents = transcripts[t.threadId] || [];
      if (contEvents.length > 0 && !t.threadId.includes('-cont-')) {
        mergedTranscripts[rootId] = [...(mergedTranscripts[rootId] || []), ...contEvents];
      }
    }
  }

  return {
    tasks: order.map((id) => taskMap.get(id)!),
    transcripts: mergedTranscripts,
  };
}

/** Duration of the most recent finished turn in a stored transcript. */
function lastTurnDuration(events: RuntimeEvent[] | undefined): number | undefined {
  if (!events?.length) return undefined;
  const started = new Map<string, number>();
  let last: number | undefined;
  for (const event of events) {
    if (event.kind === 'turn.started' && event.turnId) {
      started.set(event.turnId, event.at || 0);
    } else if (
      (event.kind === 'turn.completed' || event.kind === 'turn.failed') &&
      event.turnId
    ) {
      const at = started.get(event.turnId);
      if (at) last = Math.max(0, (event.at || at) - at);
    }
  }
  return last;
}


/** Folds one runtime event into a task card's state. */
function applyEvent(task: SpawnedTask, event: RuntimeEvent): SpawnedTask {
  if (event.kind === 'usage') {
    const context = nextContext(task.context, event);
    return context === task.context ? task : { ...task, context };
  }
  const blocks = reduceEvent(task.blocks, event);
  if (event.kind === 'turn.started') {
    return { ...task, blocks, isBusy: true, isLive: true, workStartedAt: event.at || Date.now() };
  }
  const finished = event.kind === 'turn.completed' || event.kind === 'turn.failed';
  if (!finished) return blocks === task.blocks ? task : { ...task, blocks };
  return {
    ...task,
    blocks,
    isBusy: false,
    lastTurnMs: task.workStartedAt ? Math.max(0, (event.at || Date.now()) - task.workStartedAt) : task.lastTurnMs,
    workStartedAt: undefined,
  };
}

/** Applies a batch to a list of tasks, touching only the threads it names. */
function applyBatch(
  list: SpawnedTask[],
  byThread: Map<string, RuntimeEvent[]>,
  onFinished?: (task: SpawnedTask) => void,
): SpawnedTask[] {
  let changed = false;
  const next = list.map((task) => {
    const events = byThread.get(task.threadId);
    if (!events) return task;
    changed = true;
    let updated = task;
    for (const event of events) {
      updated = applyEvent(updated, event);
      if (onFinished && (event.kind === 'turn.completed' || event.kind === 'turn.failed')) {
        onFinished(updated);
      }
    }
    return updated;
  });
  return changed ? next : list;
}

/** How long events for a not-yet-registered thread are held for it. */
const EARLY_EVENT_TTL_MS = 60_000;
const EARLY_EVENT_LIMIT = 500;

/** Stable key for a plan so the card can show launched status, not confirm. */
export function planKey(titles: string[]): string {
  return titles.join('\n');
}

/**
 * Owns the delegation half of the orchestrator: recognising a plan, asking
 * about isolation, spawning the agents, and keeping each spawned session's
 * transcript up to date.
 *
 * The Orchestrator backend is source of truth for launches: it executes
 * coordinator plans itself and announces them over orchestrator:spawned. This
 * hook adopts those into the deck; manual confirm remains only as an override
 * for launching the same plan with different models.
 */
function rootThreadId(threadId: string): string {
  const index = threadId.indexOf('-cont-');
  return index === -1 ? threadId : threadId.slice(0, index);
}

export function useSpawner(options?: UseSpawnerOptions): Spawner {
  const [plan, setPlan] = useState<PendingPlan | null>(null);
  const [tasks, setTasks] = useState<SpawnedTask[]>([]);
  const [backgroundTasks, setBackgroundTasks] = useState<SpawnedTask[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [workspaceId, setWorkspaceId] = useState<string | null>(null);
  const [canUseWorktree, setCanUseWorktree] = useState(false);
  const [launchedKeys, setLaunchedKeys] = useState<string[]>([]);
  const currentWorkspaceId = useRef<string | null>(null);
  const threads = useRef<Set<string>>(new Set());
  // Events for threads the UI has not registered yet. Spawning starts the
  // session and sends the first turn before the call returns, so its first
  // events can arrive before the card exists; they are replayed on adoption.
  const early = useRef<Map<string, { at: number; events: RuntimeEvent[] }>>(new Map());
  const tasksRef = useRef<SpawnedTask[]>([]);
  const backgroundRef = useRef<SpawnedTask[]>([]);
  const lastSentOptions = useRef<Record<string, string>>({});
  const onBackgroundCompleteRef = useRef(options?.onBackgroundComplete);
  onBackgroundCompleteRef.current = options?.onBackgroundComplete;
  tasksRef.current = tasks;
  backgroundRef.current = backgroundTasks;

  /** Registers a thread and folds in any events that arrived before it. */
  const withEarlyEvents = useCallback((task: SpawnedTask): SpawnedTask => {
    threads.current.add(task.threadId);
    const held = early.current.get(task.threadId);
    early.current.delete(task.threadId);
    return held ? held.events.reduce(applyEvent, task) : task;
  }, []);

  useEffect(() => {
    IsGitRepo('').then(setCanUseWorktree).catch(() => false);
  }, []);

  const retitle = useCallback((threadId: string, title: string) => {
    const root = rootThreadId(threadId);
    const apply = (previous: SpawnedTask[]) => {
      let changed = false;
      const next = previous.map((task) => {
        if (rootThreadId(task.threadId) !== root || task.title === title) return task;
        changed = true;
        return { ...task, title };
      });
      return changed ? next : previous;
    };
    setTasks(apply);
    setBackgroundTasks(apply);
  }, []);

  useEffect(
    () =>
      EventsOn('chat:title', (payload: { threadId?: string; title?: string }) => {
        if (payload?.threadId && payload.title) retitle(payload.threadId, payload.title);
      }),
    [retitle],
  );

  const renameChat = useCallback(
    async (threadId: string, title: string) => {
      const clean = title.replace(/\s+/g, ' ').trim();
      if (!clean || threadId.startsWith('draft-')) return;
      retitle(threadId, clean);
      try {
        await RenameChat(threadId, clean);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    },
    [retitle],
  );

  const inspect = useCallback(async (text: string) => {
    try {
      const parsed = await ParseTasks(text);
      if (!parsed || parsed.length === 0) return;

      // An empty path resolves to the active project on the backend, so the
      // worktree question is asked about the directory the agents will really
      // start in rather than about Composer's own folder.
      const isRepo = await IsGitRepo('').catch(() => false);
      setCanUseWorktree(isRepo);
      setPlan({ tasks: parsed, canUseWorktree: isRepo });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  useEffect(() => {
    const off = onRuntimeEvents((batch) => {
      const byThread = new Map<string, RuntimeEvent[]>();
      const now = Date.now();
      for (const event of batch) {
        if (!threads.current.has(event.threadId)) {
          if (!event.threadId || event.threadId === 'coordinator') continue;
          const held = early.current.get(event.threadId) ?? { at: now, events: [] };
          if (held.events.length < EARLY_EVENT_LIMIT) held.events.push(event);
          early.current.set(event.threadId, held);
          continue;
        }
        const list = byThread.get(event.threadId);
        if (list) list.push(event);
        else byThread.set(event.threadId, [event]);
      }
      for (const [threadId, held] of early.current) {
        if (now - held.at > EARLY_EVENT_TTL_MS) early.current.delete(threadId);
      }
      if (byThread.size === 0) return;

      setTasks((previous) => applyBatch(previous, byThread));
      setBackgroundTasks((previous) => {
        const finished: SpawnedTask[] = [];
        const next = applyBatch(previous, byThread, (task) => finished.push(task));
        for (const task of finished) {
          setTimeout(() => onBackgroundCompleteRef.current?.(task), 0);
        }
        return next;
      });
    });
    return off;
  }, []);

  const confirmTasks = useCallback(
    async (
      planTasks: { title: string; prompt: string; cwd?: string; action?: string; targetThreadId?: string }[],
      useWorktree: boolean,
      modelIds: string[] = [],
      project?: { name: string; path: string },
    ) => {
      if (!planTasks || planTasks.length === 0) return;

      // The Orchestrator backend is source of truth: if it already executed
      // this plan, confirming again would spawn duplicates. Just surface the
      // agents instead of relaunching.
      try {
        const latest = await OrchestratorLatest().catch(() => null);
        if (latest?.executed && latest.tasks?.length === planTasks.length) {
          const latestTitles = latest.tasks.map((t) => t.title);
          const same = planTasks.every((task, index) => task.title === latestTitles[index]);
          if (same) {
            setPlan(null);
            setLaunchedKeys((previous) =>
              previous.includes(planKey(latestTitles))
                ? previous
                : [...previous, planKey(latestTitles)],
            );
            return;
          }
        }
      } catch {
        // Fall through to manual launch when the check itself fails.
      }

      const store = useOrchestratorStore.getState();
      const fallback = store.getSelectedModel();
      if (!fallback) {
        setError('No agent CLI available');
        return;
      }

      setPlan(null);
      setError(null);

      try {
        const currentProj = project || (await ActiveProject().catch(() => null));
        const projCwd = currentProj?.path || planTasks.find((task) => task.cwd)?.cwd || '';
        if (!projCwd) {
          setError('Choose a project folder before starting an agent');
          return;
        }

        // Separate message routing tasks from new spawn tasks
        const messageTasks = planTasks.filter((t) => t.action === 'message' && t.targetThreadId);
        const spawnTasks = planTasks.filter((t) => t.action !== 'message' || !t.targetThreadId);

        // Execute message tasks directly to existing agents
        for (const msgTask of messageTasks) {
          const targetId = msgTask.targetThreadId!;
          await OrchestratorMessageAgent(targetId, msgTask.prompt);
          setTasks((previous) =>
            previous.map((t) =>
              t.threadId === targetId
                ? {
                    ...t,
                    isBusy: true,
                    blocks: [
                      ...t.blocks,
                      userBlock(msgTask.prompt, `user-${Date.now()}`, [], Date.now()),
                    ],
                  }
                : t,
            ),
          );
        }

        // Execute spawn tasks if any exist
        if (spawnTasks.length > 0) {
          const result = await SpawnTasks(
            spawnTasks.map((task) => {
              const originalIndex = planTasks.indexOf(task);
              const chosen = store.models.find((m) => m.id === modelIds[originalIndex]) ?? fallback;
              return session.SpawnRequest.createFrom({
                title: task.title,
                prompt: task.prompt,
                cwd: task.cwd || projCwd,
                driver: chosen.providerId,
                account: store.accountFor(chosen.providerId, projCwd),
                model: chosen.id,
                options: toModelOptions(chosen, store.getCurrentModelSettings(chosen.id)),
              });
            }),
            {
              driver: fallback.providerId,
              account: store.accountFor(fallback.providerId, projCwd),
              model: fallback.id,
              options: toModelOptions(fallback, store.getCurrentModelSettings(fallback.id)),
              cwd: projCwd,
              useWorktree,
              title: spawnTasks[0]?.title ?? 'Tasks',
              prompt: spawnTasks.map((task) => task.title).join(', '),
              workspaceId: currentWorkspaceId.current || '',
              permissionMode: store.autoApprovePermissions ? 'bypassPermissions' : 'default',
            },
          );

          if (result.workspace?.id) {
            currentWorkspaceId.current = result.workspace.id;
            setWorkspaceId(result.workspace.id);
          }

          for (const task of result.tasks) {
            const returned = store.models.find((m) => m.id === task.model);
            if (returned) {
              lastSentOptions.current[task.threadId] = JSON.stringify(
                toModelOptions(returned, store.getCurrentModelSettings(returned.id)),
              );
            }
          }
          const created = result.tasks.map((task, index) => {
            const initialPrompt = spawnTasks[index]?.prompt || task.prompt;
            return withEarlyEvents({
              threadId: task.threadId,
              cardKey: task.threadId,
              title: task.title,
              branch: task.worktree?.branch,
              model: task.model,
              driver: task.driver,
              account: task.account,
              blocks: initialPrompt
                ? [userBlock(initialPrompt, `orch-prompt-${task.threadId}`, [], task.createdAt || Date.now())]
                : ([] as AgentStreamBlock[]),
              isBusy: true,
              projectName: currentProj?.name,
              projectPath: currentProj?.path,
            });
          });
          setTasks((previous) => [...previous, ...created]);

          if (result.errors?.length) setError(result.errors.join('\n'));
        }
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    },
    [withEarlyEvents],
  );

  const confirm = useCallback(
    async (useWorktree: boolean, modelIds: string[] = []) => {
      if (!plan) return;
      await confirmTasks(plan.tasks, useWorktree, modelIds);
    },
    [plan, confirmTasks],
  );

  const dismiss = useCallback(() => setPlan(null), []);

  function describeSwitch(
    prevProviderName: string | undefined,
    prevModelName: string | undefined,
    nextProviderName: string,
    nextModelName: string,
  ): string {
    const from = `${prevProviderName || 'agent'}: ${prevModelName || 'previous model'}`;
    const to = `${nextProviderName}: ${nextModelName}`;
    return `Switched from ${from} to ${to}`;
  }

  /** Display name for a provider id, capitalised when the provider list is unavailable. */
  function providerDisplayName(driverId: string | undefined): string {
    if (!driverId) return 'agent';
    const known = useOrchestratorStore.getState().providers.find((p) => p.id === driverId)?.name;
    if (known) return known;
    return driverId.charAt(0).toUpperCase() + driverId.slice(1);
  }

  const sendWithModel = useCallback(
    async (
      threadId: string,
      text: string,
      files: domain.FileRef[] = [],
      modelId?: string,
    ): Promise<string | undefined> => {
      const trimmed = text.trim();
      if (!trimmed && files.length === 0) return undefined;

      const store = useOrchestratorStore.getState();
      const currentTask = tasksRef.current.find((t) => t.threadId === threadId);
      if (currentTask?.isDraft) return undefined;

      if (currentTask && currentTask.workspaceId && currentTask.isLive === false) {
        // Imported outside chats are read-only context: messaging one starts a
        // fresh agent in the same folder with the transcript attached, rather
        // than resuming a conversation the CLI never had.
        const importTarget = modelId
          ? (store.models.find((m) => m.id === modelId) ?? store.getSelectedModel())?.providerId
          : currentTask.driver;
        const continuesClaude =
          currentTask.importedFrom === 'claude-code' && (importTarget || 'claude') === 'claude';
        if (currentTask.importedFrom && !continuesClaude) {
          try {
            const store = useOrchestratorStore.getState();
            const desired = modelId
              ? store.models.find((m) => m.id === modelId) ?? store.getSelectedModel()
              : undefined;
            const promptText =
              files.length > 0
                ? `${trimmed}${files.map((f) => `\n@${f.path}`).join('')}`
                : trimmed;
            const result = await SpawnFromImported(
              currentTask.workspaceId,
              promptText,
              desired?.providerId || currentTask.driver || 'claude',
              desired?.id || currentTask.model || '',
              desired ? toModelOptions(desired, store.getCurrentModelSettings(desired.id)) : null,
            );
            const spawned = result.tasks?.[0];
            if (!spawned) throw new Error('spawn returned no tasks');
            if (desired) {
              lastSentOptions.current[spawned.threadId] = JSON.stringify(
                toModelOptions(desired, store.getCurrentModelSettings(desired.id)),
              );
            }
            threads.current.delete(currentTask.threadId);
            const stamp = Date.now();
            const prevDriver = currentTask.driver;
            const prevModelId = currentTask.model;
            const prevModelName =
              store.models.find((m) => m.id === prevModelId)?.name || prevModelId;
            const nextDriver = spawned.driver;
            const nextModel = spawned.model;
            const nextProvider = store.providers.find((p) => p.id === nextDriver);
            const nextProviderName = nextProvider?.name || providerDisplayName(nextDriver);
            const nextModelName = desired?.name || nextModel || 'model';
            const isDifferent =
              (prevDriver && prevDriver !== nextDriver) ||
              (prevModelId && prevModelId !== nextModel);
            const notice = isDifferent
              ? describeSwitch(
                  providerDisplayName(prevDriver),
                  prevModelName,
                  nextProviderName,
                  nextModelName,
                )
              : '';
            const icon = desired?.icon || nextProvider?.icon || '';

            const prevDrivers =
              (currentTask as any).drivers || (currentTask.driver ? [currentTask.driver] : []);
            const prevModels =
              (currentTask as any).models || (currentTask.model ? [currentTask.model] : []);
            const nextDrivers = Array.from(
              new Set([...prevDrivers, nextDriver].filter(Boolean)),
            );
            const nextModels = Array.from(
              new Set([...prevModels, nextModel].filter(Boolean)),
            );

            const newBlocks: AgentStreamBlock[] = [
              ...currentTask.blocks,
              ...(notice
                ? [
                    {
                      type: 'notice' as const,
                      id: `notice-${stamp}`,
                      label: notice,
                      icon: icon || undefined,
                    },
                  ]
                : []),
              userBlock(trimmed, `user-${stamp}`, files, stamp),
            ];

            const updated = withEarlyEvents({
              ...currentTask,
              threadId: spawned.threadId,
              title: currentTask.title,
              branch: (spawned as any).worktree?.branch ?? currentTask.branch,
              model: nextModel,
              driver: nextDriver,
              account: spawned.account,
              drivers: nextDrivers,
              models: nextModels,
              blocks: newBlocks,
              isBusy: true,
              isLive: true,
              importedFrom: undefined,
            });

            setTasks((prev) =>
              prev.map((t) => (t.threadId === currentTask.threadId ? updated : t)),
            );
            return spawned.threadId;
          } catch (cause) {
            setError(cause instanceof Error ? cause.message : String(cause));
            return undefined;
          }
        }
        try {
          // Only this chat's agent is started, not every task of its session.
          await ResumeHistoryThread(currentTask.workspaceId, threadId);
          threads.current.add(threadId);
          setTasks((prev) =>
            prev.map((t) =>
              t.workspaceId === currentTask.workspaceId
                ? { ...t, isLive: true, importedFrom: undefined }
                : t,
            ),
          );
        } catch (cause) {
          setError(cause instanceof Error ? cause.message : String(cause));
          return undefined;
        }
      }

      const desired = modelId
        ? store.models.find((m) => m.id === modelId) ?? store.getSelectedModel()
        : undefined;
      const prevDriver = currentTask?.driver;
      const prevModelId = currentTask?.model;
      const prevModelName =
        store.models.find((m) => m.id === prevModelId)?.name || prevModelId;

      const providerChanges = Boolean(desired && desired.providerId !== (prevDriver || desired.providerId));
      const pendingAccount = desired ? store.pendingAccount(threadId, desired.providerId) : '';
      const wantedAccount =
        desired && providerChanges ? pendingAccount || store.shownAccount(desired.providerId) : pendingAccount;
      const accountChanges =
        Boolean(wantedAccount) && !providerChanges && wantedAccount !== (currentTask?.account || 'default');

      if (desired && currentTask && (providerChanges || accountChanges)) {
        // Cross-provider switch reuses the same thread: the card, its size,
        // its transcript and its history all stay one continuous conversation.
        // The backend folds the handoff plus this message into a single turn.
        const options = toModelOptions(desired, store.getCurrentModelSettings(desired.id));
        const prevProvider = store.providers.find((p) => p.id === prevDriver);
        const nextProvider =
          store.providers.find((p) => p.id === desired.providerId);
        const withAccount = (name: string, provider: typeof nextProvider, id: string | undefined) => {
          const accounts = provider?.accounts ?? [];
          if (accounts.length < 2) return name;
          const label = accounts.find((account) => account.id === (id || 'default'))?.name || id || 'default';
          return `${name} (${label})`;
        };
        const prevProviderName = withAccount(providerDisplayName(prevDriver), prevProvider, currentTask.account);
        const nextProviderName = withAccount(
          nextProvider?.name || providerDisplayName(desired.providerId),
          nextProvider,
          wantedAccount,
        );
        const notice = describeSwitch(prevProviderName, prevModelName, nextProviderName, desired.name);
        const icon = desired.icon || nextProvider?.icon || '';
        const stamp = Date.now();
        setTasks((previous) =>
          previous.map((task) =>
            task.threadId === threadId
              ? {
                  ...task,
                  isBusy: true,
                  isLive: true,
                  blocks: [
                    ...task.blocks,
                    {
                      type: 'notice' as const,
                      id: `notice-${stamp}`,
                      label: notice,
                      icon: icon || undefined,
                    },
                    userBlock(trimmed, `user-${stamp}`, files, stamp),
                  ],
                }
              : task,
          ),
        );
        try {
          const switched = await SwitchTaskAccountWithOptions(
            threadId,
            desired.providerId,
            wantedAccount,
            desired.id,
            options,
            notice,
            icon,
            trimmed,
            files,
          );
          lastSentOptions.current[threadId] = JSON.stringify(options);
          store.clearPendingAccount(threadId);
          setTasks((previous) =>
            previous.map((task) => {
              if (task.threadId !== threadId) return task;
              const prevDrivers = (task as any).drivers || (task.driver ? [task.driver] : []);
              const prevModels = (task as any).models || (task.model ? [task.model] : []);
              const nextDrivers = Array.from(new Set([...prevDrivers, switched.driver].filter(Boolean)));
              const nextModels = Array.from(new Set([...prevModels, switched.model].filter(Boolean)));
              return {
                ...task,
                model: switched.model,
                driver: switched.driver,
                account: switched.account,
                drivers: nextDrivers,
                models: nextModels,
              };
            }),
          );
          return threadId;
        } catch (cause) {
          setTasks((previous) =>
            previous.map((task) => (task.threadId === threadId ? { ...task, isBusy: false } : task)),
          );
          setError(cause instanceof Error ? cause.message : String(cause));
          return undefined;
        }
      }

      if (desired && currentTask && desired.id !== prevModelId) {
        const options = toModelOptions(desired, store.getCurrentModelSettings(desired.id));
        const providerName = providerDisplayName(desired.providerId);
        const notice = prevModelId ? describeSwitch(providerName, prevModelName, providerName, desired.name) : '';
        const icon = desired.icon || '';
        const stamp = Date.now();
        setTasks((previous) =>
          previous.map((task) =>
            task.threadId === threadId
              ? {
                  ...task,
                  isBusy: true,
                  isLive: true,
                  blocks: [
                    ...task.blocks,
                    ...(notice
                      ? [
                          {
                            type: 'notice' as const,
                            id: `notice-${stamp}`,
                            label: notice,
                            icon: icon || undefined,
                          },
                        ]
                      : []),
                    userBlock(trimmed, `user-${stamp}`, files, stamp),
                  ],
                }
              : task,
          ),
        );
        try {
          const updated = await UpdateTaskModel(
            threadId,
            desired.providerId,
            desired.id,
            options,
            notice,
            icon,
          );
          lastSentOptions.current[threadId] = JSON.stringify(options);
          setTasks((previous) =>
            previous.map((task) =>
              task.threadId === threadId
                ? { ...task, model: updated.model, driver: updated.driver }
                : task,
            ),
          );
        } catch (cause) {
          setTasks((previous) =>
            previous.map((task) => (task.threadId === threadId ? { ...task, isBusy: false } : task)),
          );
          setError(cause instanceof Error ? cause.message : String(cause));
          return undefined;
        }
      } else if (desired && currentTask) {
        const options = toModelOptions(desired, store.getCurrentModelSettings(desired.id));
        const key = JSON.stringify(options);
        if (lastSentOptions.current[threadId] && lastSentOptions.current[threadId] !== key) {
          const providerName = providerDisplayName(desired.providerId);
          const notice = `Effort changed for ${providerName}: ${desired.name}`;
          const icon = desired.icon || '';
          const stamp = Date.now();
          setTasks((previous) =>
            previous.map((task) =>
              task.threadId === threadId
                ? {
                    ...task,
                    isBusy: true,
                    isLive: true,
                    blocks: [
                      ...task.blocks,
                      {
                        type: 'notice' as const,
                        id: `notice-${stamp}`,
                        label: notice,
                        icon: icon || undefined,
                      },
                      userBlock(trimmed, `user-${stamp}`, files, stamp),
                    ],
                  }
                : task,
            ),
          );
          try {
            await UpdateTaskModel(threadId, desired.providerId, desired.id, options, notice, icon);
            lastSentOptions.current[threadId] = key;
          } catch (cause) {
            setTasks((previous) =>
              previous.map((task) =>
                task.threadId === threadId ? { ...task, isBusy: false } : task,
              ),
            );
            setError(cause instanceof Error ? cause.message : String(cause));
            return undefined;
          }
        } else {
          lastSentOptions.current[threadId] = key;
          const turnId = `${threadId}-turn-${Date.now()}`;
          setTasks((previous) =>
            previous.map((task) =>
              task.threadId === threadId
                ? {
                    ...task,
                    isBusy: true,
                    isLive: true,
                    blocks: [...task.blocks, userBlock(trimmed, `user-${Date.now()}`, files, Date.now())],
                  }
                : task,
            ),
          );
          try {
            await SendTurn(
              domain.SendTurnInput.createFrom({ threadId, turnId, text: trimmed, files }),
            );
          } catch (cause) {
            setTasks((previous) =>
              previous.map((task) =>
                task.threadId === threadId ? { ...task, isBusy: false } : task,
              ),
            );
            setError(cause instanceof Error ? cause.message : String(cause));
          }
          return threadId;
        }
      } else {
        const turnId = `${threadId}-turn-${Date.now()}`;
        setTasks((previous) =>
          previous.map((task) =>
            task.threadId === threadId
              ? {
                  ...task,
                  isBusy: true,
                  isLive: true,
                  blocks: [...task.blocks, userBlock(trimmed, `user-${Date.now()}`, files, Date.now())],
                }
              : task,
          ),
        );
        try {
          await SendTurn(
            domain.SendTurnInput.createFrom({ threadId, turnId, text: trimmed, files }),
          );
        } catch (cause) {
          setTasks((previous) =>
            previous.map((task) => (task.threadId === threadId ? { ...task, isBusy: false } : task)),
          );
          setError(cause instanceof Error ? cause.message : String(cause));
        }
        return threadId;
      }

      const turnId = `${threadId}-turn-${Date.now()}`;
      try {
        await SendTurn(
          domain.SendTurnInput.createFrom({ threadId, turnId, text: trimmed, files }),
        );
      } catch (cause) {
        setTasks((previous) =>
          previous.map((task) => (task.threadId === threadId ? { ...task, isBusy: false } : task)),
        );
        setError(cause instanceof Error ? cause.message : String(cause));
      }
      return threadId;
    },
    [],
  );

  const send = useCallback(
    async (threadId: string, text: string, files: domain.FileRef[] = []) => {
      await sendWithModel(threadId, text, files, undefined);
    },
    [sendWithModel],
  );

  const interrupt = useCallback(async (threadId: string) => {
    try {
      await InterruptTurn(threadId);
    } catch {
      // The turn may already have finished; the state update below still applies.
    }
    setTasks((previous) =>
      previous.map((task) => (task.threadId === threadId ? { ...task, isBusy: false } : task)),
    );
  }, []);

  const openDraft = useCallback((): string => {
    const id = `draft-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
    setError(null);
    setTasks((previous) => {
      if (previous.some((t) => t.threadId === id)) return previous;
      return [
        ...previous,
        {
          threadId: id,
          cardKey: id,
          title: 'New chat',
          blocks: [] as AgentStreamBlock[],
          isBusy: false,
          isDraft: true,
        },
      ];
    });
    return id;
  }, []);

  const close = useCallback(async (threadId: string) => {
    threads.current.delete(threadId);
    const isDraft = threadId.startsWith('draft-');
    setTasks((previous) => previous.filter((t) => t.threadId !== threadId));
    setBackgroundTasks((previous) => previous.filter((t) => t.threadId !== threadId));
    if (isDraft) return;
    try {
      await StopSession(threadId);
    } catch {
      // Closing the window is the user's intent regardless of teardown result.
    }
  }, []);

  const terminate = useCallback(async (threadId: string) => {
    await close(threadId);
  }, [close]);

  const spawnAgent = useCallback(
    async (
      prompt: string,
      title?: string,
      modelId?: string,
      project?: { name: string; path: string },
      files: domain.FileRef[] = [],
      replaceDraftId?: string,
    ) => {
      const trimmed = prompt.trim();
      if (!trimmed && files.length === 0) return;

      const store = useOrchestratorStore.getState();
      const chosen = store.models.find((m) => m.id === modelId) ?? store.getSelectedModel();
      if (!chosen) {
        setError('No agent CLI available');
        return;
      }

      setError(null);
      const taskTitle = title || (trimmed.length > 28 ? `${trimmed.slice(0, 28)}…` : trimmed) || 'New chat';

      try {
        const currentProj = project || (await ActiveProject().catch(() => null));
        const projCwd = currentProj?.path || '';
        if (!projCwd) {
          setError('Choose a project folder before starting an agent');
          return;
        }
        const result = await SpawnTasks(
          [
            session.SpawnRequest.createFrom({
              title: taskTitle,
              prompt: trimmed,
              cwd: projCwd,
              driver: chosen.providerId,
              account: store.accountFor(chosen.providerId, projCwd),
              model: chosen.id,
              options: toModelOptions(chosen, store.getCurrentModelSettings(chosen.id)),
              files,
            }),
          ],
          {
            driver: chosen.providerId,
            account: store.accountFor(chosen.providerId, projCwd),
            model: chosen.id,
            options: toModelOptions(chosen, store.getCurrentModelSettings(chosen.id)),
            cwd: projCwd,
            useWorktree: false,
            title: taskTitle,
            prompt: trimmed,
            workspaceId: '',
            permissionMode: store.autoApprovePermissions ? 'bypassPermissions' : 'default',
          },
        );

        if (result.workspace?.id) {
          currentWorkspaceId.current = null;
          setWorkspaceId(result.workspace.id);
        }

        lastSentOptions.current[result.tasks[0].threadId] = JSON.stringify(
          toModelOptions(chosen, store.getCurrentModelSettings(chosen.id)),
        );

        const created = result.tasks.map((task, index) =>
          withEarlyEvents({
            threadId: task.threadId,
            cardKey: replaceDraftId && index === 0 ? replaceDraftId : task.threadId,
            title: task.title,
            branch: task.worktree?.branch,
            model: task.model,
            driver: task.driver,
            account: task.account,
            blocks: [userBlock(trimmed, `orch-prompt-${task.threadId}`, files, task.createdAt || Date.now())],
            isBusy: true,
            projectName: currentProj?.name,
            projectPath: currentProj?.path,
          }),
        );
        if (replaceDraftId) {
          setTasks((previous) => {
            const idx = previous.findIndex((t) => t.threadId === replaceDraftId);
            if (idx === -1) return [...previous, ...created];
            const next = [...previous];
            next.splice(idx, 1, ...created);
            return next;
          });
        } else {
          setTasks((previous) => [...previous, ...created]);
        }

        return result.tasks[0]?.threadId;
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    },
    [withEarlyEvents],
  );

  const spawnSkillTest = useCallback(
    async (
      prompt: string,
      files: domain.FileRef[],
      skills: string[],
      modelId?: string,
      project?: { name: string; path: string },
    ) => {
      const trimmed = prompt.trim();
      if (!trimmed && files.length === 0) return;
      const store = useOrchestratorStore.getState();
      const chosen = store.models.find((m) => m.id === modelId) ?? store.getSelectedModel();
      if (!chosen) {
        setError('No agent CLI available');
        return;
      }
      if (!['claude', 'opencode', 'antigravity'].includes(chosen.providerId)) {
        setError('Skill tests run on Claude, OpenCode or Antigravity');
        return;
      }
      setError(null);
      try {
        const currentProj = project || (await ActiveProject().catch(() => null));
        const projCwd = currentProj?.path || '';
        if (!projCwd) {
          setError('Choose a project folder before starting a skill test');
          return;
        }
        const options = toModelOptions(chosen, store.getCurrentModelSettings(chosen.id));
        const result = await StartSkillTest(main.SkillTestInput.createFrom({
          driver: chosen.providerId,
          prompt: trimmed,
          files,
          skills,
          cwd: projCwd,
          account: store.accountFor(chosen.providerId, projCwd),
          model: chosen.id,
          options,
          permissionMode: store.autoApprovePermissions ? 'bypassPermissions' : 'default',
        }));
        if (result.workspace?.id) {
          currentWorkspaceId.current = null;
          setWorkspaceId(result.workspace.id);
        }
        const created = result.tasks.map((task) => {
          lastSentOptions.current[task.threadId] = JSON.stringify(options);
          return withEarlyEvents({
            threadId: task.threadId,
            cardKey: task.threadId,
            title: task.title,
            branch: task.worktree?.branch,
            model: task.model,
            driver: task.driver,
            account: task.account,
            blocks: [userBlock(trimmed, `orch-prompt-${task.threadId}`, files, task.createdAt || Date.now())],
            isBusy: true,
            projectName: currentProj?.name,
            projectPath: currentProj?.path,
          });
        });
        setTasks((previous) => [...previous, ...created]);
        if (result.errors?.length) setError(result.errors.join(' · '));
        return result.tasks[0]?.threadId;
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    },
    [withEarlyEvents],
  );

  const openHistorySession = useCallback(
    async (targetWorkspaceId: string, targetThreadId?: string): Promise<number> => {
      try {
        setError(null);
        // 1. If targetThreadId is specified, check if that specific task is already in deck
        const rootTargetId = targetThreadId?.includes('-cont-')
          ? targetThreadId.split('-cont-')[0]
          : targetThreadId;

        const tasks = tasksRef.current;
        const backgroundTasks = backgroundRef.current;
        if (rootTargetId) {
          const existingIdx = tasks.findIndex(
            (t) => t.threadId === rootTargetId || t.threadId === targetThreadId,
          );
          if (existingIdx !== -1) {
            return existingIdx;
          }

          // Check if this task is currently running in backgroundTasks
          const bgIdx = backgroundTasks.findIndex(
            (t) => t.threadId === rootTargetId || t.threadId === targetThreadId,
          );
          if (bgIdx !== -1) {
            const bgTask = backgroundTasks[bgIdx];
            setBackgroundTasks((prev) => prev.filter((_, i) => i !== bgIdx));
            setTasks((prev) => [...prev, bgTask]);
            return tasks.length;
          }
        }

        const loaded = await LoadHistoryTail(targetWorkspaceId, 1500000);
        const meta = loaded.meta;
        const rawTranscripts = (loaded.transcripts ?? {}) as Record<string, RuntimeEvent[]>;
        const truncated = (loaded.truncated ?? {}) as Record<string, number>;
        let rawTasks = meta.tasks ? [...meta.tasks] : [];
        const cwd = meta.workspace?.cwd || '';
        const folderName = cwd ? cwd.replace(/\\/g, '/').split('/').filter(Boolean).pop() : undefined;

        // If session had no subagents but had coordinator blocks:
        if (rawTasks.length === 0 && (meta.coordinatorThreadId || meta.workspace?.prompt)) {
          const threadId = meta.coordinatorThreadId || `coordinator-${targetWorkspaceId}`;
          const coordBlocks = replay(rawTranscripts[threadId]);
          const firstNonNotice = coordBlocks.find((b) => b.type !== 'notice');
          const prompt = meta.workspace?.prompt;
          const blocks =
            firstNonNotice?.type !== 'user' && prompt
              ? [
                  userBlock(
                    prompt,
                    `orch-prompt-${threadId}`,
                    [],
                    meta.workspace?.createdAt || Date.now(),
                  ),
                  ...coordBlocks,
                ]
              : coordBlocks;
          if (blocks.length > 0) {
            rawTasks.push({
              threadId,
              title: meta.workspace?.title || 'Session',
              prompt: meta.workspace?.prompt,
              blocks,
            } as any);
          }
        }

        if (rawTasks.length === 0) {
          return -1;
        }

        // Normalize continuation tasks so mid-chat switches are 1 unified conversation
        const { tasks: loadedTasks, transcripts } = normalizeSessionTasks(rawTasks, rawTranscripts);

        // Target ONLY the single requested task (or primary/first task) - NEVER open 4 or 5 at once
        const t = rootTargetId
          ? loadedTasks.find((item) => item.threadId === rootTargetId) || loadedTasks[0]
          : loadedTasks[0];

        if (!t) {
          return -1;
        }

        const alreadyIdx = tasks.findIndex((x) => x.threadId === t.threadId);
        if (alreadyIdx !== -1) {
          return alreadyIdx;
        }

        threads.current.add(t.threadId);
        const blocks = threadBlocks(meta, rawTranscripts, t.threadId);

        const threadEvents = (transcripts[t.threadId] ?? []) as RuntimeEvent[];
        const recorded = [...threadEvents].reverse().find((event) => event.model);
        const taskObj: SpawnedTask = {
          threadId: t.threadId,
          cardKey: t.threadId,
          title: t.title || 'Agent',
          branch: t.worktree?.branch,
          model: t.model || recorded?.model,
          driver: (t as any).driver || recorded?.driver,
          account: t.account,
          blocks,
          lastTurnMs: lastTurnDuration(threadEvents),
          context: latestContext(threadEvents),
          isBusy: false,
          workspaceId: targetWorkspaceId,
          isLive: false,
          hasEarlier: Boolean(truncated[t.threadId]),
          importedFrom: (meta.workspace as any)?.importedFrom,
          projectName: folderName,
          projectPath: cwd,
        };

        const storedOptions = (t as any).options;
        if (storedOptions) {
          lastSentOptions.current[t.threadId] = JSON.stringify(storedOptions);
        }

        const newIndex = tasks.length;
        setTasks((prev) => [...prev, taskObj]);
        return newIndex;
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
        return -1;
      }
    },
    [],
  );

  const loadEarlier = useCallback(async (threadId: string) => {
    const task = tasksRef.current.find((item) => item.threadId === threadId);
    if (!task || !task.hasEarlier || task.loadingEarlier || !task.workspaceId) return;
    const workspaceId = task.workspaceId;
    setTasks((previous) =>
      previous.map((item) => (item.threadId === threadId ? { ...item, loadingEarlier: true } : item)),
    );
    try {
      const loaded = await LoadHistory(workspaceId);
      const rawTranscripts = (loaded.transcripts ?? {}) as Record<string, RuntimeEvent[]>;
      const blocks = threadBlocks(loaded.meta, rawTranscripts, threadId);
      setTasks((previous) =>
        previous.map((item) =>
          item.threadId === threadId ? { ...item, blocks, hasEarlier: false, loadingEarlier: false } : item,
        ),
      );
    } catch (cause) {
      setTasks((previous) =>
        previous.map((item) => (item.threadId === threadId ? { ...item, hasEarlier: false, loadingEarlier: false } : item)),
      );
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  const deleteTurn = useCallback(async (threadId: string, turnId: string) => {
    try {
      const events = ((await DeleteTurn(threadId, turnId)) ?? []) as RuntimeEvent[];
      const replayed = replay(events);
      setTasks((previous) =>
        previous.map((task) => {
          if (task.threadId !== threadId) return task;
          const head = task.blocks[0];
          const keepHead =
            head?.type === 'user' && head.id.startsWith('orch-prompt-') && replayed.find((b) => b.type !== 'notice')?.type !== 'user';
          return {
            ...task,
            blocks: keepHead ? [head, ...replayed] : replayed,
            isBusy: false,
            context: latestContext(events),
            lastTurnMs: lastTurnDuration(events),
          };
        }),
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  const moveChat = useCallback(async (payload: ChatDragPayload, project: { name: string; path: string }) => {
    try {
      await MoveChat(payload.workspaceId, project.path);
      setTasks((previous) =>
        previous.map((task) =>
          task.threadId === payload.threadId || (payload.workspaceId && task.workspaceId === payload.workspaceId)
            ? { ...task, projectName: project.name, projectPath: project.path }
            : task,
        ),
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  const clear = useCallback(() => {
    for (const id of threads.current) {
      StopSession(id).catch(() => {});
    }
    threads.current = new Set();
    setTasks([]);
    setBackgroundTasks([]);
    setPlan(null);
    setError(null);
    setLaunchedKeys([]);
    currentWorkspaceId.current = null;
    setWorkspaceId(null);
  }, []);

  const allActiveTasks = useMemo(() => {
    return [...tasks, ...backgroundTasks];
  }, [tasks, backgroundTasks]);

  /**
   * Adopts agents The Orchestrator launched backend-side into the deck.
   * Announced over orchestrator:spawned; same card mapping as a manual
   * confirm, minus the SpawnTasks round-trip. Also flips the matching plan
   * card from confirm gate to launched status and clears a matching pending
   * plan so it cannot be confirmed into duplicates.
   */
  const adoptSpawned = useCallback((result: session.SpawnResult) => {
    if (!result?.tasks || result.tasks.length === 0) return;
    if (result.workspace?.id) {
      currentWorkspaceId.current = result.workspace.id;
      setWorkspaceId(result.workspace.id);
    }
    const key = planKey(result.tasks.map((task) => task.title));
    setLaunchedKeys((previous) => (previous.includes(key) ? previous : [...previous, key]));
    setPlan((previous) => {
      if (!previous) return previous;
      return planKey(previous.tasks.map((task) => task.title)) === key ? null : previous;
    });
    if (result.errors?.length) setError(result.errors.join('\n'));
    for (const task of result.tasks) {
      const storedOptions = (task as any).options;
      if (storedOptions) {
        lastSentOptions.current[task.threadId] = JSON.stringify(storedOptions);
      }
    }
    setTasks((previous) => {
      const known = new Set(previous.map((task) => task.threadId));
      const fresh = result.tasks.filter((task) => !known.has(task.threadId));
      if (fresh.length === 0) return previous;
      return [
        ...previous,
        ...fresh.map((task) =>
          withEarlyEvents({
            threadId: task.threadId,
            cardKey: task.threadId,
            title: task.title,
            branch: task.worktree?.branch,
            model: task.model,
            driver: task.driver,
            account: task.account,
            blocks: task.prompt
              ? [userBlock(task.prompt, `orch-prompt-${task.threadId}`, [], task.createdAt || Date.now())]
              : ([] as AgentStreamBlock[]),
            isBusy: true,
            workspaceId: result.workspace?.id,
            isLive: true,
            projectName: undefined,
            projectPath: result.workspace?.cwd,
          }),
        ),
      ];
    });
  }, [withEarlyEvents]);

  return useMemo(
    () => ({
      plan,
      tasks,
      backgroundTasks,
      allActiveTasks,
      error,
      workspaceId,
      canUseWorktree,
      launchedKeys,
      inspect,
      confirm,
      confirmTasks,
      adoptSpawned,
      dismiss,
      send,
      sendWithModel,
      interrupt,
      close,
      terminate,
      spawnAgent,
      openDraft,
      spawnSkillTest,
      openHistorySession,
      loadEarlier,
      clear,
      deleteTurn,
      moveChat,
      renameChat,
    }),
    [
      plan, tasks, backgroundTasks, allActiveTasks, error, workspaceId, canUseWorktree, launchedKeys,
      inspect, confirm, confirmTasks, adoptSpawned, dismiss, send, sendWithModel, interrupt, close,
      terminate, spawnAgent, openDraft, spawnSkillTest, openHistorySession, loadEarlier, clear, deleteTurn, moveChat, renameChat,
    ],
  );
}
