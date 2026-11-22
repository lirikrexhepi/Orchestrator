import { useCallback, useEffect, useState } from 'react';
import { CheckAppUpdate, CheckAppUpdateNow, InstallAppUpdate } from '../../../wailsjs/go/main/App';
import { updater } from '../../../wailsjs/go/models';
import { EventsOn } from '../../../wailsjs/runtime/runtime';

export interface AppUpdateProgress {
  stage: 'downloading' | 'verifying' | 'installing' | 'error';
  downloaded?: number;
  total?: number;
  percent?: number;
  error?: string;
}

export interface AppUpdateState {
  status: updater.Status | null;
  checking: boolean;
  installing: boolean;
  progress: AppUpdateProgress | null;
  error: string | null;
  checkNow: () => Promise<void>;
  install: () => Promise<void>;
}

export function useAppUpdate(): AppUpdateState {
  const [status, setStatus] = useState<updater.Status | null>(null);
  const [checking, setChecking] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [progress, setProgress] = useState<AppUpdateProgress | null>(null);
  const [error, setError] = useState<string | null>(null);

  const checkNow = useCallback(async () => {
    setChecking(true);
    setError(null);
    try {
      const res = await CheckAppUpdateNow();
      setStatus(res);
      if (res.error) setError(res.error);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setChecking(false);
    }
  }, []);

  const install = useCallback(async () => {
    setInstalling(true);
    setError(null);
    setProgress({ stage: 'downloading', percent: 0 });
    try {
      await InstallAppUpdate();
    } catch (cause) {
      setInstalling(false);
      setProgress(null);
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  useEffect(() => {
    let alive = true;

    // Fast initial check against cache or server
    CheckAppUpdate()
      .then((res) => {
        if (alive && res) setStatus(res);
      })
      .catch(() => undefined);

    // Refresh immediately after startup (3s) to ensure fresh GitHub releases state
    const bootTimer = window.setTimeout(() => {
      if (alive) void checkNow();
    }, 3000);

    // Periodic automatic background check every 3 minutes
    const interval = window.setInterval(() => {
      if (alive) void checkNow();
    }, 3 * 60 * 1000);

    // Check automatically whenever the user brings the app into focus
    const onFocus = () => {
      if (alive) void checkNow();
    };
    window.addEventListener('focus', onFocus);

    const offAvailable = EventsOn('app:update-available', (payload: updater.Status) => {
      if (alive && payload) setStatus(payload);
    });
    const offProgress = EventsOn('app:update-progress', (payload: AppUpdateProgress) => {
      if (!alive || !payload) return;
      setProgress(payload);
      if (payload.stage === 'error') {
        setInstalling(false);
        setError(payload.error || 'Update failed');
      }
    });

    return () => {
      alive = false;
      window.clearTimeout(bootTimer);
      window.clearInterval(interval);
      window.removeEventListener('focus', onFocus);
      offAvailable();
      offProgress();
    };
  }, [checkNow]);

  return { status, checking, installing, progress, error, checkNow, install };
}
