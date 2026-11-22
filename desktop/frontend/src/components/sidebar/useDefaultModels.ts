import { useCallback, useEffect, useState } from 'react';
import {
  GetAccountModels,
  GetProviderSettings,
  ListProviders,
  UpdateProviderSettings,
} from '../../../wailsjs/go/main/App';
import { domain } from '../../../wailsjs/go/models';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { AIModel, CLIProvider } from '../orchestrator/types';
import { toModel, toProvider } from '../orchestrator/orchestratorData';

export interface EffortChoice {
  value: string;
  label: string;
}

export interface AccountDefaultEntry {
  models: AIModel[];
  modelId: string;
  effortId: string;
  effortChoices: EffortChoice[];
}

export interface ProviderDefault {
  provider: CLIProvider;
  models: AIModel[];
  /** Empty means the CLI's own default is used rather than a pinned choice. */
  modelId: string;
  enabled: boolean;
  /** Backend effort id for the pinned model (e.g. "high"). Empty when the model has no effort knob. */
  effortId: string;
  effortChoices: EffortChoice[];
  /** Per-account default model & effort, keyed by account ID (e.g. "default", "lirikrexhepiii-neon"). */
  accountDefaults: Record<string, AccountDefaultEntry>;
}

export interface DefaultModels {
  entries: ProviderDefault[];
  error: string | null;
  isSaving: string | null;
  select: (providerId: string, modelId: string) => Promise<void>;
  selectEffort: (providerId: string, effortId: string) => Promise<void>;
  selectForAccount: (providerId: string, accountId: string, modelId: string) => Promise<void>;
  selectEffortForAccount: (providerId: string, accountId: string, effortId: string) => Promise<void>;
  toggle: (providerId: string, enabled: boolean) => Promise<void>;
}

const EFFORT_OPTION = 'effort';

function effortDescriptor(model: AIModel | undefined) {
  return model?.options?.find((option) => option.id === EFFORT_OPTION);
}

function resolveEffort(
  model: AIModel | undefined,
  saved: string,
): { effortId: string; choices: EffortChoice[] } {
  const descriptor = effortDescriptor(model);
  const choices: EffortChoice[] =
    descriptor?.choices?.map((choice) => ({ value: choice.id, label: choice.label })) ?? [];
  if (choices.length === 0) return { effortId: '', choices };
  if (saved && choices.some((choice) => choice.value === saved)) {
    return { effortId: saved, choices };
  }
  const fallback =
    descriptor?.choices?.find((choice) => choice.default)?.id ?? choices[0]?.value ?? '';
  return { effortId: fallback, choices };
}

function effortLabel(model: AIModel | undefined, effortId: string): string | null {
  const descriptor = effortDescriptor(model);
  return descriptor?.choices?.find((choice) => choice.id === effortId)?.label ?? null;
}

/**
 * Per-CLI and per-account preferred model, permission toggle and default effort.
 *
 * Stored against the provider in backend settings so choices survive restart.
 */
export function useDefaultModels(isOpen: boolean): DefaultModels {
  const [entries, setEntries] = useState<ProviderDefault[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [isSaving, setSaving] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const snapshots = await ListProviders(false);
      const ready = snapshots.filter((s) => s.availability === 'ready');

      const items: ProviderDefault[] = await Promise.all(
        ready.map(async (snapshot) => {
          const provider = toProvider(snapshot);
          const models = (snapshot.models ?? []).map((m) => toModel(snapshot, m));
          const settings = await GetProviderSettings(provider.id);
          const modelId = settings.model ?? '';
          const model = models.find((m) => m.id === modelId);
          const savedEffort =
            typeof settings.options?.[EFFORT_OPTION] === 'string'
              ? (settings.options[EFFORT_OPTION] as string)
              : '';
          const { effortId, choices } = resolveEffort(model, savedEffort);

          const accounts = snapshot.accounts ?? [];
          const savedAccountDefaults = settings.accountDefaults ?? {};
          const accountDefaults: Record<string, AccountDefaultEntry> = {};

          await Promise.all(
            accounts.map(async (account) => {
              let accModels = models;
              if (accounts.length > 1) {
                try {
                  const raw = await GetAccountModels(provider.id, account.id);
                  if (raw && raw.length > 0) {
                    const parsed = raw.map((m) => toModel(snapshot, m));
                    accModels = parsed.filter((m, idx, arr) => arr.findIndex((x) => x.id === m.id) === idx);
                  }
                } catch {
                  // Fall back to provider models
                }
              }
              const savedAcc = savedAccountDefaults[account.id];
              const accModelId =
                savedAcc?.model !== undefined
                  ? savedAcc.model
                  : account.id === 'default'
                    ? modelId
                    : '';
              const accModel = accModels.find((m) => m.id === accModelId);
              const accSavedEffort =
                typeof savedAcc?.options?.[EFFORT_OPTION] === 'string'
                  ? (savedAcc.options[EFFORT_OPTION] as string)
                  : account.id === 'default'
                    ? savedEffort
                    : '';
              const resolvedAcc = resolveEffort(accModel, accSavedEffort);

              accountDefaults[account.id] = {
                models: accModels,
                modelId: accModelId,
                effortId: resolvedAcc.effortId,
                effortChoices: resolvedAcc.choices,
              };
            }),
          );

          return {
            provider,
            models,
            modelId,
            enabled: Boolean(settings.enabled),
            effortId,
            effortChoices: choices,
            accountDefaults,
          };
        }),
      );

      setEntries(items);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  useEffect(() => {
    if (!isOpen) return;
    void refresh();
  }, [isOpen, refresh]);

  const toggle = useCallback(async (providerId: string, enabled: boolean) => {
    setSaving(providerId);
    try {
      const current = await GetProviderSettings(providerId);
      await UpdateProviderSettings(
        providerId,
        domain.ProviderSettings.createFrom({ ...current, enabled }),
      );

      setEntries((prev) =>
        prev.map((entry) =>
          entry.provider.id === providerId ? { ...entry, enabled } : entry,
        ),
      );

      await useOrchestratorStore.getState().loadProviders(true);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(null);
    }
  }, []);

  const selectForAccount = useCallback(
    async (providerId: string, accountId: string, modelId: string) => {
      const savingKey = `${providerId}:${accountId}`;
      setSaving(savingKey);
      try {
        const current = await GetProviderSettings(providerId);
        const entry = entries.find((item) => item.provider.id === providerId);
        const accEntry = entry?.accountDefaults[accountId];
        const modelList = accEntry?.models ?? entry?.models ?? [];
        const model = modelList.find((m) => m.id === modelId);
        const existingAcc = current.accountDefaults?.[accountId];
        const previousEffort =
          typeof existingAcc?.options?.[EFFORT_OPTION] === 'string'
            ? (existingAcc.options[EFFORT_OPTION] as string)
            : accEntry?.effortId ?? '';
        const { effortId } = resolveEffort(model, previousEffort);
        const accOptions: Record<string, unknown> = { ...(existingAcc?.options ?? {}) };
        if (model && effortDescriptor(model) && effortId) {
          accOptions[EFFORT_OPTION] = effortId;
        } else {
          delete accOptions[EFFORT_OPTION];
        }

        const nextAccountDefaults: Record<string, domain.AccountModelDefault> = {
          ...(current.accountDefaults ?? {}),
          [accountId]: domain.AccountModelDefault.createFrom({
            model: modelId,
            options: accOptions,
          }),
        };

        const patch: Record<string, unknown> = {
          ...current,
          accountDefaults: nextAccountDefaults,
        };
        if (accountId === 'default') {
          patch.model = modelId;
          patch.options = accOptions;
        }

        await UpdateProviderSettings(
          providerId,
          domain.ProviderSettings.createFrom(patch),
        );

        setEntries((prev) =>
          prev.map((item) => {
            if (item.provider.id !== providerId) return item;
            const prevAcc = item.accountDefaults[accountId];
            const modelsForAcc = prevAcc?.models ?? item.models;
            const nextModel = modelsForAcc.find((m) => m.id === modelId);
            const resolved = resolveEffort(nextModel, effortId);
            const updatedAccDefaults = {
              ...item.accountDefaults,
              [accountId]: {
                models: modelsForAcc,
                modelId,
                effortId: resolved.effortId,
                effortChoices: resolved.choices,
              },
            };
            if (accountId === 'default') {
              return {
                ...item,
                modelId,
                effortId: resolved.effortId,
                effortChoices: resolved.choices,
                accountDefaults: updatedAccDefaults,
              };
            }
            return { ...item, accountDefaults: updatedAccDefaults };
          }),
        );

        const store = useOrchestratorStore.getState();
        store.setPreferredAccountModel(providerId, accountId, modelId, effortId);
        if (accountId === 'default') {
          store.setPreferredModel(providerId, modelId);
        }
        if (
          modelId &&
          store.selectedProviderId === providerId &&
          store.shownAccount(providerId) === accountId
        ) {
          store.selectModelSilently(modelId);
        }
        if (model && effortId) {
          const label = effortLabel(model, effortId);
          if (label) store.setModelSettings(modelId, { effort: label });
        }
        setError(null);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        setSaving(null);
      }
    },
    [entries],
  );

  const selectEffortForAccount = useCallback(
    async (providerId: string, accountId: string, effortId: string) => {
      const savingKey = `${providerId}:${accountId}`;
      setSaving(savingKey);
      try {
        const current = await GetProviderSettings(providerId);
        const existingAcc = current.accountDefaults?.[accountId];
        const entry = entries.find((item) => item.provider.id === providerId);
        const accEntry = entry?.accountDefaults[accountId];
        const modelId = existingAcc?.model ?? accEntry?.modelId ?? (accountId === 'default' ? current.model ?? '' : '');
        const accOptions = { ...(existingAcc?.options ?? {}), [EFFORT_OPTION]: effortId };

        const nextAccountDefaults: Record<string, domain.AccountModelDefault> = {
          ...(current.accountDefaults ?? {}),
          [accountId]: domain.AccountModelDefault.createFrom({
            model: modelId,
            options: accOptions,
          }),
        };

        const patch: Record<string, unknown> = {
          ...current,
          accountDefaults: nextAccountDefaults,
        };
        if (accountId === 'default') {
          patch.options = { ...(current.options ?? {}), [EFFORT_OPTION]: effortId };
        }

        await UpdateProviderSettings(
          providerId,
          domain.ProviderSettings.createFrom(patch),
        );

        setEntries((prev) =>
          prev.map((item) => {
            if (item.provider.id !== providerId) return item;
            const prevAcc = item.accountDefaults[accountId];
            const updatedAccDefaults = prevAcc
              ? {
                  ...item.accountDefaults,
                  [accountId]: { ...prevAcc, effortId },
                }
              : item.accountDefaults;
            if (accountId === 'default') {
              return { ...item, effortId, accountDefaults: updatedAccDefaults };
            }
            return { ...item, accountDefaults: updatedAccDefaults };
          }),
        );

        const store = useOrchestratorStore.getState();
        store.setPreferredAccountModel(providerId, accountId, modelId, effortId);
        const modelList = accEntry?.models ?? entry?.models ?? [];
        const model = modelList.find((m) => m.id === modelId);
        const label = model ? effortLabel(model, effortId) : null;
        if (model && label) {
          store.setModelSettings(model.id, { effort: label });
        }
        setError(null);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        setSaving(null);
      }
    },
    [entries],
  );

  const select = useCallback(
    async (providerId: string, modelId: string) => {
      await selectForAccount(providerId, 'default', modelId);
    },
    [selectForAccount],
  );

  const selectEffort = useCallback(
    async (providerId: string, effortId: string) => {
      await selectEffortForAccount(providerId, 'default', effortId);
    },
    [selectEffortForAccount],
  );

  return {
    entries,
    error,
    isSaving,
    select,
    selectEffort,
    selectForAccount,
    selectEffortForAccount,
    toggle,
  };
}
