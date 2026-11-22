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

  useEffect(() => {
    let alive = true;
    CheckAppUpdate()
      .then((res) => {
        if (alive) setStatus(res);
      })
      .catch(() => undefined);

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
      offAvailable();
      offProgress();
    };
  }, []);

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

  return { status, checking, installing, progress, error, checkNow, install };
}
