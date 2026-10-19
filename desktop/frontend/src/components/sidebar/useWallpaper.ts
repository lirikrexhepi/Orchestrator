import { useCallback, useEffect, useState } from 'react';
import { BUILT_IN, CUSTOM_PREFIX, Wallpaper, isCustom } from './wallpapers';

const SELECTED_KEY = 'composer:wallpaper';
const CUSTOM_KEY = 'composer:wallpapers:custom';

// Uploads are stored as data URLs in localStorage, which is a few MB in total.
// A photo straight from a camera would blow that budget on its own, so images
// are downscaled to display size before being kept.
const MAX_EDGE = 2560;
const QUALITY = 0.82;
const MAX_STORED = 6;

export interface WallpaperState {
  wallpapers: Wallpaper[];
  selected: Wallpaper | null;
  error: string | null;
  select: (id: string) => void;
  upload: (file: File) => Promise<void>;
  remove: (id: string) => void;
}

function readCustom(): Wallpaper[] {
  try {
    const raw = localStorage.getItem(CUSTOM_KEY);
    const parsed = raw ? (JSON.parse(raw) as Wallpaper[]) : [];
    return Array.isArray(parsed) ? parsed.filter((item) => item?.id && item?.url) : [];
  } catch {
    return [];
  }
}

// Re-encodes an upload at display resolution. Returns a JPEG data URL: PNG
// screenshots of photos are several times larger for no visible gain here.
async function toStoredImage(file: File): Promise<string> {
  const bitmap = await createImageBitmap(file);
  const scale = Math.min(1, MAX_EDGE / Math.max(bitmap.width, bitmap.height));
  const width = Math.round(bitmap.width * scale);
  const height = Math.round(bitmap.height * scale);

  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;

  const context = canvas.getContext('2d');
  if (!context) throw new Error('Could not process that image');
  context.drawImage(bitmap, 0, 0, width, height);
  bitmap.close();

  return canvas.toDataURL('image/jpeg', QUALITY);
}

export function useWallpaper(): WallpaperState {
  const [selectedId, setSelectedId] = useState<string>(
    () => localStorage.getItem(SELECTED_KEY) || BUILT_IN[0]?.id || '',
  );
  const [custom, setCustom] = useState<Wallpaper[]>(() => (isCustom(selectedId) ? readCustom() : []));
  const [customLoaded, setCustomLoaded] = useState(() => isCustom(selectedId));
  const [error, setError] = useState<string | null>(null);

  const wallpapers = [...BUILT_IN, ...custom];
  const selected = wallpapers.find((item) => item.id === selectedId) ?? wallpapers[0] ?? null;

  useEffect(() => {
    if (customLoaded) return;
    const load = () => {
      setCustom(readCustom());
      setCustomLoaded(true);
    };
    if ('requestIdleCallback' in window) {
      const id = window.requestIdleCallback(load, { timeout: 1000 });
      return () => window.cancelIdleCallback(id);
    }
    const timer = globalThis.setTimeout(load, 0);
    return () => globalThis.clearTimeout(timer);
  }, [customLoaded]);

  useEffect(() => {
    if (selected && (customLoaded || !isCustom(selected.id))) localStorage.setItem(SELECTED_KEY, selected.id);
  }, [customLoaded, selected]);

  const persist = useCallback((next: Wallpaper[]) => {
    setCustom(next);
    try {
      localStorage.setItem(CUSTOM_KEY, JSON.stringify(next));
      return true;
    } catch {
      // Quota exceeded: keep the image on screen for this session but say so,
      // rather than silently dropping it and confusing the next restart.
      setError('Saved for this session only — browser storage is full.');
      return false;
    }
  }, []);

  const upload = useCallback(
    async (file: File) => {
      setError(null);
      if (!file.type.startsWith('image/')) {
        setError('That file is not an image.');
        return;
      }

      try {
        const url = await toStoredImage(file);
        const entry: Wallpaper = {
          id: `${CUSTOM_PREFIX}${Date.now().toString(36)}`,
          label: file.name.replace(/\.[^.]+$/, '') || 'Custom',
          url,
        };
        // Oldest uploads fall off rather than accumulating until storage fails.
        const next = [...custom, entry].slice(-MAX_STORED);
        persist(next);
        setSelectedId(entry.id);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    },
    [custom, persist],
  );

  const remove = useCallback(
    (id: string) => {
      if (!isCustom(id)) return;
      const next = custom.filter((item) => item.id !== id);
      persist(next);
      if (selectedId === id) setSelectedId(BUILT_IN[0]?.id ?? '');
    },
    [custom, persist, selectedId],
  );

  const select = useCallback((id: string) => {
    setError(null);
    setSelectedId(id);
  }, []);

  return { wallpapers, selected, error, select, upload, remove };
}
