import { useCallback, useEffect, useMemo, useState } from 'react';

export interface WebBookmark {
  id: string;
  title: string;
  url: string;
  favicon?: string;
  project?: string | null; // null/undefined/"" = Universal (shows everywhere); string = project name
  createdAt: number;
}

const STORAGE_KEY = 'orchestrator:web_bookmarks';
const FAVICON_CACHE_KEY = 'orchestrator:web_favicons';
const BOOKMARKS_UPDATED_EVENT = 'orchestrator:web-bookmarks-updated';

function readFaviconCache(): Record<string, string> {
  try {
    const raw = localStorage.getItem(FAVICON_CACHE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch {
    return {};
  }
}

export function rememberBrowserFavicon(urlStr: string, faviconDataUrl: string): void {
  if (!urlStr || !faviconDataUrl || !faviconDataUrl.startsWith('data:image/')) return;
  const domain = getDomain(urlStr).toLowerCase();
  if (!domain) return;
  try {
    const cache = readFaviconCache();
    const changedCache = cache[domain] !== faviconDataUrl;
    if (changedCache) {
      cache[domain] = faviconDataUrl;
      localStorage.setItem(FAVICON_CACHE_KEY, JSON.stringify(cache));
    }
    const stored = readStoredBookmarks();
    let changedBookmarks = false;
    const next = stored.map((bm) => {
      if (getDomain(bm.url).toLowerCase() === domain && bm.favicon !== faviconDataUrl) {
        changedBookmarks = true;
        return { ...bm, favicon: faviconDataUrl };
      }
      return bm;
    });
    if (changedBookmarks) {
      writeStoredBookmarks(next);
      window.dispatchEvent(new Event(BOOKMARKS_UPDATED_EVENT));
    } else if (changedCache) {
      window.dispatchEvent(new Event(BOOKMARKS_UPDATED_EVENT));
    }
  } catch {
    // Ignore storage quota errors
  }
}

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

export function getFaviconUrl(urlStr: string, savedFavicon?: string): string {
  if (savedFavicon && savedFavicon.startsWith('data:image/')) {
    return savedFavicon;
  }
  const domain = getDomain(urlStr);
  const cached = readFaviconCache()[domain.toLowerCase()];
  if (cached && cached.startsWith('data:image/')) {
    return cached;
  }
  return `https://www.google.com/s2/favicons?domain=${encodeURIComponent(domain)}&sz=32`;
}

export function useWebBookmarks(projectName?: string | null) {
  const [bookmarks, setBookmarks] = useState<WebBookmark[]>(() => readStoredBookmarks());

  useEffect(() => {
    const handleStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY || e.key === FAVICON_CACHE_KEY) {
        setBookmarks(readStoredBookmarks());
      }
    };
    const handleLocalUpdate = () => {
      setBookmarks(readStoredBookmarks());
    };
    window.addEventListener('storage', handleStorage);
    window.addEventListener(BOOKMARKS_UPDATED_EVENT, handleLocalUpdate);
    return () => {
      window.removeEventListener('storage', handleStorage);
      window.removeEventListener(BOOKMARKS_UPDATED_EVENT, handleLocalUpdate);
    };
  }, []);

  const addBookmark = useCallback(
    (bookmark: { title: string; url: string; favicon?: string; project?: string | null }) => {
      const cleanUrl = bookmark.url.trim();
      const cleanTitle = bookmark.title.trim() || getDomain(cleanUrl) || cleanUrl;
      const domain = getDomain(cleanUrl).toLowerCase();
      const cachedFavicon = bookmark.favicon || readFaviconCache()[domain];
      const newBm: WebBookmark = {
        id: 'bm-' + Date.now() + '-' + Math.random().toString(36).slice(2, 7),
        title: cleanTitle,
        url: cleanUrl,
        ...(cachedFavicon ? { favicon: cachedFavicon } : {}),
        project: bookmark.project?.trim() || null,
        createdAt: Date.now(),
      };
      setBookmarks((prev) => {
        const next = [newBm, ...prev];
        writeStoredBookmarks(next);
        window.dispatchEvent(new Event(BOOKMARKS_UPDATED_EVENT));
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
