import React from 'react';
import { domain, session } from '../../../wailsjs/go/models';

export const DRIVER_NAMES: Record<string, string> = {
  claude: 'Claude Code',
  antigravity: 'Antigravity',
  codex: 'Codex',
  opencode: 'OpenCode',
};

export const usageKey = (entry: session.DriverUsage) => `${entry.driver}:${entry.account || 'default'}`;

export const usageName = (entry: session.DriverUsage) => {
  const name = DRIVER_NAMES[entry.driver] ?? entry.driver;
  return entry.accountName ? `${name} · ${entry.accountName}` : name;
};

// Past this the figures are old enough to mislead — the 5h window can move
// several points in that time — so the label is flagged rather than shown as if
// it were current.
export const STALE_AFTER_MS = 15 * 60_000;

export function isStale(timestamp: number): boolean {
  return timestamp > 0 && Date.now() - timestamp > STALE_AFTER_MS;
}

export function since(timestamp: number): string {
  if (!timestamp) return '';
  const minutes = Math.floor((Date.now() - timestamp) / 60_000);
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? `${hours}h ago` : `${Math.floor(hours / 24)}d ago`;
}

const WINDOW_NAMES: Record<string, string> = {
  five_hour: 'Session (5h)',
  seven_day: 'Weekly (7d)',
  monthly: 'Monthly',
  seven_day_opus: 'Opus weekly',
  seven_day_sonnet: 'Sonnet weekly',
  seven_day_scoped: 'Model weekly',
};

// Compact token counts: 950 → "950", 12_400 → "12.4K", 5_200_000 → "5.2M".
export function compactTokens(value: number): string {
  if (!value) return '0';
  if (value < 1000) return `${value}`;
  if (value < 1_000_000) {
    const v = value / 1000;
    return `${v >= 100 ? Math.round(v) : v.toFixed(1).replace(/\.0$/, '')}K`;
  }
  const v = value / 1_000_000;
  return `${v >= 100 ? Math.round(v) : v.toFixed(1).replace(/\.0$/, '')}M`;
}

// Model-scoped weekly windows arrive named after the model they cover, so the
// label is built rather than looked up.
export function windowLabel(window: string): string {
  const scoped = window.startsWith('seven_day_scoped:');
  if (scoped) return `${window.slice('seven_day_scoped:'.length)} weekly`;
  return WINDOW_NAMES[window] ?? window;
}

// resetsAt is unix seconds from the CLI, unlike every other timestamp here.
export function until(resetsAtSeconds: number): string {
  if (!resetsAtSeconds) return '';
  const minutes = Math.round((resetsAtSeconds * 1000 - Date.now()) / 60_000);
  if (minutes <= 0) return 'resetting';
  if (minutes < 60) return `resets in ${minutes}m`;
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? `resets in ${hours}h` : `resets in ${Math.floor(hours / 24)}d`;
}

// Bar/ring tone by quota used: calm below 75, warming below 90, hot above.
export function quotaTone(used: number | undefined): string {
  if (typeof used !== 'number' || used < 75) return 'bg-white/55';
  if (used < 90) return 'bg-amber-300/80';
  return 'bg-rose-400/85';
}

export function quotaRingColor(used: number | undefined, isLight: boolean): string {
  if (typeof used !== 'number' || used < 75) return isLight ? 'rgba(0, 0, 0, 0.55)' : 'rgba(255, 255, 255, 0.6)';
  if (used < 90) return 'rgba(252, 211, 77, 0.9)';
  return 'rgba(251, 113, 133, 0.9)';
}

export interface UsageRingProps {
  used?: number;
  logo?: string;
  size?: number;
  isLight?: boolean;
  label?: string;
}

/** Compact quota ring: progress arc around the provider logo, mirroring the
 *  island's model usage ring. The % itself sits beside the ring. */
export const UsageRing: React.FC<UsageRingProps> = ({ used, logo, size = 26, isLight, label }) => {
  const r = 8;
  const circumference = 2 * Math.PI * r;
  const known = typeof used === 'number';
  const offset = circumference - (circumference * Math.min(100, used ?? 0)) / 100;
  const logoSize = Math.round(size * 0.52);
  return (
    <span className="relative inline-flex items-center justify-center shrink-0" style={{ width: size, height: size }}>
      <svg className="absolute inset-0 w-full h-full -rotate-90" viewBox="0 0 20 20">
        <circle cx="10" cy="10" r={r} fill="none" stroke="rgba(127, 127, 127, 0.25)" strokeWidth="2" />
        {known && (
          <circle
            cx="10"
            cy="10"
            r={r}
            fill="none"
            stroke={quotaRingColor(used, !!isLight)}
            strokeWidth="2"
            strokeDasharray={circumference}
            strokeDashoffset={offset}
            strokeLinecap="round"
            className="transition-[stroke-dashoffset] duration-300"
          />
        )}
      </svg>
      {label !== undefined ? (
        <span
          className={`relative font-(family-name:--app-font) font-semibold tabular-nums tracking-tight ${isLight ? 'text-black/80' : 'text-white/85'}`}
          style={{ fontSize: Math.max(9, Math.round(size * 0.27)) }}
        >
          {label}
        </span>
      ) : logo ? (
        <img src={logo} alt="" draggable={false} className="object-contain" style={{ width: logoSize, height: logoSize }} />
      ) : null}
    </span>
  );
};

export const QuotaBar: React.FC<{ limit: domain.RateLimit }> = ({ limit }) => {
  const used = limit.usedPercent;
  const known = typeof used === 'number';
  const label = windowLabel(limit.window || limit.status || '');
  const tone = quotaTone(used);

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[11px] font-medium font-(family-name:--app-font) text-white/70 tracking-tight truncate">
          {label}
        </span>
        <span className="text-[11px] font-semibold font-(family-name:--app-font) text-white/90 tabular-nums shrink-0">
          {known ? `${used}% used` : '—'}
        </span>
      </div>

      <div className="h-[4px] rounded-full bg-white/10 overflow-hidden">
        {known && (
          <div
            className={`h-full rounded-full ${tone} transition-[width] duration-300 ease-out`}
            style={{ width: `${Math.min(100, Math.max(used, used > 0 ? 2 : 0))}%` }}
          />
        )}
      </div>
      {!!limit.resetsAt && (
        <span className="text-[10px] font-(family-name:--app-font) text-white/35 tracking-tight">
          {until(limit.resetsAt)}
        </span>
      )}
    </div>
  );
};
