import { MaterialIcon } from '../common/icons';
import React from 'react';
import { LiquidGlass } from '../../liquid-glass';
import { session } from '../../../wailsjs/go/models';
import {
  compactTokens,
  isStale,
  QuotaBar,
  since,
  usageKey,
  usageName,
} from './usageBars';

export interface UsagePanelProps {
  report: session.UsageReport | null;
  error?: string | null;
  onClose: () => void;
  className?: string;
}

export const UsagePanel: React.FC<UsagePanelProps> = ({
  report,
  error,
  onClose,
  className = '',
}) => {
  // Relative timestamps are derived from the clock, not from the report, so
  // without a tick they freeze whenever the underlying figures are unchanged —
  // making a working panel look stalled.
  const [, tick] = React.useReducer((n: number) => n + 1, 0);
  React.useEffect(() => {
    const timer = window.setInterval(tick, 30_000);
    return () => window.clearInterval(timer);
  }, []);

  const drivers = report?.drivers ?? [];
  const quotaDrivers = drivers.filter((driver) => (driver.limits?.length ?? 0) > 0);
  const quotaIssues = drivers
    .filter((driver) => !!driver.limitsError)
    .map((driver) => ({ key: usageKey(driver), name: usageName(driver), message: driver.limitsError as string }));
  // Session spend comes from the event stream and needs no subscription, so a
  // CLI without quota (OpenCode on third-party models, Codex) still shows what
  // it burned this run.
  const spendDrivers = drivers.filter(
    (driver) =>
      (driver.inputTokens ?? 0) + (driver.outputTokens ?? 0) > 0 ||
      (driver.costUsd ?? 0) > 0 ||
      (driver.turns ?? 0) > 0,
  );
  const empty = quotaDrivers.length === 0 && quotaIssues.length === 0 && spendDrivers.length === 0 && !error;

  return (
    <LiquidGlass
      variant="panel"
      surface="squircle"
      radius={20}
      bezelWidth={18}
      glassThickness={24}
      refractionScale={0.8}
      blur={0.4}
      specularOpacity={0.8}
      specularSaturation={6}
      lightAngle={-45}
      tint="var(--theme-panel-bg, rgba(18, 20, 26, 0.88))"
      shadow="apple"
      border="1px solid var(--theme-panel-border, rgba(255, 255, 255, 0.10))"
      frost={16}
      frostSaturation={170}
      className={`w-[360px] flex flex-col ${className}`}
      style={{
        boxShadow:
          '0 20px 54px rgba(0, 0, 0, 0.55), 0 4px 14px rgba(0, 0, 0, 0.35), inset 0 0.5px 0.5px rgba(255, 255, 255, 0.25)',
      }}
    >
      <div className="flex items-center justify-between px-4 pt-3.5 pb-2.5 shrink-0">
        <div className="flex items-center gap-2">
          <MaterialIcon name="speed" className="text-[18px] text-white/80"/>
          <span className="text-[13px] font-semibold font-(family-name:--app-font) text-white tracking-tight">
            Usage
          </span>
        </div>
        <button
          type="button"
          title="Close"
          onClick={onClose}
          className="w-[24px] h-[24px] rounded-[7px] hover:bg-white/10 active:scale-90 flex items-center justify-center transition-all duration-150 cursor-pointer text-white/45 hover:text-white/90"
        >
          <MaterialIcon name="close" className="text-[16px]"/>
        </button>
      </div>

      {error && (
        <div className="mx-4 mb-3 px-3 py-2 rounded-[9px] bg-red-500/10 border border-red-400/25">
          <span className="text-[11px] font-medium font-(family-name:--app-font) text-red-200/90 leading-relaxed">
            {error}
          </span>
        </div>
      )}

      {empty && (
        <div className="px-4 pb-5 pt-1">
          <p className="text-[12px] font-(family-name:--app-font) text-white/45 leading-relaxed">
            Reading your plan usage…
          </p>
        </div>
      )}

      {spendDrivers.length > 0 && (
        <div className="mx-4 mb-4 flex flex-col gap-2 shrink-0">
          <span className="text-[10px] font-semibold font-(family-name:--app-font) text-white/45 tracking-tight uppercase px-0.5">
            This run
          </span>
          {spendDrivers.map((driver) => (
            <div
              key={`spend-${usageKey(driver)}`}
              className="p-3 rounded-[12px] bg-white/[0.05] border border-white/[0.09] flex flex-col gap-1.5"
            >
              <div className="flex items-baseline justify-between gap-2">
                <span className="text-[11px] font-medium font-(family-name:--app-font) text-white/70 tracking-tight">
                  {usageName(driver)}
                </span>
                {(driver.costUsd ?? 0) > 0 && (
                  <span className="text-[11px] font-semibold font-(family-name:--app-font) text-white/90 tabular-nums">
                    ${driver.costUsd.toFixed(2)}
                  </span>
                )}
              </div>
              <div className="flex items-center gap-3 text-[10px] font-(family-name:--app-font) text-white/45 tracking-tight tabular-nums">
                <span title="Input tokens">↓ {compactTokens(driver.inputTokens ?? 0)}</span>
                <span title="Output tokens">↑ {compactTokens(driver.outputTokens ?? 0)}</span>
                {(driver.cacheReadTokens ?? 0) > 0 && (
                  <span title="Cache read tokens">cache {compactTokens(driver.cacheReadTokens)}</span>
                )}
                {(driver.turns ?? 0) > 0 && (
                  <span title="Turns">
                    {driver.turns} turn{driver.turns === 1 ? '' : 's'}
                  </span>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {(quotaDrivers.length > 0 || quotaIssues.length > 0) && (
        <div className="mx-4 mb-4 flex flex-col gap-3 shrink-0">
          {quotaDrivers.map((driver) => (
            <div
              key={`quota-${usageKey(driver)}`}
              className="p-3 rounded-[12px] bg-white/[0.05] border border-white/[0.09] flex flex-col gap-2.5"
            >
              <div className="flex items-baseline justify-between gap-2">
                <span className="text-[10px] font-semibold font-(family-name:--app-font) text-white/45 tracking-tight uppercase">
                  {usageName(driver)} plan
                </span>
                {!!driver.limitsFetchedAt && (
                  <span
                    title="Fetched from your subscription while the panel is open. If the account cannot be reached, the CLI's own cached figures are shown instead and this stamp reports how old they are."
                    className={`text-[9px] font-(family-name:--app-font) tracking-tight ${
                      isStale(driver.limitsFetchedAt) ? 'text-amber-300/60' : 'text-white/30'
                    }`}
                  >
                    Updated {since(driver.limitsFetchedAt)}
                  </span>
                )}
              </div>
              {driver.limits?.map((limit, idx) => (
                <QuotaBar key={`${usageKey(driver)}-${limit.window}-${idx}`} limit={limit} />
              ))}
            </div>
          ))}

          {/* Why a meter is missing, per CLI. Left blank it would read as a bug
              rather than a signed-out or offline provider. */}
          {quotaIssues.map(({ key, name, message }) => (
            <span
              key={`quota-error-${key}`}
              className="text-[10px] font-(family-name:--app-font) text-white/35 tracking-tight leading-relaxed px-0.5"
            >
              {name}: {message}
            </span>
          ))}
        </div>
      )}
    </LiquidGlass>
  );
};

export default UsagePanel;
