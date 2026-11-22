import { useCallback, useEffect, useRef, useState } from 'react';
import {
  AccountStatus,
  ActiveProject,
  AddAccount,
  AddAntigravityAccount,
  CaptureAntigravityProfile,
  ListProviders,
  RemoveAccount,
  RenameAccount,
  RestoreAntigravityDefault,
  SetProjectAccount,
  SignInAccount,
} from '../../../wailsjs/go/main/App';
import { domain, projects } from '../../../wailsjs/go/models';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';

export interface AccountStatusState {
  signedIn: boolean;
  known: boolean;
  detail?: string;
  checking?: boolean;
}

export const COPYABLE = new Set(['claude', 'antigravity']);

const SIGN_IN_POLL_MS = 3000;
const SIGN_IN_POLL_LIMIT = 100;

export const statusKey = (driver: string, id: string) => `${driver}:${id}`;

const message = (cause: unknown) => (cause instanceof Error ? cause.message : String(cause));

export const emailFromDetail = (detail?: string) => {
  if (!detail) return '';
  for (const part of detail.split('·')) {
    const trimmed = part.trim();
    if (trimmed.includes('@')) return trimmed;
  }
  return '';
};

export interface ProviderAccounts {
  snapshots: domain.ProviderSnapshot[];
  project: projects.Project | null;
  statuses: Record<string, AccountStatusState>;
  adding: string | null;
  name: string;
  authMode: 'keyring' | 'api_key';
  apiKey: string;
  copySettings: boolean;
  renaming: string | null;
  renameText: string;
  confirmRemove: string | null;
  busy: boolean;
  error: string | null;
  notice: string | null;
  setAdding: (driver: string | null) => void;
  setName: (name: string) => void;
  setAuthMode: (mode: 'keyring' | 'api_key') => void;
  setApiKey: (key: string) => void;
  setCopySettings: (copy: boolean) => void;
  setRenaming: (key: string | null) => void;
  setRenameText: (text: string) => void;
  setConfirmRemove: (key: string | null) => void;
  signIn: (driver: string, id: string) => void;
  captureProfile: (id: string) => void;
  add: (driver: string) => void;
  rename: (driver: string, id: string) => void;
  remove: (driver: string, id: string) => void;
  setProjectDefault: (driver: string, id: string) => void;
}

/**
 * Owns provider snapshots, per-account sign-in status and every account
 * mutation. Previously lived inside AccountsSection; lifted so the merged
 * provider cards can render accounts inline.
 */
export function useProviderAccounts(): ProviderAccounts {
  const [snapshots, setSnapshots] = useState<domain.ProviderSnapshot[]>([]);
  const [project, setProject] = useState<projects.Project | null>(null);
  const [statuses, setStatuses] = useState<Record<string, AccountStatusState>>({});
  const [adding, setAdding] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [authMode, setAuthMode] = useState<'keyring' | 'api_key'>('keyring');
  const [apiKey, setApiKey] = useState('');
  const [copySettings, setCopySettings] = useState(true);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [renameText, setRenameText] = useState('');
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const polls = useRef<Record<string, number>>({});

  const load = useCallback(async () => {
    try {
      const [list, active] = await Promise.all([ListProviders(false), ActiveProject().catch(() => null)]);
      setSnapshots((list ?? []).filter((snapshot) => (snapshot.accounts?.length ?? 0) > 0));
      setProject(active ?? null);
    } catch (cause) {
      setError(message(cause));
    }
  }, []);

  const check = useCallback(async (driver: string, id: string) => {
    const key = statusKey(driver, id);
    setStatuses((previous) => ({ ...previous, [key]: { ...(previous[key] ?? { signedIn: false, known: false }), checking: true } }));
    try {
      const status = await AccountStatus(driver, id);
      setStatuses((previous) => ({ ...previous, [key]: { ...status, checking: false } }));
      return status.signedIn;
    } catch (cause) {
      setStatuses((previous) => ({ ...previous, [key]: { signedIn: false, known: false, detail: message(cause), checking: false } }));
      return false;
    }
  }, []);

  useEffect(() => {
    void load();
    const running = polls.current;
    return () => {
      Object.values(running).forEach((timer) => window.clearInterval(timer));
    };
  }, [load]);

  useEffect(() => {
    for (const snapshot of snapshots) {
      if (snapshot.availability !== 'ready') continue;
      for (const account of snapshot.accounts ?? []) {
        if (!statuses[statusKey(snapshot.driver, account.id)]) void check(snapshot.driver, account.id);
      }
    }
  }, [snapshots, statuses, check]);

  const refreshEverywhere = useCallback(async () => {
    await load();
    await useOrchestratorStore.getState().loadProviders(true);
  }, [load]);

  const run = async (action: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await action();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  };

  const watchSignIn = useCallback(
    (driver: string, id: string) => {
      const key = statusKey(driver, id);
      window.clearInterval(polls.current[key]);
      let tries = 0;
      polls.current[key] = window.setInterval(async () => {
        tries += 1;
        const done = await check(driver, id);
        if (done) {
          if (driver === 'antigravity') {
            try {
              await CaptureAntigravityProfile(id);
              await check(driver, id);
            } catch {
              // ignore
            }
          }
          window.clearInterval(polls.current[key]);
          delete polls.current[key];
        } else if (tries >= SIGN_IN_POLL_LIMIT) {
          if (driver === 'antigravity') {
            void RestoreAntigravityDefault().catch(() => {});
          }
          window.clearInterval(polls.current[key]);
          delete polls.current[key];
        }
      }, SIGN_IN_POLL_MS);
    },
    [check],
  );

  const signIn = (driver: string, id: string) =>
    run(async () => {
      await SignInAccount(driver, id);
      setNotice('Sign-in window opened.');
      watchSignIn(driver, id);
    });

  const captureProfile = (id: string) =>
    run(async () => {
      await CaptureAntigravityProfile(id);
      setNotice('Saved active keyring credentials to profile.');
      await refreshEverywhere();
      await check('antigravity', id);
    });

  const add = (driver: string) =>
    run(async () => {
      let result;
      if (driver === 'antigravity') {
        result = await AddAntigravityAccount(name, authMode, apiKey);
      } else {
        result = await AddAccount(driver, name, COPYABLE.has(driver) && copySettings);
      }
      const newId = result.account.id;
      setAdding(null);
      setName('');
      setApiKey('');
      setAuthMode('keyring');
      if (result.warning) setError(result.warning);
      await refreshEverywhere();

      if (driver === 'antigravity' && authMode === 'api_key') {
        setNotice(`Added ${result.account.name} (API Key).`);
        await check(driver, newId);
        return;
      }

      try {
        await SignInAccount(driver, newId);
        setNotice(`Added ${result.account.name}. Sign-in window opened.`);
        watchSignIn(driver, newId);
      } catch (cause) {
        setNotice(`Added ${result.account.name}.`);
        setError(message(cause));
      }
    });

  const rename = (driver: string, id: string) =>
    run(async () => {
      await RenameAccount(driver, id, renameText);
      setRenaming(null);
      await refreshEverywhere();
    });

  const remove = (driver: string, id: string) =>
    run(async () => {
      await RemoveAccount(driver, id);
      setConfirmRemove(null);
      setNotice('Removed.');
      await refreshEverywhere();
    });

  const setProjectDefault = (driver: string, id: string) =>
    run(async () => {
      if (!project) return;
      const updated = await SetProjectAccount(project.id, driver, id === 'default' ? '' : id);
      setProject(updated);
    });

  return {
    snapshots,
    project,
    statuses,
    adding,
    name,
    authMode,
    apiKey,
    copySettings,
    renaming,
    renameText,
    confirmRemove,
    busy,
    error,
    notice,
    setAdding,
    setName,
    setAuthMode,
    setApiKey,
    setCopySettings,
    setRenaming,
    setRenameText,
    setConfirmRemove,
    signIn,
    captureProfile,
    add,
    rename,
    remove,
    setProjectDefault,
  };
}
