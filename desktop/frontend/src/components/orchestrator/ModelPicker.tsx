import { Check, MaterialIcon } from '../common/icons';
import React, { useEffect, useState, useRef } from 'react';
import { motion } from 'motion/react';
import { useOrchestratorStore } from './useOrchestratorStore';
import { GlassSquircle } from '../../ui/glass/GlassSquircle';
import { ScrollArea } from '../common/ScrollArea';
import { AIModel, CLIProvider } from './types';
import { providerIcon } from './providerIcons';
import { useTheme } from '../../themes';
import { ActiveProject, GetProviderSettings, UpdateProviderSettings } from '../../../wailsjs/go/main/App';
import { domain } from '../../../wailsjs/go/models';
import { CLIAccount } from './types';
import { toModelOptions } from './orchestratorData';

interface AccountRowProps {
  accounts: CLIAccount[];
  selectedId: string;
  onSelect: (accountId: string) => void;
  isLight?: boolean;
}

const AccountRow: React.FC<AccountRowProps> = ({ accounts, selectedId, onSelect, isLight }) => (
  <div className="flex flex-col gap-1 pt-0.5">
    <span className={`px-1 text-[10px] font-semibold font-(family-name:--app-font) tracking-tight uppercase ${isLight ? 'text-black/40' : 'text-white/40'}`}>
      Account
    </span>
    <div className="flex flex-wrap gap-1" role="radiogroup" aria-label="Account">
      {accounts.map((account) => {
        const selected = account.id === selectedId;
        return (
          <button
            key={account.id}
            type="button"
            role="radio"
            aria-checked={selected}
            onClick={(event) => {
              event.stopPropagation();
              onSelect(account.id);
            }}
            className={`h-[26px] max-w-full px-2.5 rounded-[8px] border text-[12px] font-medium font-(family-name:--app-font) tracking-tight truncate transition-all duration-150 cursor-pointer active:scale-[0.97] flex items-center gap-1.5 ${
              selected
                ? 'bg-white/[0.12] border-white/[0.10] text-white'
                : 'border-transparent text-white/60 hover:text-white hover:bg-white/[0.06]'
            }`}
          >
            <span>{account.name}</span>
            {account.authMode && (
              <span className="text-[9px] font-normal px-1 py-0.2 rounded uppercase opacity-60">
                {account.authMode === 'api_key' ? 'API' : 'Keyring'}
              </span>
            )}
          </button>
        );
      })}
    </div>
  </div>
);

interface AccountSwitchConfirmProps {
  current: string;
  next: string;
  onCancel: () => void;
  onConfirm: () => void;
}

const AccountSwitchConfirm: React.FC<AccountSwitchConfirmProps> = ({ current, next, onCancel, onConfirm }) => (
  <div
    onClick={(event) => event.stopPropagation()}
    className="flex flex-col gap-2 rounded-[10px] border border-amber-400/25 bg-amber-400/[0.07] p-2"
  >
    <div className="flex gap-1.5 text-[11.5px] leading-snug font-(family-name:--app-font) text-white/80">
      <span className="mt-[1px] inline-flex shrink-0 text-amber-400">
        <MaterialIcon name="warning" size={14} />
      </span>
      <span>
        This chat is using <span className="font-semibold text-white">{current}</span>. Switch it to{' '}
        <span className="font-semibold text-white">{next}</span>? It restarts on {next} with the conversation handed
        over.
      </span>
    </div>
    <div className="flex justify-end gap-1">
      <button
        type="button"
        onClick={onCancel}
        className="h-[24px] px-2.5 rounded-[7px] text-[11.5px] font-medium font-(family-name:--app-font) text-white/60 hover:text-white hover:bg-white/[0.08] transition-colors cursor-pointer"
      >
        Cancel
      </button>
      <button
        type="button"
        onClick={onConfirm}
        className="h-[24px] px-2.5 rounded-[7px] text-[11.5px] font-semibold font-(family-name:--app-font) bg-amber-400/90 text-black hover:bg-amber-300 transition-colors cursor-pointer active:scale-[0.97]"
      >
        Switch to {next}
      </button>
    </div>
  </div>
);

export interface ModelPickerProps {
  selectedModelId?: string;
  selectedProviderId?: string;
  onSelectModel?: (modelId: string, providerId?: string) => void;
  onSelectProvider?: (providerId: string) => void;
  isFloatingPopup?: boolean;
  choosesAccount?: boolean;
}

interface ModelRowsProps {
  models: AIModel[];
  providers: CLIProvider[];
  selectedModelId: string;
  selectedProviderId: string;
  defaultModelId?: string;
  accountName?: string;
  onSelect: (modelId: string, providerId: string) => void;
  onToggleDefault?: (model: AIModel) => void;
  scrollable: boolean;
  isLight?: boolean;
  isEffortOpen?: boolean;
}

const ModelRows: React.FC<ModelRowsProps> = ({
  models,
  providers,
  selectedModelId,
  selectedProviderId,
  defaultModelId,
  accountName,
  onSelect,
  onToggleDefault,
  scrollable,
  isLight,
  isEffortOpen,
}) => {
  if (models.length === 0) {
    return (
      <div className="py-4 text-center text-[12px] text-white/40 font-(family-name:--app-font)">
        No models available
      </div>
    );
  }

  const rows = models.map((model) => {
    const isSelected = model.id === selectedModelId && model.providerId === selectedProviderId;
    const isDefault = Boolean(defaultModelId && model.id === defaultModelId);
    const provider = providers.find((p) => p.id === model.providerId);
    const iconSrc = providerIcon(model.providerId, isLight) || model.icon || provider?.icon;
    const hasEfforts = Boolean(
      (model.effortLevels && model.effortLevels.length > 0) || model.supportsThinking,
    );

    return (
      <div
        key={`${model.providerId}:${model.id}`}
        role="button"
        tabIndex={0}
        onClick={(event) => {
          event.stopPropagation();
          onSelect(model.id, model.providerId);
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            event.stopPropagation();
            onSelect(model.id, model.providerId);
          }
        }}
        className={`group relative w-full h-[36px] shrink-0 text-left px-2.5 rounded-[9px] flex items-center gap-2.5 border transition-all duration-150 cursor-pointer active:scale-[0.98] ${
          isSelected
            ? 'bg-white/[0.12] border-white/[0.10] text-white font-medium shadow-[inset_0_1px_0_rgba(255,255,255,0.06)]'
            : 'border-transparent text-white/75 hover:text-white hover:bg-white/[0.06]'
        }`}
      >
        {iconSrc && (
          <img
            src={iconSrc}
            alt=""
            className="w-[18px] h-[18px] object-contain shrink-0"
            draggable={false}
          />
        )}
        <span className="text-[13px] font-medium font-(family-name:--app-font) tracking-tight truncate flex-1">
          {model.name}
        </span>
        <div className="flex items-center gap-1 shrink-0">
          {onToggleDefault && (
            <button
              type="button"
              title={
                isDefault
                  ? `Default for ${accountName || 'this account'} (click to unset)`
                  : `Set as default for ${accountName || 'this account'}`
              }
              onClick={(event) => {
                event.stopPropagation();
                onToggleDefault(model);
              }}
              className={`w-[20px] h-[20px] rounded-[6px] flex items-center justify-center transition-all cursor-pointer ${
                isDefault
                  ? 'text-amber-300/90 hover:text-amber-200 opacity-100'
                  : 'text-white/35 hover:text-white/80 opacity-0 group-hover:opacity-100'
              }`}
            >
              <MaterialIcon name={isDefault ? 'star' : 'star_border'} size={14} />
            </button>
          )}
          {isSelected && (
            <>
              <Check size={14} strokeWidth={2.2} className="text-white shrink-0" />
              {hasEfforts && (
                <MaterialIcon
                  name="chevron_right"
                  className={`text-[15px] transition-transform duration-200 ${
                    isEffortOpen ? 'rotate-90 text-white' : 'text-white/40 hover:text-white/80'
                  }`}
                />
              )}
            </>
          )}
        </div>
      </div>
    );
  });

  return scrollable ? (
    <ScrollArea maxHeight={260} className="flex flex-col gap-0.5 pr-0.5">
      {rows}
    </ScrollArea>
  ) : (
    <div className="flex flex-col gap-0.5">{rows}</div>
  );
};

export const ModelPicker: React.FC<ModelPickerProps> = ({
  selectedModelId: propModelId,
  selectedProviderId: propProviderId,
  onSelectModel: propOnSelectModel,
  onSelectProvider: propOnSelectProvider,
  isFloatingPopup = false,
  choosesAccount: propChoosesAccount,
}) => {
  const store = useOrchestratorStore();

  // Automatically trigger provider discovery if empty
  useEffect(() => {
    if (store.providers.length === 0) {
      void store.loadProviders();
    }
  }, [store.providers.length]);

  const selectedModelId = propModelId !== undefined ? propModelId : store.selectedModelId;
  const [localProviderId, setLocalProviderId] = useState<string | null>(null);

  const modelProviderId = store.models.find((m) => m.id === selectedModelId)?.providerId;

  // The explicitly chosen provider wins over the one inferred from the model:
  // Antigravity also serves Claude-named models, so inferring from the model id
  // kept the picker stuck on Antigravity when the Claude tab was clicked.
  const selectedProviderId =
    localProviderId ||
    propProviderId ||
    (!propOnSelectProvider ? store.selectedProviderId : '') ||
    modelProviderId ||
    store.selectedProviderId ||
    store.providers[0]?.id ||
    'antigravity';

  const currentAccount = store.shownAccount(selectedProviderId);
  const currentModels = store.getModelsForProvider(selectedProviderId, currentAccount);
  const choosesAccount = propChoosesAccount !== undefined ? propChoosesAccount : true;
  const accounts = store.providers.find((p) => p.id === selectedProviderId)?.accounts ?? [];
  const setAccountProject = store.setAccountProject;
  const [confirmAccount, setConfirmAccount] = useState<string | null>(null);
  const focusedSession = store.focusedSession;
  const sessionAccount =
    focusedSession && focusedSession.providerId === selectedProviderId ? focusedSession.account || 'default' : null;
  const accountLabel = (id: string) => accounts.find((account) => account.id === id)?.name || id;

  useEffect(() => {
    if (selectedProviderId && currentAccount) {
      void store.loadAccountModels(selectedProviderId, currentAccount);
    }
  }, [selectedProviderId, currentAccount]);

  const handleSelectAccount = (accountId: string) => {
    if (sessionAccount && accountId !== sessionAccount && accountId !== store.shownAccount(selectedProviderId)) {
      setConfirmAccount(accountId);
      return;
    }
    setConfirmAccount(null);
    store.selectAccount(selectedProviderId, accountId);
  };

  useEffect(() => {
    if (!choosesAccount) return;
    let live = true;
    ActiveProject()
      .then((project) => {
        if (live && project?.path) setAccountProject(project.path, project.accounts ?? {});
      })
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [choosesAccount, setAccountProject]);

  const hasMoreThan5 = currentModels.length > 5;

  const handleSelectModel = (modelId: string, providerId: string) => {
    if (propOnSelectModel) {
      propOnSelectModel(modelId, providerId);
    } else {
      store.selectModel(modelId, providerId);
    }
  };

  const handleSelectProvider = (providerId: string) => {
    if (propOnSelectProvider) {
      setLocalProviderId(providerId);
      propOnSelectProvider(providerId);
    } else {
      store.selectProvider(providerId);
    }
  };

  const currentDefaultModelId =
    store.preferredAccountModels[`${selectedProviderId}:${currentAccount}`] ??
    (currentAccount === 'default' ? store.preferredModels[selectedProviderId] : '');

  const handleToggleDefault = async (model: AIModel) => {
    const isAlreadyDefault = currentDefaultModelId === model.id;
    const nextModelId = isAlreadyDefault ? '' : model.id;
    const opts = nextModelId ? toModelOptions(model, store.getCurrentModelSettings(model.id)) : {};
    const effortId = typeof opts.effort === 'string' ? opts.effort : '';

    store.setPreferredAccountModel(selectedProviderId, currentAccount, nextModelId, effortId);
    if (currentAccount === 'default') {
      store.setPreferredModel(selectedProviderId, nextModelId);
    }

    try {
      const current = await GetProviderSettings(selectedProviderId);
      const nextAccountDefaults: Record<string, domain.AccountModelDefault> = {
        ...(current.accountDefaults ?? {}),
        [currentAccount]: domain.AccountModelDefault.createFrom({
          model: nextModelId,
          options: opts,
        }),
      };
      const patch: Record<string, unknown> = {
        ...current,
        accountDefaults: nextAccountDefaults,
      };
      if (currentAccount === 'default') {
        patch.model = nextModelId;
        patch.options = opts;
      }
      await UpdateProviderSettings(selectedProviderId, domain.ProviderSettings.createFrom(patch));
    } catch (err) {
      console.warn('Failed to save default model for account:', err);
    }
  };

  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';

  return (
    <GlassSquircle
      radius={18}
      fill={isLight ? 'rgba(255, 255, 255, 0.96)' : 'linear-gradient(135deg, #0B0A09 0%, #191616 100%)'}
      frost={16}
      border={{
        strokeWidth: 1,
        align: 'inside',
        gradient: isLight
          ? 'linear-gradient(135deg, rgba(0, 0, 0, 0.22) 0%, rgba(0, 0, 0, 0.05) 25%, rgba(0, 0, 0, 0) 45%, rgba(0, 0, 0, 0) 55%, rgba(0, 0, 0, 0.04) 75%, rgba(0, 0, 0, 0.16) 100%)'
          : 'linear-gradient(135deg, rgba(255, 255, 255, 0.40) 0%, rgba(255, 255, 255, 0.08) 25%, rgba(255, 255, 255, 0) 45%, rgba(255, 255, 255, 0) 55%, rgba(255, 255, 255, 0.06) 75%, rgba(255, 255, 255, 0.28) 100%)',
      }}
      onClick={(e) => e.stopPropagation()}
      style={{
        width: 250,
        borderRadius: 18,
        padding: 8,
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
        boxShadow: isLight
          ? '0 16px 40px rgba(0, 0, 0, 0.12)'
          : '0 20px 48px rgba(0, 0, 0, 0.70)',
      }}
    >
      {/* Model List */}
      <ModelRows
        models={currentModels}
        providers={store.providers}
        selectedModelId={selectedModelId}
        selectedProviderId={selectedProviderId}
        defaultModelId={currentDefaultModelId}
        accountName={accountLabel(currentAccount)}
        onSelect={handleSelectModel}
        onToggleDefault={(model) => void handleToggleDefault(model)}
        scrollable={hasMoreThan5}
        isLight={isLight}
        isEffortOpen={store.isEffortPickerOpen}
      />

      {choosesAccount && accounts.length > 1 && (
        <>
          <div className="h-[1px] bg-white/[0.08] my-1 mx-1" />
          {confirmAccount && sessionAccount ? (
            <AccountSwitchConfirm
              current={accountLabel(sessionAccount)}
              next={accountLabel(confirmAccount)}
              onCancel={() => setConfirmAccount(null)}
              onConfirm={() => {
                store.selectAccount(selectedProviderId, confirmAccount);
                setConfirmAccount(null);
              }}
            />
          ) : (
            <AccountRow
              accounts={accounts}
              selectedId={store.shownAccount(selectedProviderId)}
              onSelect={handleSelectAccount}
              isLight={isLight}
            />
          )}
        </>
      )}

      {/* Divider */}
      {store.providers.length > 1 && <div className="h-[1px] bg-white/[0.08] my-1 mx-1" />}

      {/* CLI Provider Switcher Bar */}
      {store.providers.length > 0 ? (
        <div className="relative h-[34px] rounded-[10px] bg-white/[0.04] border border-white/[0.07] p-0.5 flex items-center gap-1">
          {store.providers.map((provider) => {
            const isActive = provider.id === selectedProviderId;

            return (
              <button
                key={provider.id}
                type="button"
                title={provider.name}
                onClick={(e) => {
                  e.stopPropagation();
                  handleSelectProvider(provider.id);
                }}
                className={`relative z-10 flex-1 h-full rounded-[8px] flex items-center justify-center cursor-pointer active:scale-95 transition-all ${
                  isActive ? 'opacity-100' : 'opacity-40 hover:opacity-80'
                }`}
              >
                {isActive && (
                  <motion.div
                    layoutId="model-picker-provider-highlight"
                    className="absolute inset-0 rounded-[8px] bg-white/[0.12] border border-white/[0.08] shadow-[inset_0_1px_0_rgba(255,255,255,0.06)] pointer-events-none"
                    transition={{
                      type: 'spring',
                      stiffness: 440,
                      damping: 30,
                      mass: 0.8,
                    }}
                  />
                )}
                {provider.icon ? (
                  <img
                    src={providerIcon(provider.id, isLight) || provider.icon}
                    alt={provider.name}
                    className="w-[18px] h-[18px] object-contain relative z-10 rounded-[3px]"
                    draggable={false}
                  />
                ) : (
                  <MaterialIcon name="smart_toy" className="text-[18px] text-white/60 relative z-10"/>
                )}
              </button>
            );
          })}
        </div>
      ) : (
        <div className="py-2 text-center text-[11px] text-white/40 font-(family-name:--app-font)">
          {store.isLoadingProviders ? 'Loading providers...' : 'No providers detected'}
        </div>
      )}
    </GlassSquircle>
  );
};
