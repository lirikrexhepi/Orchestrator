import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { onRuntimeEvents } from '../agent-session/runtimeEvents';
import {
  GitCommitDiff,
  GitFileDiff,
  GitOverview,
  RemoveGitWorktree,
  RevealPath,
} from '../../../wailsjs/go/main/App';
import { domain } from '../../../wailsjs/go/models';

const IDLE_REFRESH_MS = 6_000;
const SETTLE_MS = 1_200;

/** Which side of a lane is being viewed: working tree, or one commit. */
export type GitView = { kind: 'changes' } | { kind: 'commit'; sha: string };

/** Identifies a file row uniquely — a path can appear staged and unstaged. */
export interface FileKey {
  path: string;
  staged: boolean;
}

export interface GitState {
  lanes: domain.WorktreeChanges[];
  activeLane: domain.WorktreeChanges | null;
  activeLanePath: string | null;
  view: GitView;
  selectedFile: FileKey | null;
  /** Diffs for the current selection: one file, or every file in a commit. */
  diffs: domain.DiffFile[];
  isLoading: boolean;
  isDiffLoading: boolean;
  error: string | null;
  diffError: string | null;
  selectLane: (path: string) => void;
  selectView: (view: GitView) => void;
  selectFile: (file: FileKey) => void;
  refresh: (silent?: boolean) => Promise<void>;
  removeWorktree: (path: string, force: boolean) => Promise<string | null>;
  reveal: (path: string) => Promise<void>;
}

function message(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

function sameFile(left: FileKey | null, right: FileKey | null): boolean {
  if (!left || !right) return left === right;
  return left.path === right.path && left.staged === right.staged;
}

/**
 * Drives the git window: which checkout is being viewed, which file inside it,
 * and the diff for that selection.
 *
 * Every git call can fail — a worktree deleted from under us, a repository mid
 * rebase — so failures are held per concern rather than as one flag. A diff that
 * fails to load must not blank the lane list that is still perfectly valid.
 */
export function useGit(isOpen: boolean): GitState {
  const [lanes, setLanes] = useState<domain.WorktreeChanges[]>([]);
  const [activeLanePath, setActiveLanePath] = useState<string | null>(null);
  const [view, setView] = useState<GitView>({ kind: 'changes' });
  const [selectedFile, setSelectedFile] = useState<FileKey | null>(null);
  const [diffs, setDiffs] = useState<domain.DiffFile[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [diffError, setDiffError] = useState<string | null>(null);

  // Guards against a slow response for an earlier selection overwriting a newer
  // one — the classic out-of-order fetch that shows the wrong file's diff.
  const diffToken = useRef(0);
  const dirty = useRef(false);

  const lanesKey = useRef('');
  const diffKey = useRef('');
  const inFlight = useRef<Promise<void> | null>(null);
  const queued = useRef(false);

  const load = useCallback(async () => {
    try {
      const next = await GitOverview();
      const key = JSON.stringify(next);
      if (key !== lanesKey.current) {
        lanesKey.current = key;
        setLanes(next);
      }
      setError((prev) => (prev === null ? prev : null));
    } catch (cause) {
      setError(message(cause));
    }
  }, []);

  const refresh = useCallback(
    async (silent = false) => {
      if (inFlight.current) {
        queued.current = true;
        if (!silent) {
          setIsLoading(true);
          await inFlight.current;
          setIsLoading(false);
        }
        return;
      }
      if (!silent) setIsLoading(true);
      const run = (async () => {
        do {
          queued.current = false;
          await load();
        } while (queued.current);
      })();
      inFlight.current = run;
      try {
        await run;
      } finally {
        inFlight.current = null;
        if (!silent) setIsLoading(false);
      }
    },
    [load],
  );

  useEffect(() => {
    if (!isOpen) return;
    void refresh();
  }, [isOpen, refresh]);

  // Agents write files constantly; refreshing per event would run git dozens of
  // times a second. Instead events mark the view dirty and a timer collects
  // them, so a burst of activity costs one refresh.
  useEffect(() => {
    if (!isOpen) return;
    const off = onRuntimeEvents(() => {
      dirty.current = true;
    });
    const settle = window.setInterval(() => {
      if (!dirty.current || document.hidden) return;
      dirty.current = false;
      void refresh(true);
    }, SETTLE_MS);
    const idle = window.setInterval(() => {
      if (!document.hidden) void refresh(true);
    }, IDLE_REFRESH_MS);

    return () => {
      off();
      window.clearInterval(settle);
      window.clearInterval(idle);
    };
  }, [isOpen, refresh]);

  // The project lane is the opening view, and a lane that disappears — its
  // worktree removed — falls back rather than leaving the window blank.
  useEffect(() => {
    if (lanes.length === 0) return;
    const stillThere = lanes.some((lane) => lane.path === activeLanePath);
    if (!stillThere) {
      setActiveLanePath((lanes.find((lane) => lane.isMain) ?? lanes[0]).path);
      setView({ kind: 'changes' });
      setSelectedFile(null);
    }
  }, [lanes, activeLanePath]);

  const activeLane = useMemo(
    () => lanes.find((lane) => lane.path === activeLanePath) ?? null,
    [lanes, activeLanePath],
  );

  // Keep a selection pointing at something real: a file that stops being
  // changed, or an agent that commits its work, must not leave a stale diff up.
  useEffect(() => {
    if (view.kind !== 'changes' || !activeLane) return;
    const files = activeLane.files ?? [];
    if (files.length === 0) {
      if (selectedFile) setSelectedFile(null);
      return;
    }
    const present = files.some((file) => sameFile(selectedFile, file));
    if (!present) {
      setSelectedFile({ path: files[0].path, staged: files[0].staged });
    }
  }, [activeLane, view.kind, selectedFile]);

  useEffect(() => {
    if (!isOpen || !activeLane) return;

    const token = ++diffToken.current;
    const apply = (next: domain.DiffFile[], failure: string | null) => {
      if (diffToken.current !== token) return;
      setDiffs(next);
      setDiffError(failure);
      setIsDiffLoading(false);
    };

    const selectionKey =
      view.kind === 'commit'
        ? `commit:${activeLane.path}:${view.sha}`
        : `file:${activeLane.path}:${selectedFile?.path}:${selectedFile?.staged}`;
    const sameSelection = diffKey.current === selectionKey;
    diffKey.current = selectionKey;

    if (view.kind === 'commit') {
      if (!sameSelection) setIsDiffLoading(true);
      GitCommitDiff(activeLane.path, view.sha)
        .then((next) => apply(next ?? [], null))
        .catch((cause) => apply([], message(cause)));
      return;
    }

    if (!selectedFile) {
      apply([], null);
      return;
    }

    if (!sameSelection) setIsDiffLoading(true);
    GitFileDiff(activeLane.path, selectedFile.path, selectedFile.staged)
      .then((next) => apply(next ? [next] : [], null))
      .catch((cause) => apply([], message(cause)));
  }, [isOpen, activeLane, view, selectedFile]);

  const selectLane = useCallback((path: string) => {
    setActiveLanePath(path);
    setView({ kind: 'changes' });
    setSelectedFile(null);
    setDiffs([]);
    setDiffError(null);
  }, []);

  const selectView = useCallback((next: GitView) => {
    setView(next);
    setDiffs([]);
    setDiffError(null);
    if (next.kind === 'commit') setSelectedFile(null);
  }, []);

  const selectFile = useCallback((file: FileKey) => {
    setView({ kind: 'changes' });
    setSelectedFile(file);
  }, []);

  // Returns the refusal rather than throwing: git declines to remove a worktree
  // holding uncommitted work, and that refusal is the prompt the user needs.
  const removeWorktree = useCallback(
    async (path: string, force: boolean): Promise<string | null> => {
      try {
        await RemoveGitWorktree(path, force);
        await refresh();
        return null;
      } catch (cause) {
        return message(cause);
      }
    },
    [refresh],
  );

  const reveal = useCallback(async (path: string) => {
    try {
      await RevealPath(path);
    } catch (cause) {
      setError(message(cause));
    }
  }, []);

  return {
    lanes,
    activeLane,
    activeLanePath,
    view,
    selectedFile,
    diffs,
    isLoading,
    isDiffLoading,
    error,
    diffError,
    selectLane,
    selectView,
    selectFile,
    refresh,
    removeWorktree,
    reveal,
  };
}
