import { MaterialIcon } from '../common/icons';
import React, { useEffect, useState } from 'react';
import { motion } from 'motion/react';
import { useOrchestratorStore } from './useOrchestratorStore';
import { LiquidGlass } from '../../liquid-glass';
import { ScrollArea } from '../common/ScrollArea';
import { AIModel, CLIProvider } from './types';
import { providerIcon } from './providerIcons';
import { useTheme } from '../../themes';
import { ActiveProject } from '../../../wailsjs/go/main/App';
import { CLIAccount } from './types';

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
            className={`h-[26px] max-w-full px-2.5 rounded-[8px] border text-[12px] font-medium font-(family-name:--app-font) tracking-tight truncate transition-all duration-150 cursor-pointer active:scale-[0.97] ${
              selected
                ? 'bg-white/[0.12] border-white/[0.10] text-white'
                : 'border-transparent text-white/60 hover:text-white hover:bg-white/[0.06]'
            }`}
          >
            {account.name}
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
  onSelectModel?: (modelId: string) => void;
  onSelectProvider?: (providerId: string) => void;
  isFloatingPopup?: boolean;
}

interface ModelRowsProps {
  models: AIModel[];
  providers: CLIProvider[];
  selectedModelId: string;
  onSelect: (modelId: string) => void;
  scrollable: boolean;
  isLight?: boolean;
}

const ModelRows: React.FC<ModelRowsProps> = ({
  models,
  providers,
  selectedModelId,
  onSelect,
  scrollable,
  isLight,
}) => {
  const rows = models.map((model) => {
    const isSelected = model.id === selectedModelId;
    const provider = providers.find((p) => p.id === model.providerId);
    const iconSrc = providerIcon(model.providerId, isLight) || model.icon || provider?.icon;

    return (
      <button
        key={model.id}
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          onSelect(model.id);
        }}
        className={`relative w-full h-[36px] shrink-0 text-left px-2.5 rounded-[9px] flex items-center gap-2.5 border transition-all duration-150 cursor-pointer active:scale-[0.98] ${
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
        <span className="text-[13px] font-medium font-(family-name:--app-font) tracking-tight truncate">
          {model.name}
        </span>
      </button>
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
}) => {
  const store = useOrchestratorStore();

  const selectedModelId = propModelId !== undefined ? propModelId : store.selectedModelId;
  const [localProviderId, setLocalProviderId] = useState<string | null>(null);

  const selectedProviderId =
    propProviderId !== undefined
      ? (localProviderId || propProviderId)
      : store.selectedProviderId;

  const currentModels = store.getModelsForProvider(selectedProviderId);
  const choosesAccount = propModelId === undefined && propProviderId === undefined;
  const accounts = store.providers.find((p) => p.id === selectedProviderId)?.accounts ?? [];
  const setAccountProject = store.setAccountProject;
  const [confirmAccount, setConfirmAccount] = useState<string | null>(null);
  const focusedSession = store.focusedSession;
  const sessionAccount =
    focusedSession && focusedSession.providerId === selectedProviderId ? focusedSession.account || 'default' : null;
  const accountLabel = (id: string) => accounts.find((account) => account.id === id)?.name || id;

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
  const selectedProviderIndex = store.providers.findIndex((p) => p.id === selectedProviderId);

  const handleSelectModel = (modelId: string) => {
    if (propOnSelectModel) {
      propOnSelectModel(modelId);
    } else {
      store.selectModel(modelId);
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

  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';

  return (
    <LiquidGlass
      variant="panel"
      surface="squircle"
      radius={18}
      bezelWidth={16}
      glassThickness={28}
      refractionScale={0.5}
      blur={0.5}
      frost={24}
      frostSaturation={isLight ? 110 : 130}
      specularOpacity={isLight ? 0.3 : 0.06}
      tint="var(--theme-input-bg, rgba(10, 10, 10, 0.96))"
      shadow={isLight ? 'subtle' : 'apple'}
      border="1px solid var(--theme-input-border, rgba(255, 255, 255, 0.08))"
      className="w-[230px] p-2 text-white shadow-2xl"
      style={{
        boxShadow: isLight
          ? '0 20px 48px -6px rgba(0, 0, 0, 0.15), 0 6px 18px rgba(0, 0, 0, 0.08), inset 0 1px 0 rgba(255, 255, 255, 0.9)'
          : '0 24px 50px -6px rgba(0, 0, 0, 0.82), 0 8px 24px rgba(0, 0, 0, 0.55), inset 0 1px 0 rgba(255, 255, 255, 0.09)',
      }}
    >
      {/* The selected row paints its own highlight. */}
      <ModelRows
        models={currentModels}
        providers={store.providers}
        selectedModelId={selectedModelId}
        onSelect={handleSelectModel}
        scrollable={hasMoreThan5}
        isLight={isLight}
      />

      {choosesAccount && accounts.length > 1 && (
        <>
          <div className="h-[1px] bg-white/[0.08] my-1.5 mx-1" />
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
      {store.providers.length > 0 && <div className="h-[1px] bg-white/[0.08] my-1.5 mx-1" />}

      {/* CLI Provider Switcher Bar with Fluid Motion Sliding Indicator */}
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
          No providers enabled
        </div>
      )}
    </LiquidGlass>
  );
};
