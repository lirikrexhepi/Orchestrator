import { useCallback, useEffect, useState } from 'react';
import {
  ActiveProject,
  AddProject,
  ChooseProject,
  ListProjects,
  MoveProject,
  RemoveProject,
  SelectProject,
} from '../../../wailsjs/go/main/App';
import { projects } from '../../../wailsjs/go/models';
import { EventsOn } from '../../../wailsjs/runtime/runtime';

export type Project = projects.Project;

export interface ProjectsState {
  projects: Project[];
  active: Project | null;
  error: string | null;
  isChoosing: boolean;
  /** Opens the OS folder picker; resolves false when cancelled. */
  choose: () => Promise<boolean>;
  add: (path: string) => Promise<boolean>;
  select: (id: string) => Promise<boolean>;
  remove: (id: string) => Promise<void>;
  move: (id: string, toIndex: number) => Promise<void>;
  refresh: () => Promise<void>;
}

/**
 * Owns the saved project list and which one agents start in.
 *
 * @param onSwitched runs after the active project changes, so the caller can
 *   reset the coordinator conversation for the new project. Running agents
 *   are preserved across projects; only the coordinator transcript is cut.
 */
export function useProjects(onSwitched?: () => void | Promise<void>): ProjectsState {
  const [list, setList] = useState<Project[]>([]);
  const [active, setActive] = useState<Project | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isChoosing, setChoosing] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const [saved, current] = await Promise.all([ListProjects(), ActiveProject()]);
      setList(saved ?? []);
      setActive(current ?? null);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  useEffect(() => {
    void refresh();
    // Projects can be added from the phone while the window is open.
    return EventsOn('projects:changed', () => void refresh());
  }, [refresh]);

  // Applies a project the backend has already saved. The switch callback is
  // skipped when the same project is re-selected, so clicking the current entry
  // never throws away a conversation for no reason.
  const apply = useCallback(
    async (project: Project | null, previousId?: string) => {
      if (!project) return false;
      setActive(project);
      await refresh();
      if (project.id !== previousId) await onSwitched?.();
      return true;
    },
    [onSwitched, refresh],
  );

  const choose = useCallback(async () => {
    setChoosing(true);
    try {
      const previousId = active?.id;
      // A cancelled dialog resolves to nothing; that is a normal outcome and
      // must not surface as an error.
      const project = await ChooseProject();
      if (!project) return false;
      return await apply(project, previousId);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      return false;
    } finally {
      setChoosing(false);
    }
  }, [active?.id, apply]);

  const add = useCallback(
    async (path: string) => {
      try {
        const previousId = active?.id;
        return await apply(await AddProject(path), previousId);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
        return false;
      }
    },
    [active?.id, apply],
  );

  const select = useCallback(
    async (id: string) => {
      if (id === active?.id) return false;
      try {
        return await apply(await SelectProject(id), active?.id);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
        return false;
      }
    },
    [active?.id, apply],
  );

  const remove = useCallback(
    async (id: string) => {
      try {
        await RemoveProject(id);
        await refresh();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    },
    [refresh],
  );

  const move = useCallback(
    async (id: string, toIndex: number) => {
      try {
        await MoveProject(id, toIndex);
        await refresh();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    },
    [refresh],
  );

  return { projects: list, active, error, isChoosing, choose, add, select, remove, move, refresh };
}
