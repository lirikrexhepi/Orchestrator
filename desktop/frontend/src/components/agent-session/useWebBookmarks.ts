import { useCallback, useEffect, useMemo, useState } from 'react';

export interface WebBookmark {
  id: string;
  title: string;
  url: string;
  project?: string | null; // null/undefined/"" = Universal (shows everywhere); string = project name
  createdAt: number;
}

const STORAGE_KEY = 'orchestrator:web_bookmarks';

function readStoredBookmarks(): WebBookmark[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((b) => b && typeof b.id === 'string' && typeof b.url === 'string');
  } catch {
    return [];
  }
}

function writeStoredBookmarks(bookmarks: WebBookmark[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(bookmarks));
  } catch (err) {
    console.error('Failed to save web bookmarks:', err);
  }
}

export function getDomain(urlStr: string): string {
  try {
    const clean = urlStr.trim();
    const u = new URL(clean.startsWith('http://') || clean.startsWith('https://') ? clean : `https://${clean}`);
    return u.hostname;
  } catch {
    return urlStr;
  }
}

export function getFaviconUrl(urlStr: string): string {
  const domain = getDomain(urlStr);
  return `https://www.google.com/s2/favicons?domain=${encodeURIComponent(domain)}&sz=32`;
}

export function useWebBookmarks(projectName?: string | null) {
  const [bookmarks, setBookmarks] = useState<WebBookmark[]>(() => readStoredBookmarks());

  useEffect(() => {
    const handleStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY) {
        setBookmarks(readStoredBookmarks());
      }
    };
    window.addEventListener('storage', handleStorage);
    return () => window.removeEventListener('storage', handleStorage);
  }, []);

  const addBookmark = useCallback(
    (bookmark: { title: string; url: string; project?: string | null }) => {
      const cleanUrl = bookmark.url.trim();
      const cleanTitle = bookmark.title.trim() || getDomain(cleanUrl) || cleanUrl;
      const newBm: WebBookmark = {
        id: 'bm-' + Date.now() + '-' + Math.random().toString(36).slice(2, 7),
        title: cleanTitle,
        url: cleanUrl,
        project: bookmark.project?.trim() || null,
        createdAt: Date.now(),
      };
      setBookmarks((prev) => {
        const next = [newBm, ...prev];
        writeStoredBookmarks(next);
        return next;
      });
      return newBm;
    },
    [],
  );

  const updateBookmark = useCallback(
    (id: string, updates: { title?: string; url?: string; project?: string | null }) => {
      setBookmarks((prev) => {
        const next = prev.map((b) => {
          if (b.id !== id) return b;
          return {
            ...b,
            title: updates.title !== undefined ? updates.title.trim() || b.title : b.title,
            url: updates.url !== undefined ? updates.url.trim() || b.url : b.url,
            project: updates.project !== undefined ? (updates.project?.trim() || null) : b.project,
          };
        });
        writeStoredBookmarks(next);
        return next;
      });
    },
    [],
  );

  const deleteBookmark = useCallback((id: string) => {
    setBookmarks((prev) => {
      const next = prev.filter((b) => b.id !== id);
      writeStoredBookmarks(next);
      return next;
    });
  }, []);

  const isBookmarked = useCallback(
    (url: string) => {
      if (!url || url === 'about:blank') return false;
      const clean = url.trim().replace(/\/+$/, '').toLowerCase();
      return bookmarks.some((b) => b.url.trim().replace(/\/+$/, '').toLowerCase() === clean);
    },
    [bookmarks],
  );

  const getBookmarkByUrl = useCallback(
    (url: string) => {
      if (!url || url === 'about:blank') return null;
      const clean = url.trim().replace(/\/+$/, '').toLowerCase();
      return bookmarks.find((b) => b.url.trim().replace(/\/+$/, '').toLowerCase() === clean) || null;
    },
    [bookmarks],
  );

  // Applicable bookmarks for the active project: universal ones + ones matching projectName
  const visibleBookmarks = useMemo(() => {
    const currentName = projectName?.trim().toLowerCase();
    return bookmarks.filter((b) => {
      if (!b.project) return true; // Universal
      if (!currentName) return false;
      return b.project.trim().toLowerCase() === currentName;
    });
  }, [bookmarks, projectName]);

  return {
    bookmarks,
    visibleBookmarks,
    addBookmark,
    updateBookmark,
    deleteBookmark,
    isBookmarked,
    getBookmarkByUrl,
  };
}
