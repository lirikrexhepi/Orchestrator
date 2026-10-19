import { useCallback, useRef, useState } from 'react';
import {
  ImportClaudeCodeSession,
  ImportOpenCodeSession,
  ListChatSourcePage,
} from '../../../wailsjs/go/main/App';
import { claudeimport } from '../../../wailsjs/go/models';

export type ChatProvider = 'claude' | 'opencode' | 'antigravity';

export const CHAT_PROVIDERS: ChatProvider[] = ['claude', 'antigravity', 'opencode'];

const FIRST_PAGE = 5;
const NEXT_PAGE = 10;
const MAX_EMPTY_PAGES = 12;

export interface ClaudeImportState {
  sessions: claudeimport.ExternalSession[];
  isLoading: boolean;
  isLoadingMore: boolean;
  exhausted: Record<ChatProvider, boolean>;
  isImporting: boolean;
  error: string | null;
  list: () => Promise<void>;
  loadMore: (providers?: ChatProvider[]) => Promise<void>;
  importOne: (filePath: string) => Promise<string | null>;
  reset: () => void;
}

const freshCursors = (): Record<ChatProvider, number> => ({ claude: 0, opencode: 0, antigravity: 0 });
const freshExhausted = (): Record<ChatProvider, boolean> => ({ claude: false, opencode: false, antigravity: false });

function mergeSessions(
  current: claudeimport.ExternalSession[],
  incoming: claudeimport.ExternalSession[],
): claudeimport.ExternalSession[] {
  const byPath = new Map<string, claudeimport.ExternalSession>();
  for (const session of current) byPath.set(session.filePath, session);
  for (const session of incoming) byPath.set(session.filePath, session);
  return [...byPath.values()].sort((a, b) => b.updatedAt - a.updatedAt);
}

export function useClaudeImport(): ClaudeImportState {
  const [sessions, setSessions] = useState<claudeimport.ExternalSession[]>([]);
  const [isLoading, setLoading] = useState(false);
  const [isLoadingMore, setLoadingMore] = useState(false);
  const [exhausted, setExhausted] = useState<Record<ChatProvider, boolean>>(freshExhausted);
  const [isImporting, setImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const cursors = useRef(freshCursors());
  const busy = useRef<Set<ChatProvider>>(new Set());
  const loadedOnce = useRef(false);

  const fetchProvider = useCallback(async (provider: ChatProvider, start: number, want: number, size: number) => {
    let next = start;
    let collected = 0;
    let emptyPages = 0;
    while (next >= 0 && collected < want && emptyPages < MAX_EMPTY_PAGES) {
      const page = await ListChatSourcePage(provider, next, size);
      const found = page?.sessions ?? [];
      next = page?.next ?? -1;
      if (found.length === 0) {
        emptyPages++;
      } else {
        emptyPages = 0;
        collected += found.length;
        setSessions((current) => mergeSessions(current, found));
      }
    }
    return next;
  }, []);

  const list = useCallback(async () => {
    if (busy.current.size > 0) return;
    const first = !loadedOnce.current;
    if (first) setLoading(true);
    CHAT_PROVIDERS.forEach((provider) => busy.current.add(provider));
    await Promise.all(
      CHAT_PROVIDERS.map(async (provider) => {
        try {
          const next = await fetchProvider(provider, 0, FIRST_PAGE, FIRST_PAGE);
          if (first || next < 0) {
            cursors.current[provider] = next;
            setExhausted((current) => ({ ...current, [provider]: next < 0 }));
          }
          setError(null);
        } catch (cause) {
          setError(cause instanceof Error ? cause.message : String(cause));
        } finally {
          busy.current.delete(provider);
        }
      }),
    );
    loadedOnce.current = true;
    setLoading(false);
  }, [fetchProvider]);

  const loadMore = useCallback(
    async (providers: ChatProvider[] = CHAT_PROVIDERS) => {
      if (!loadedOnce.current) return;
      const targets = providers.filter((provider) => cursors.current[provider] >= 0 && !busy.current.has(provider));
      if (targets.length === 0) return;
      targets.forEach((provider) => busy.current.add(provider));
      setLoadingMore(true);
      await Promise.all(
        targets.map(async (provider) => {
          try {
            const next = await fetchProvider(provider, cursors.current[provider], NEXT_PAGE, NEXT_PAGE);
            cursors.current[provider] = next;
            setExhausted((current) => ({ ...current, [provider]: next < 0 }));
          } catch (cause) {
            setError(cause instanceof Error ? cause.message : String(cause));
          } finally {
            busy.current.delete(provider);
          }
        }),
      );
      setLoadingMore(false);
    },
    [fetchProvider],
  );

  const importOne = useCallback(async (filePath: string): Promise<string | null> => {
    setImporting(true);
    try {
      if (filePath.startsWith('antigravity:')) {
        setError("Opening Antigravity chats isn't supported yet.");
        return null;
      }
      const workspaceId = filePath.startsWith('opencode:')
        ? await ImportOpenCodeSession(filePath.slice('opencode:'.length))
        : await ImportClaudeCodeSession(filePath);
      setError(null);
      return workspaceId ?? null;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      return null;
    } finally {
      setImporting(false);
    }
  }, []);

  const reset = useCallback(() => {
    setSessions([]);
    setError(null);
    setExhausted(freshExhausted());
    cursors.current = freshCursors();
    loadedOnce.current = false;
  }, []);

  return { sessions, isLoading, isLoadingMore, exhausted, isImporting, error, list, loadMore, importOne, reset };
}
