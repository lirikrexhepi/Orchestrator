import React from 'react';
import { ChevronDown, Plus } from '../common/icons';
import { CleanDropdown } from '../common/CleanDropdown';
import { providerIcon } from '../orchestrator/providerIcons';
import { DefaultModels, ProviderDefault } from './useDefaultModels';
import { COPYABLE, emailFromDetail, ProviderAccounts, statusKey } from './useProviderAccounts';
import { ClaudeUpdateState } from '../orchestrator/useClaudeUpdate';
import { compactTokens, isStale, QuotaBar, since, until, UsageRing } from './usageBars';
import { Switch } from './SettingsParts';
import { domain, session } from '../../../wailsjs/go/models';

export interface ProviderCardProps {
  entry: ProviderDefault;
  defaultModels: DefaultModels;
  accounts: ProviderAccounts;
  snapshot?: domain.ProviderSnapshot;
  usage: session.DriverUsage[];
  claudeUpdate: ClaudeUpdateState | null;
  isLight: boolean;
}

interface AccountRow {
  key: string;
  accountId?: string;
  name: string;
  usage?: session.DriverUsage;
}

function primaryLimit(entry?: session.DriverUsage) {
  return (
    entry?.limits?.find(
      (l) =>
        (l.window === 'five_hour' || l.window.toLowerCase().includes('5h')) &&
        typeof l.usedPercent === 'number',
    ) ??
    entry?.limits?.find((l) => typeof l.usedPercent === 'number') ??
    entry?.limits?.[0]
  );
}

export const ProviderCard: React.FC<ProviderCardProps> = ({
  entry,
  defaultModels,
  accounts,
  snapshot,
  usage,
  claudeUpdate,
  isLight,
}) => {
  const { provider, enabled } = entry;
  const [open, setOpen] = React.useState<string | null>(null);

  const fg = isLight ? 'text-black/90' : 'text-white/90';
  const sub = isLight ? 'text-black/45' : 'text-white/40';
  const link = `text-[11.5px] font-medium tracking-tight transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-default ${
    isLight ? 'text-black/55 hover:text-black' : 'text-white/55 hover:text-white'
  }`;
  const input = `h-[28px] flex-1 min-w-0 px-2.5 rounded-[8px] text-[12px] outline-none border ${
    isLight ? 'bg-white/70 border-black/10 text-black' : 'bg-white/[0.06] border-white/10 text-white'
  }`;

  const driverAccounts = snapshot?.accounts ?? [];
  const projectDefault = accounts.project?.accounts?.[provider.id] || 'default';

  const rows: AccountRow[] = driverAccounts.map((account) => ({
    key: statusKey(provider.id, account.id),
    accountId: account.id,
    name: account.name,
    usage: usage.find((u) => (u.account || 'default') === account.id),
  }));
  for (const u of usage) {
    if (rows.some((row) => row.usage === u)) continue;
    rows.push({ key: `usage-${u.account || 'run'}`, name: u.accountName || 'This run', usage: u });
  }

  const update = provider.id === 'claude' ? claudeUpdate : null;

  return (
    <div className={`flex flex-col rounded-[16px] font-(family-name:--app-font) ${isLight ? 'bg-black/[0.035]' : 'bg-white/[0.04]'}`}>
      <div className="flex items-center gap-2.5 px-3.5 pt-3 pb-2.5">
        <img
          src={providerIcon(provider.id, isLight) || provider.icon}
          alt=""
          draggable={false}
          className="w-[18px] h-[18px] object-contain shrink-0 rounded-[3px]"
        />
        <span className={`text-[13.5px] font-semibold tracking-tight truncate ${fg}`}>{provider.name}</span>
        {update?.status?.available ? (
          <button
            type="button"
            disabled={update.updating}
            onClick={() => void update.startUpdate()}
            className="h-[22px] px-2.5 rounded-full text-[11px] font-medium bg-[#007AFF] hover:bg-[#0A84FF] text-white active:scale-95 transition-all cursor-pointer disabled:opacity-50"
            title={`${update.status.installed} → ${update.status.latest}`}
          >
            {update.updating ? 'Updating…' : 'Update'}
          </button>
        ) : null}
        <span className="flex-1" />
        <Switch on={enabled} onChange={() => void defaultModels.toggle(provider.id, !enabled)} isLight={isLight} label={`Enable ${provider.name}`} />
      </div>

      {update && (update.output || update.error) && (
        <span className={`px-3.5 pb-2.5 text-[11px] leading-snug ${update.error ? 'text-red-300/90' : sub}`}>
          {update.error ?? update.output}
        </span>
      )}

      {rows.length > 0 && (
        <div className={`flex flex-col border-t ${isLight ? 'border-black/[0.06]' : 'border-white/[0.06]'}`}>
          {rows.map((row) => {
            const status = row.accountId ? accounts.statuses[statusKey(provider.id, row.accountId)] : undefined;
            const accountObj = driverAccounts.find((a) => a.id === row.accountId);
            const authMode = accountObj?.authMode;
            const email = emailFromDetail(status?.detail);
            const title = email || row.name;
            const label = email
              ? row.name.toLowerCase() === email.toLowerCase()
                ? 'Signed in'
                : row.name
              : status?.known
                ? status.signedIn
                  ? 'Signed in'
                  : 'Not signed in'
                : status?.checking
                  ? 'Checking…'
                  : '';
            const limit = primaryLimit(row.usage);
            const used = limit?.usedPercent;
            const known = typeof used === 'number';
            const isOpen = open === row.key;
            const renaming = accounts.renaming === row.key;
            const confirming = accounts.confirmRemove === row.key;
            const isDefault = (row.accountId || 'default') === (projectDefault || 'default');
            return (
              <div key={row.key} className="flex flex-col">
                <div className="flex items-center gap-3 px-3.5 py-2.5">
                  <UsageRing used={used} size={36} isLight={isLight} label={known ? `${used}%` : '—'} />
                  {renaming && row.accountId ? (
                    <form
                      className="flex items-center gap-2 flex-1 min-w-0"
                      onSubmit={(event) => {
                        event.preventDefault();
                        void accounts.rename(provider.id, row.accountId!);
                      }}
                    >
                      <input autoFocus value={accounts.renameText} onChange={(event) => accounts.setRenameText(event.target.value)} className={input} />
                      <button type="submit" disabled={accounts.busy || !accounts.renameText.trim()} className={link}>
                        Save
                      </button>
                      <button type="button" onClick={() => accounts.setRenaming(null)} className={link}>
                        Cancel
                      </button>
                    </form>
                  ) : (
                    <>
                      <span className="flex flex-col min-w-0 flex-1">
                        <div className="flex items-center gap-1.5 min-w-0">
                          <span className={`text-[12.5px] font-medium tracking-tight truncate ${fg}`} title={status?.detail}>
                            {title}
                          </span>
                          {authMode && (
                            <span
                              className={`text-[9.5px] font-medium px-1.5 py-0.5 rounded-full uppercase tracking-wider shrink-0 ${
                                authMode === 'api_key'
                                  ? isLight
                                    ? 'bg-purple-100 text-purple-700'
                                    : 'bg-purple-500/20 text-purple-300'
                                  : isLight
                                  ? 'bg-blue-100 text-blue-700'
                                  : 'bg-blue-500/20 text-blue-300'
                              }`}
                            >
                              {authMode === 'api_key' ? 'API Key' : 'Keyring'}
                            </span>
                          )}
                          {isDefault ? (
                            <span
                              className={`text-[9.5px] font-semibold px-2 py-0.5 rounded-full tracking-tight shrink-0 ${
                                isLight ? 'bg-black/10 text-black/80' : 'bg-white/15 text-white/90'
                              }`}
                            >
                              Default
                            </span>
                          ) : driverAccounts.length > 1 ? (
                            <button
                              type="button"
                              disabled={accounts.busy}
                              onClick={(e) => {
                                e.stopPropagation();
                                void accounts.setProjectDefault(provider.id, row.accountId || 'default');
                              }}
                              className={`text-[9.5px] font-medium px-2 py-0.5 rounded-full tracking-tight transition-colors cursor-pointer shrink-0 opacity-70 hover:opacity-100 ${
                                isLight
                                  ? 'bg-black/5 hover:bg-black/10 text-black/70 hover:text-black'
                                  : 'bg-white/5 hover:bg-white/10 text-white/70 hover:text-white'
                              }`}
                              title={accounts.project ? `Make default for ${accounts.project.name}` : 'Make default'}
                            >
                              Make default
                            </button>
                          ) : null}
                        </div>
                        <span className={`text-[11px] tracking-tight truncate ${sub}`}>
                          {[label, limit?.resetsAt ? until(limit.resetsAt) : ''].filter(Boolean).join(' · ')}
                        </span>
                      </span>
                      <button type="button" onClick={() => setOpen(isOpen ? null : row.key)} className={`${link} flex items-center gap-0.5 shrink-0`}>
                        Details
                        <ChevronDown size={13} strokeWidth={2} className={`transition-transform duration-200 ${isOpen ? 'rotate-180' : ''}`} />
                      </button>
                    </>
                  )}
                </div>
                {isOpen && !renaming && (
                  <div className="flex flex-col gap-2.5 pl-[62px] pr-3.5 pb-3">
                    {row.usage?.limits?.map((l) => (
                      <QuotaBar key={`${row.key}-${l.window}`} limit={l} />
                    ))}
                    {row.usage && (row.usage.inputTokens ?? 0) + (row.usage.outputTokens ?? 0) > 0 && (
                      <span className={`flex items-center gap-3 text-[10.5px] tabular-nums ${sub}`}>
                        <span title="Input tokens">↓ {compactTokens(row.usage.inputTokens ?? 0)}</span>
                        <span title="Output tokens">↑ {compactTokens(row.usage.outputTokens ?? 0)}</span>
                        {(row.usage.costUsd ?? 0) > 0 && <span>${row.usage.costUsd.toFixed(2)}</span>}
                        {!!row.usage.limitsFetchedAt && (
                          <span className={isStale(row.usage.limitsFetchedAt) ? 'text-amber-300/70' : ''}>Updated {since(row.usage.limitsFetchedAt)}</span>
                        )}
                      </span>
                    )}
                    {row.usage?.limitsError && <span className={`text-[10.5px] leading-relaxed ${sub}`}>{row.usage.limitsError}</span>}
                    {row.accountId && (
                      <div className="flex items-center gap-3">
                        {confirming ? (
                          <>
                            <button type="button" disabled={accounts.busy} onClick={() => void accounts.remove(provider.id, row.accountId!)} className={`${link} !text-red-400`}>
                              Remove
                            </button>
                            <button type="button" onClick={() => accounts.setConfirmRemove(null)} className={link}>
                              Keep
                            </button>
                          </>
                        ) : (
                          <>
                            {isDefault ? (
                              <span className={`text-[11.5px] font-medium tracking-tight ${sub} flex items-center gap-1 shrink-0`}>
                                ✓ Default account
                              </span>
                            ) : (
                              <button
                                type="button"
                                disabled={accounts.busy}
                                onClick={() => void accounts.setProjectDefault(provider.id, row.accountId || 'default')}
                                className={link}
                                title={accounts.project ? `Set as default account for ${accounts.project.name}` : 'Set as default account'}
                              >
                                Set as default
                              </button>
                            )}
                            {!(provider.id === 'antigravity' && authMode === 'api_key') && (
                              <button type="button" disabled={accounts.busy} onClick={() => void accounts.signIn(provider.id, row.accountId!)} className={link}>
                                {status?.signedIn ? 'Sign in again' : 'Sign in'}
                              </button>
                            )}
                            {provider.id === 'antigravity' && authMode === 'keyring' && row.accountId !== 'default' && (
                              <button
                                type="button"
                                disabled={accounts.busy}
                                onClick={() => void accounts.captureProfile(row.accountId!)}
                                className={link}
                                title="Capture active OS keyring credentials to this profile"
                              >
                                Capture login
                              </button>
                            )}
                            <button
                              type="button"
                              onClick={() => {
                                accounts.setRenaming(row.key);
                                accounts.setRenameText(row.name);
                              }}
                              className={link}
                            >
                              Rename
                            </button>
                            {row.accountId !== 'default' && (
                              <button type="button" onClick={() => accounts.setConfirmRemove(row.key)} className={link}>
                                Remove
                              </button>
                            )}
                          </>
                        )}
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {snapshot && (
        <div className={`flex items-center gap-3 px-3.5 py-2.5 border-t ${isLight ? 'border-black/[0.06]' : 'border-white/[0.06]'}`}>
          {accounts.adding === provider.id ? (
            <form
              className="flex flex-col gap-1.5 flex-1"
              onSubmit={(event) => {
                event.preventDefault();
                void accounts.add(provider.id);
              }}
            >
              <div className="flex items-center gap-2">
                <input
                  autoFocus
                  placeholder="Account name, e.g. Personal"
                  value={accounts.name}
                  onChange={(event) => accounts.setName(event.target.value)}
                  className={input}
                />
                <button
                  type="submit"
                  disabled={
                    accounts.busy ||
                    !accounts.name.trim() ||
                    (provider.id === 'antigravity' && accounts.authMode === 'api_key' && !accounts.apiKey.trim())
                  }
                  className={link}
                >
                  Add
                </button>
                <button type="button" onClick={() => accounts.setAdding(null)} className={link}>
                  Cancel
                </button>
              </div>
              {provider.id === 'antigravity' && (
                <div className="flex flex-col gap-1.5 pt-0.5">
                  <div className={`flex items-center gap-3 text-[11px] ${sub}`}>
                    <label className="flex items-center gap-1 cursor-pointer">
                      <input
                        type="radio"
                        name="antigravityAuthMode"
                        checked={accounts.authMode === 'keyring'}
                        onChange={() => accounts.setAuthMode('keyring')}
                      />
                      <span>Keyring (Google Sub)</span>
                    </label>
                    <label className="flex items-center gap-1 cursor-pointer">
                      <input
                        type="radio"
                        name="antigravityAuthMode"
                        checked={accounts.authMode === 'api_key'}
                        onChange={() => accounts.setAuthMode('api_key')}
                      />
                      <span>API Key</span>
                    </label>
                  </div>
                  {accounts.authMode === 'api_key' && (
                    <input
                      placeholder="GEMINI_API_KEY (AIza...)"
                      type="password"
                      value={accounts.apiKey}
                      onChange={(event) => accounts.setApiKey(event.target.value)}
                      className={input}
                    />
                  )}
                </div>
              )}
              {COPYABLE.has(provider.id) && (
                <label className={`flex items-center gap-1.5 text-[11px] cursor-pointer ${sub}`}>
                  <input type="checkbox" checked={accounts.copySettings} onChange={(event) => accounts.setCopySettings(event.target.checked)} />
                  Copy settings from {driverAccounts[0]?.name || 'Default'}
                </label>
              )}
            </form>
          ) : (
            <>
              <button
                type="button"
                onClick={() => {
                  accounts.setAdding(provider.id);
                  accounts.setName('');
                }}
                className={`${link} flex items-center gap-1`}
              >
                <Plus size={13} strokeWidth={2} />
                Add account
              </button>
              <span className="flex-1" />
              {accounts.project && driverAccounts.length > 1 && (
                <>
                  <span className={`text-[11px] truncate ${sub}`}>{accounts.project.name} uses</span>
                  <CleanDropdown
                    value={projectDefault}
                    options={driverAccounts.map((account) => ({ value: account.id, label: account.name }))}
                    disabled={accounts.busy}
                    onChange={(value) => void accounts.setProjectDefault(provider.id, value)}
                    className="w-[118px] shrink-0"
                  />
                </>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
};
