import { create } from 'zustand';
import { AIModel, CLIProvider, ModelSettings, ThinkingEffort, ThinkingMode } from './types';
import { DEFAULT_MODELS, DEFAULT_PROVIDERS, toModel, toProvider } from './orchestratorData';
import { ListProviders } from '../../../wailsjs/go/main/App';

export interface FocusedSession {
  threadId: string;
  providerId: string;
  account: string;
}

interface PendingAccount {
  providerId: string;
  account: string;
}

interface OrchestratorStore {
  providers: CLIProvider[];
  models: AIModel[];
  selectedProviderId: string;
  selectedModelId: string;
  preferredModels: Record<string, string>;
  modelSettings: Record<string, ModelSettings>;
  messageText: string;
  isModelPickerOpen: boolean;
  isEffortPickerOpen: boolean;
  activeConfiguringModelId: string | null;
  isLoadingProviders: boolean;
  providersError: string | null;
  autoStartAgents: boolean;
  autoApprovePermissions: boolean;
  interfaceSounds: boolean;
  showProjectFavicons: boolean;
  chatsFullscreen: boolean;
  accountProject: string;
  chosenAccounts: Record<string, string>;
  projectAccounts: Record<string, string>;
  focusedSession: FocusedSession | null;
  pendingAccounts: Record<string, PendingAccount>;

  // Actions
  setAccountProject: (projectPath: string, defaults?: Record<string, string>) => void;
  selectAccount: (providerId: string, accountId: string) => void;
  accountFor: (providerId: string, projectPath?: string) => string;
  shownAccount: (providerId: string) => string;
  focusSession: (session: FocusedSession | null) => void;
  pendingAccount: (threadId: string, providerId: string) => string;
  clearPendingAccount: (threadId: string) => void;
  setAutoStartAgents: (enabled: boolean) => void;
  setAutoApprovePermissions: (enabled: boolean) => void;
  setInterfaceSounds: (enabled: boolean) => void;
  setShowProjectFavicons: (enabled: boolean) => void;
  setChatsFullscreen: (enabled: boolean) => void;
  loadProviders: (force?: boolean) => Promise<void>;
  setProviders: (providers: CLIProvider[]) => void;
  setModels: (models: AIModel[]) => void;
  setPreferredModel: (providerId: string, modelId: string) => void;
  addProvider: (provider: CLIProvider) => void;
  addModel: (model: AIModel) => void;
  selectProvider: (providerId: string) => void;
  selectModel: (modelId: string) => void;
  /** Selects without opening the effort picker, for changes the user did not
   *  make from the picker itself. */
  selectModelSilently: (modelId: string) => void;
  setModelSettings: (modelId: string, settings: Partial<ModelSettings>) => void;
  setMessageText: (text: string) => void;
  setModelPickerOpen: (open: boolean) => void;
  setEffortPickerOpen: (open: boolean) => void;
  toggleModelPicker: () => void;
  closeAllPickers: () => void;
  openEffortForModel: (modelId: string) => void;

  // Selectors / Helpers
  getSelectedModel: () => AIModel | undefined;
  getSelectedProvider: () => CLIProvider | undefined;
  getConfiguringModel: () => AIModel | undefined;
  getModelsForProvider: (providerId: string) => AIModel[];
  getCurrentModelSettings: (modelId?: string) => ModelSettings;
}

const initialSettings: Record<string, ModelSettings> = {};

const normalizePath = (path: string) => path.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();

const samePath = (a: string, b: string) => normalizePath(a) === normalizePath(b);

export const useOrchestratorStore = create<OrchestratorStore>((set, get) => ({
  providers: DEFAULT_PROVIDERS,
  models: DEFAULT_MODELS,
  selectedProviderId: '',
  selectedModelId: '',
  preferredModels: {},
  modelSettings: initialSettings,
  messageText: '',
  isModelPickerOpen: false,
  isEffortPickerOpen: false,
  activeConfiguringModelId: null,
  isLoadingProviders: false,
  providersError: null,
  autoStartAgents:
    typeof window !== 'undefined'
      ? localStorage.getItem('orchestrator_auto_start_agents') === 'true'
      : false,
  autoApprovePermissions:
    typeof window !== 'undefined'
      ? localStorage.getItem('orchestrator_auto_approve_permissions') !== 'false'
      : true,
  interfaceSounds:
    typeof window !== 'undefined'
      ? localStorage.getItem('orchestrator_interface_sounds') !== 'false'
      : true,
  showProjectFavicons:
    typeof window !== 'undefined'
      ? localStorage.getItem('orchestrator_show_project_favicons') === 'true'
      : false,
  chatsFullscreen:
    typeof window !== 'undefined'
      ? localStorage.getItem('orchestrator_chats_fullscreen') === 'true'
      : false,
  accountProject: '',
  chosenAccounts: {},
  projectAccounts: {},
  focusedSession: null,
  pendingAccounts: {},

  setAccountProject: (projectPath, defaults = {}) => {
    const same = samePath(get().accountProject, projectPath);
    set({
      accountProject: projectPath,
      chosenAccounts: same ? get().chosenAccounts : {},
      projectAccounts: { ...defaults },
    });
  },

  selectAccount: (providerId, accountId) =>
    set((state) => {
      const focused = state.focusedSession;
      if (!focused) return { chosenAccounts: { ...state.chosenAccounts, [providerId]: accountId } };
      return { pendingAccounts: { ...state.pendingAccounts, [focused.threadId]: { providerId, account: accountId } } };
    }),

  accountFor: (providerId, projectPath) => {
    const { accountProject, chosenAccounts } = get();
    if (projectPath !== undefined && !samePath(accountProject, projectPath)) return '';
    return chosenAccounts[providerId] || '';
  },

  shownAccount: (providerId) => {
    const { chosenAccounts, projectAccounts, providers, focusedSession, pendingAccounts } = get();
    const accounts = providers.find((p) => p.id === providerId)?.accounts ?? [];
    const pending = focusedSession ? pendingAccounts[focusedSession.threadId] : undefined;
    const wanted =
      (pending?.providerId === providerId && pending.account) ||
      (focusedSession?.providerId === providerId && (focusedSession.account || 'default')) ||
      chosenAccounts[providerId] ||
      projectAccounts[providerId] ||
      'default';
    return accounts.some((account) => account.id === wanted) ? wanted : 'default';
  },

  focusSession: (session) => {
    const current = get().focusedSession;
    if (
      current?.threadId === session?.threadId &&
      current?.providerId === session?.providerId &&
      current?.account === session?.account
    ) {
      return;
    }
    set({ focusedSession: session });
  },

  pendingAccount: (threadId, providerId) => {
    const pending = get().pendingAccounts[threadId];
    return pending?.providerId === providerId ? pending.account : '';
  },

  clearPendingAccount: (threadId) =>
    set((state) => {
      if (!state.pendingAccounts[threadId]) return state;
      const next = { ...state.pendingAccounts };
      delete next[threadId];
      return { pendingAccounts: next };
    }),

  setAutoStartAgents: (autoStartAgents: boolean) => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('orchestrator_auto_start_agents', autoStartAgents ? 'true' : 'false');
    }
    set({ autoStartAgents });
  },

  setAutoApprovePermissions: (autoApprovePermissions: boolean) => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('orchestrator_auto_approve_permissions', autoApprovePermissions ? 'true' : 'false');
    }
    set({ autoApprovePermissions });
  },

  setInterfaceSounds: (interfaceSounds: boolean) => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('orchestrator_interface_sounds', interfaceSounds ? 'true' : 'false');
    }
    set({ interfaceSounds });
  },

  setChatsFullscreen: (chatsFullscreen: boolean) => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('orchestrator_chats_fullscreen', chatsFullscreen ? 'true' : 'false');
    }
    set({ chatsFullscreen });
  },

  setShowProjectFavicons: (showProjectFavicons: boolean) => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('orchestrator_show_project_favicons', showProjectFavicons ? 'true' : 'false');
    }
    set({ showProjectFavicons });
  },

  // Discovers installed CLIs and their models. Only ready providers are
  // selectable; the current selection is preserved across refreshes when it
  // still exists.
  loadProviders: async (force = false) => {
    set({ isLoadingProviders: true });
    try {
      const snapshots = await ListProviders(force);
      const ready = snapshots.filter(
        (snapshot) => snapshot.availability === 'ready' && snapshot.settings?.enabled,
      );

      const providers = ready.map(toProvider);
      const models = ready.flatMap((snapshot) =>
        (snapshot.models ?? []).map((model) => toModel(snapshot, model)),
      );

      const settings = { ...get().modelSettings };
      const savedEffortByModel = new Map<string, string>();
      for (const snapshot of ready) {
        const savedModel = snapshot.settings?.model;
        const savedEffort = (snapshot.settings as unknown as { options?: Record<string, unknown> })
          ?.options?.['effort'];
        if (typeof savedModel === 'string' && savedModel !== '' && typeof savedEffort === 'string' && savedEffort !== '') {
          savedEffortByModel.set(savedModel, savedEffort);
        }
      }
      for (const model of models) {
        if (!settings[model.id]) {
          const savedEffortId = savedEffortByModel.get(model.id);
          const savedLabel = savedEffortId
            ? model.options
                ?.find((option) => option.id === 'effort')
                ?.choices?.find((choice) => choice.id === savedEffortId)?.label
            : undefined;
          settings[model.id] = {
            effort: savedLabel || model.defaultEffort || model.effortLevels?.[0] || 'Medium',
            mode: model.defaultMode || 'normal',
          };
        }
      }

      const previous = get().selectedModelId;
      const keep = models.find((model) => model.id === previous);

      const preferredModels: Record<string, string> = {};
      for (const snapshot of ready) {
        if (snapshot.settings?.model) {
          preferredModels[snapshot.driver] = snapshot.settings.model;
        }
      }

      // A model the user pinned in Settings wins the initial choice. `defaultModel`
      // comes from the provider's saved settings, so a fresh run opens on the CLI
      // and model they chose rather than on whichever happens to sort first.
      const preferred = ready
        .map((snapshot) => snapshot.settings?.model)
        .filter((id): id is string => !!id)
        .map((id) => models.find((model) => model.id === id))
        .find((model): model is typeof models[number] => !!model);

      const fallback =
        preferred ?? models.find((model) => model.providerId === 'claude') ?? models[0];
      const selected = keep || fallback;

      set({
        providers,
        models,
        preferredModels,
        modelSettings: settings,
        selectedModelId: selected?.id || '',
        selectedProviderId: selected?.providerId || providers[0]?.id || '',
        isLoadingProviders: false,
        providersError: snapshots.length === 0 ? 'No agent CLIs detected' : null,
      });
    } catch (cause) {
      set({
        isLoadingProviders: false,
        providersError: cause instanceof Error ? cause.message : String(cause),
      });
    }
  },

  setProviders: (providers) => set({ providers }),
  setModels: (models) => set({ models }),
  setPreferredModel: (providerId, modelId) =>
    set((state) => ({
      preferredModels: { ...state.preferredModels, [providerId]: modelId },
    })),

  addProvider: (provider) =>
    set((state) => ({
      providers: [...state.providers.filter((p) => p.id !== provider.id), provider],
    })),

  addModel: (model) =>
    set((state) => {
      const exists = state.models.some((m) => m.id === model.id);
      return {
        models: exists ? state.models.map((m) => (m.id === model.id ? model : m)) : [...state.models, model],
        modelSettings: {
          ...state.modelSettings,
          [model.id]: state.modelSettings[model.id] || {
            effort: model.defaultEffort || 'Medium',
            mode: model.defaultMode || (model.supportsThinking ? 'thinking' : 'normal'),
          },
        },
      };
    }),

  selectProvider: (providerId) => {
    const providerModels = get().models.filter((m) => m.providerId === providerId);
    const preferredId = get().preferredModels[providerId];
    const preferredModel = preferredId ? providerModels.find((m) => m.id === preferredId) : undefined;
    const newSelectedModelId = preferredModel?.id || providerModels[0]?.id || get().selectedModelId;
    set({
      selectedProviderId: providerId,
      selectedModelId: newSelectedModelId,
      activeConfiguringModelId: newSelectedModelId,
    });
  },

  selectModel: (modelId) => {
    const model = get().models.find((m) => m.id === modelId);
    if (!model) return;
    const { selectedModelId, isEffortPickerOpen } = get();
    const hasEfforts = Boolean(
      (model.effortLevels && model.effortLevels.length > 0) || model.supportsThinking,
    );

    // Toggle effort modal: if clicking the active model and effort modal is open, close it
    if (selectedModelId === modelId && isEffortPickerOpen) {
      set({ isEffortPickerOpen: false });
    } else {
      set({
        selectedModelId: modelId,
        selectedProviderId: model.providerId,
        activeConfiguringModelId: modelId,
        isEffortPickerOpen: hasEfforts,
      });
    }
  },

  selectModelSilently: (modelId) => {
    const model = get().models.find((m) => m.id === modelId);
    if (!model) return;
    set({ selectedModelId: modelId, selectedProviderId: model.providerId });
  },

  openEffortForModel: (modelId) => {
    const model = get().models.find((m) => m.id === modelId);
    const hasEfforts = Boolean(
      model && ((model.effortLevels && model.effortLevels.length > 0) || model.supportsThinking),
    );
    if (!hasEfforts) return;
    set({
      activeConfiguringModelId: modelId,
      isEffortPickerOpen: true,
    });
  },

  setModelSettings: (modelId, newSettings) => {
    set((state) => ({
      modelSettings: {
        ...state.modelSettings,
        [modelId]: {
          ...(state.modelSettings[modelId] || { effort: 'Medium', mode: 'normal' }),
          ...newSettings,
        },
      },
    }));
  },

  setMessageText: (messageText) => set({ messageText }),

  setModelPickerOpen: (isModelPickerOpen) =>
    set((state) => ({
      isModelPickerOpen,
      isEffortPickerOpen: isModelPickerOpen ? state.isEffortPickerOpen : false,
    })),

  setEffortPickerOpen: (isEffortPickerOpen) => {
    if (isEffortPickerOpen) {
      const activeId = get().activeConfiguringModelId || get().selectedModelId;
      const model = get().models.find((m) => m.id === activeId);
      const hasEfforts = Boolean(
        model && ((model.effortLevels && model.effortLevels.length > 0) || model.supportsThinking),
      );
      if (!hasEfforts) {
        set({ isEffortPickerOpen: false });
        return;
      }
    }
    set({ isEffortPickerOpen });
  },

  toggleModelPicker: () =>
    set((state) => {
      const nextOpen = !state.isModelPickerOpen;
      return {
        isModelPickerOpen: nextOpen,
        isEffortPickerOpen: nextOpen ? false : false,
        activeConfiguringModelId: nextOpen ? state.selectedModelId : null,
      };
    }),

  closeAllPickers: () =>
    set({
      isModelPickerOpen: false,
      isEffortPickerOpen: false,
    }),

  getSelectedModel: () => {
    const { models, selectedModelId } = get();
    return models.find((m) => m.id === selectedModelId) || models[0];
  },

  getSelectedProvider: () => {
    const { providers, selectedProviderId } = get();
    return providers.find((p) => p.id === selectedProviderId) || providers[0];
  },

  getConfiguringModel: () => {
    const { models, activeConfiguringModelId, selectedModelId } = get();
    const id = activeConfiguringModelId || selectedModelId;
    return models.find((m) => m.id === id) || models[0];
  },

  getModelsForProvider: (providerId) => {
    return get().models.filter((m) => m.providerId === providerId);
  },

  getCurrentModelSettings: (modelId) => {
    const id = modelId || get().selectedModelId;
    const model = get().models.find((m) => m.id === id);
    const existing = get().modelSettings[id];
    if (existing) return existing;
    return {
      effort: model?.defaultEffort || 'Medium',
      mode: model?.defaultMode || (model?.supportsThinking ? 'thinking' : 'normal'),
    };
  },
}));
