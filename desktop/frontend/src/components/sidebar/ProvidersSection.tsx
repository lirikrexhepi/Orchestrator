import React, { useEffect, useState } from 'react';
import { DefaultModels } from './useDefaultModels';
import { useProviderAccounts } from './useProviderAccounts';
import { ClaudeUpdateState } from '../orchestrator/useClaudeUpdate';
import { useUsage } from './useUsage';
import { ProviderCard } from './ProviderCard';

export interface ProvidersSectionProps {
  defaultModels: DefaultModels;
  claudeUpdate: ClaudeUpdateState;
  isLight: boolean;
  active?: boolean;
}

/**
 * One "Providers" section: a single card per CLI with its model and effort
 * picks, accounts and usage together. Replaces the old split across the
 * Providers & Models, Accounts and Usage sections.
 */
export const ProvidersSection: React.FC<ProvidersSectionProps> = ({
  defaultModels,
  claudeUpdate,
  isLight,
  active = true,
}) => {
  const accounts = useProviderAccounts();
  const [visible, setVisible] = useState(() => (typeof document === 'undefined' ? true : !document.hidden));
  useEffect(() => {
    const onVisibility = () => setVisible(!document.hidden);
    document.addEventListener('visibilitychange', onVisibility);
    return () => document.removeEventListener('visibilitychange', onVisibility);
  }, []);
  const usage = useUsage(active && visible);

  return (
    <>
      {defaultModels.error && (
        <div className="px-3 py-2 rounded-[9px] bg-amber-500/10 border border-amber-400/25">
          <span className="text-[11px] font-medium font-(family-name:--app-font) text-amber-100/90 leading-relaxed">
            {defaultModels.error}
          </span>
        </div>
      )}

      <div className="flex flex-col gap-2.5">
        {defaultModels.entries.map((entry) => {
          const snapshot = accounts.snapshots.find((s) => s.driver === entry.provider.id);
          const driverUsage = (usage.report?.drivers ?? []).filter((d) => d.driver === entry.provider.id);
          return (
            <ProviderCard
              key={entry.provider.id}
              entry={entry}
              defaultModels={defaultModels}
              accounts={accounts}
              snapshot={snapshot}
              usage={driverUsage}
              claudeUpdate={entry.provider.id === 'claude' ? claudeUpdate : null}
              isLight={isLight}
            />
          );
        })}
      </div>

      {accounts.notice && (
        <div className={`text-[10px] font-medium font-(family-name:--app-font) px-2 py-1 rounded-[6px] ${isLight ? 'bg-black/[0.05] text-black/70' : 'bg-white/[0.06] text-white/70'}`}>
          {accounts.notice}
        </div>
      )}
      {accounts.error && (
        <div className={`text-[10px] font-medium font-(family-name:--app-font) px-2 py-1 rounded-[6px] ${isLight ? 'bg-red-500/10 text-red-800 border border-red-500/20' : 'bg-red-500/15 text-red-200 border border-red-500/25'}`}>
          {accounts.error}
        </div>
      )}
      {usage.error && (
        <div className={`text-[10px] font-medium font-(family-name:--app-font) px-2 py-1 rounded-[6px] ${isLight ? 'bg-red-500/10 text-red-800 border border-red-500/20' : 'bg-red-500/15 text-red-200 border border-red-500/25'}`}>
          {usage.error}
        </div>
      )}
    </>
  );
};
