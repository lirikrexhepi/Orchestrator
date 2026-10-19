import { useCallback, useEffect, useRef, useState } from 'react';
import { ProjectTree, ProjectTreeStatus } from '../../../wailsjs/go/main/App';
import { files } from '../../../wailsjs/go/models';
import { onRuntimeEvents } from '../agent-session/runtimeEvents';

const SETTLE_MS = 1_500;
const IDLE_REFRESH_MS = 10_000;

export interface ProjectTreeState {
  /** Children per folder path ('' is the root), loaded as folders open. */
  children: Record<string, files.Entry[]>;
  expanded: ReadonlySet<string>;
  status: files.TreeStatus | null;
  loading: ReadonlySet<string>;
  error: string | null;
  toggle: (dir: string) => void;
  refresh: () => Promise<void>;
}

function message(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

/**
 * The explorer's data for one checkout: folders load lazily as they open, like
 * VS Code, and git decorations come from one status call for the whole tree.
 *
 * Agents write constantly, so runtime events only mark the tree dirty and a
 * timer refreshes it at most every SETTLE_MS: open folders are re-read and the
 * status recomputed, without collapsing anything the user opened.
 */
export function useProjectTree(root: string | null, active: boolean): ProjectTreeState {
  const [children, setChildren] = useState<Record<string, files.Entry[]>>({});
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const [status, setStatus] = useState<files.TreeStatus | null>(null);
  const [loading, setLoading] = useState<ReadonlySet<string>>(() => new Set());
  const [error, setError] = useState<string | null>(null);

  const expandedRef = useRef(expanded);
  expandedRef.current = expanded;
  const rootRef = useRef(root);
  rootRef.current = root;
  const dirty = useRef(false);
  const inFlight = useRef<Promise<void> | null>(null);
  const queued = useRef(false);

  const load = useCallback(async (dir: string) => {
    const at = rootRef.current;
    if (at === null) return;
    setLoading((prev) => new Set(prev).add(dir));
    try {
      const entries = await ProjectTree(at, dir);
      if (rootRef.current !== at) return;
      setChildren((prev) => ({ ...prev, [dir]: entries ?? [] }));
      setError(null);
    } catch (cause) {
      if (rootRef.current === at) setError(message(cause));
    } finally {
      setLoading((prev) => {
        const next = new Set(prev);
        next.delete(dir);
        return next;
      });
    }
  }, []);

  const reload = useCallback(async () => {
    const at = rootRef.current;
    if (at === null) return;
    const dirs = ['', ...expandedRef.current];
    const statusCall = ProjectTreeStatus(at)
      .then((next) => {
        if (rootRef.current === at) setStatus(next);
      })
      .catch(() => {
        // Decorations are a bonus; a failing git call leaves the tree usable.
        if (rootRef.current === at) setStatus(null);
      });
    await Promise.all([statusCall, ...dirs.map((dir) => load(dir))]);
  }, [load]);

  const refresh = useCallback(async () => {
    if (inFlight.current) {
      queued.current = true;
      return inFlight.current;
    }
    const run = (async () => {
      do {
        queued.current = false;
        await reload();
      } while (queued.current);
    })();
    inFlight.current = run;
    try {
      await run;
    } finally {
      inFlight.current = null;
    }
  }, [reload]);

  // A new checkout is a new tree.
  useEffect(() => {
    setChildren({});
    setExpanded(new Set());
    setStatus(null);
    setError(null);
    if (root !== null && active) void refresh();
  }, [root]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!active || root === null) return;
    void refresh();
    const off = onRuntimeEvents(() => {
      dirty.current = true;
    });
    const settle = window.setInterval(() => {
      if (!dirty.current || document.hidden) return;
      dirty.current = false;
      void refresh();
    }, SETTLE_MS);
    const idle = window.setInterval(() => {
      if (!document.hidden) void refresh();
    }, IDLE_REFRESH_MS);
    return () => {
      off();
      window.clearInterval(settle);
      window.clearInterval(idle);
    };
  }, [active, root, refresh]);

  const toggle = useCallback(
    (dir: string) => {
      setExpanded((prev) => {
        const next = new Set(prev);
        if (next.has(dir)) {
          next.delete(dir);
        } else {
          next.add(dir);
          void load(dir);
        }
        return next;
      });
    },
    [load],
  );

  return { children, expanded, status, loading, error, toggle, refresh };
}
