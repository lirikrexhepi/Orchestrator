import { useCallback, useEffect, useRef, useState } from 'react';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import { onRuntimeEvents } from '../agent-session/runtimeEvents';
import {
  SetChatArchived,
  ListHistory,
  LoadHistory,
  NewChat,
  ProjectSwitched,
  ResumeHistory,
} from '../../../wailsjs/go/main/App';
import { history, session } from '../../../wailsjs/go/models';
import { AgentStreamBlock } from '../agent-session';
import { reduceEvent, RuntimeEvent, userBlock } from '../agent-session/eventReducer';

/** One agent's restored transcript, ready to render in a window. */
export interface RestoredTask {
  threadId: string;
  title: string;
  model?: string;
  branch?: string;
  state: string;
  blocks: AgentStreamBlock[];
  /** Live once resumed; until then the window is a read-only replay. */
  isLive: boolean;
  /** Set when a resume attempt could not continue the original conversation. */
  note?: string;
}

/**
 * A past session, reopened.
 *
 * The orchestrator transcript is carried alongside the agents rather than
 * separately: a Composer session is one request and everything it produced, and
 * showing the agents without the conversation that created them loses the part
 * that explains why they exist.
 */
export interface RestoredSession {
  workspaceId: string;
  title: string;
  cwd: string;
  createdAt: number;
  coordinatorBlocks: AgentStreamBlock[];
  tasks: RestoredTask[];
}

export interface HistoryState {
  entries: history.Meta[];
  isLoading: boolean;
  error: string | null;
  restored: RestoredSession | null;
  isResuming: boolean;
  refresh: () => Promise<void>;
  open: (workspaceId: string) => Promise<void>;
  resume: (workspaceId: string) => Promise<void>;
  remove: (workspaceId: string, threadId?: string) => Promise<void>;
  setArchived: (workspaceId: string, threadId: string | undefined, archived: boolean) => Promise<void>;
  /** Ends every running agent and starts a fresh orchestrator conversation. */
  newChat: () => Promise<void>;
  /** Resets the coordinator conversation for the new project, keeping agents alive. */
  projectSwitched: () => Promise<void>;
  close: () => void;
}


/** Replays stored events through the same reducer the live feed uses. */
function replay(events: RuntimeEvent[] | undefined): AgentStreamBlock[] {
  if (!events?.length) return [];
  return events.reduce<AgentStreamBlock[]>((blocks, event) => reduceEvent(blocks, event), []);
}

function toRestored(loaded: history.Session): RestoredSession {
  const meta = loaded.meta;
  const transcripts = (loaded.transcripts ?? {}) as Record<string, RuntimeEvent[]>;

  const coordinatorBlocks = (() => {
    const rawEvents = meta.coordinatorThreadId ? transcripts[meta.coordinatorThreadId] : undefined;
    const coordBlocks = replay(rawEvents);
    const firstNonNotice = coordBlocks.find((b) => b.type !== 'notice');
    const prompt = meta.workspace?.prompt;
    if (firstNonNotice?.type !== 'user' && prompt) {
      return [
        userBlock(
          prompt,
          `orch-prompt-${meta.coordinatorThreadId || 'coordinator'}`,
          [],
          meta.workspace?.createdAt || Date.now(),
        ),
        ...coordBlocks,
      ];
    }
    return coordBlocks;
  })();

  return {
    workspaceId: meta.workspace?.id ?? '',
    title: meta.workspace?.title || 'Session',
    cwd: meta.workspace?.cwd ?? '',
    createdAt: meta.workspace?.createdAt ?? 0,
    coordinatorBlocks,
    tasks: (meta.tasks ?? []).map((task) => {
      const replayed = replay(transcripts[task.threadId]);
      const firstNonNotice = replayed.find((b) => b.type !== 'notice');
      const prompt = task.prompt || meta.workspace?.prompt;
      const blocks =
        firstNonNotice?.type !== 'user' && prompt
          ? [
              userBlock(
                prompt,
                `orch-prompt-${task.threadId}`,
                [],
                task.createdAt || meta.workspace?.createdAt || Date.now(),
              ),
              ...replayed,
            ]
          : replayed;

      return {
        threadId: task.threadId,
        title: task.title,
        model: task.model,
        branch: task.worktree?.branch,
        state: task.state,
        blocks,
        isLive: false,
      };
    }),
  };
}

/**
 * Owns the history panel and the session it reopens.
 *
 * Listing is deliberately separate from loading: the panel only needs titles and
 * timestamps, and reading every transcript to draw a list would mean parsing
 * every event ever recorded.
 */
/**
 * @param onCleared runs after a new chat is started, so the caller can drop the
 *   windows and transcripts belonging to the session that just ended.
 */
export function useHistory(isOpen: boolean, onCleared?: () => void): HistoryState {
  const [entries, setEntries] = useState<history.Meta[]>([]);
  const [isLoading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [restored, setRestored] = useState<RestoredSession | null>(null);
  const [isResuming, setResuming] = useState(false);
  // Threads brought back to life. Only these accept live events: a replayed
  // transcript must not start moving because an unrelated agent is running.
  const liveThreads = useRef<Set<string>>(new Set());

  useEffect(() => {
    const off = onRuntimeEvents((batch) => {
      const events = batch.filter((event) => liveThreads.current.has(event.threadId));
      if (events.length === 0) return;

      setRestored((previous) => {
        if (!previous) return previous;
        return {
          ...previous,
          tasks: previous.tasks.map((task) => {
            const mine = events.filter((event) => event.threadId === task.threadId);
            return mine.length > 0 ? { ...task, blocks: mine.reduce(reduceEvent, task.blocks) } : task;
          }),
        };
      });
    });
    return off;
  }, []);

  const refresh = useCallback(async () => {
    try {
      setEntries((await ListHistory()) ?? []);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  useEffect(() => {
    if (!isOpen) return;
    setLoading(true);
    void refresh().finally(() => setLoading(false));
  }, [isOpen, refresh]);

  // Live reactivity: update history list immediately when sessions are created,
  // agents are launched, or tasks are modified, without requiring an app restart.
  useEffect(() => {
    const offHistory = EventsOn('history:changed', () => {
      void refresh();
    });
    const offSpawned = EventsOn('orchestrator:spawned', () => {
      void refresh();
    });
    return () => {
      offHistory();
      offSpawned();
    };
  }, [refresh]);

  const open = useCallback(async (workspaceId: string) => {
    setLoading(true);
    try {
      const loaded = toRestored(await LoadHistory(workspaceId));
      // Opening a different session must not leave the previous one's threads
      // marked live, or its events would land in the newly opened windows.
      liveThreads.current = new Set();
      setRestored(loaded);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  const resume = useCallback(
    async (workspaceId: string) => {
      setResuming(true);
      try {
        const result: session.ResumeResult = await ResumeHistory(workspaceId);
        const outcomes = new Map(
          (result.outcomes ?? []).map((outcome) => [outcome.threadId, outcome]),
        );

        for (const outcome of result.outcomes ?? []) {
          if (outcome.live) liveThreads.current.add(outcome.threadId);
        }

        // A resumed agent and one that merely restarted in the same directory
        // look identical until it answers, so the distinction is carried
        // through to the window rather than flattened into "live".
        setRestored((previous) => {
          if (!previous) return previous;
          return {
            ...previous,
            tasks: previous.tasks.map((task) => {
              const outcome = outcomes.get(task.threadId);
              if (!outcome) return task;
              return {
                ...task,
                isLive: outcome.live,
                note: outcome.continued ? undefined : outcome.reason,
              };
            }),
          };
        });
        setError(null);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        setResuming(false);
      }
    },
    [],
  );

  const setArchived = useCallback(
    async (workspaceId: string, threadId: string | undefined, archived: boolean) => {
      try {
        await SetChatArchived(workspaceId, threadId ?? '', archived);
        await refresh();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    },
    [refresh],
  );

  const remove = useCallback(
    (workspaceId: string, threadId?: string) => setArchived(workspaceId, threadId, true),
    [setArchived],
  );

  const close = useCallback(() => {
    liveThreads.current = new Set();
    setRestored(null);
  }, []);

  const newChat = useCallback(async () => {
    try {
      await NewChat();
      liveThreads.current = new Set();
      setRestored(null);
      onCleared?.();
      // The session that just ended becomes a history entry, so the list is
      // re-read rather than left showing the state from before it closed.
      await refresh();
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, [onCleared, refresh]);

  const projectSwitched = useCallback(async () => {
    try {
      await ProjectSwitched();
      liveThreads.current = new Set();
      setRestored(null);
      await refresh();
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, [refresh]);

  return {
    entries,
    isLoading,
    error,
    restored,
    isResuming,
    refresh,
    open,
    resume,
    remove,
    setArchived,
    newChat,
    projectSwitched,
    close,
  };
}
