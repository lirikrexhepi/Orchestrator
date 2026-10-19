import React, { useEffect, useRef, useState } from 'react';
import { activity } from '../../../wailsjs/go/models';
import { Check, ChevronDown } from '../common/icons';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { ActivityPeriod, useActivityOverview } from './useActivityOverview';

const PERIODS: { id: ActivityPeriod; label: string; previous: string }[] = [
  { id: 'week', label: 'This week', previous: 'last week' },
  { id: 'month', label: 'This month', previous: 'last month' },
  { id: 'year', label: 'This year', previous: 'last year' },
];

const HEAT_DARK = ['rgba(255,255,255,0.055)', '#0e4429', '#006d32', '#26a641', '#39d353'];
const HEAT_LIGHT = ['rgba(0,0,0,0.06)', '#9be9a8', '#40c463', '#30a14e', '#216e39'];

const PROVIDER_ORDER = ['codex', 'claude', 'opencode', 'antigravity'];
const PROVIDER_DARK = ['#3987e5', '#d95926', '#199e70', '#c98500', '#9085e9'];
const PROVIDER_LIGHT = ['#2a78d6', '#eb6834', '#1baf7a', '#eda100', '#6250d6'];
const OTHER_DARK = '#6b6a66';
const OTHER_LIGHT = '#a3a29c';

const GOOD = '#0ca30c';
const CRITICAL = '#d03b3b';

const STATS = [
  { label: 'messages', key: 'messages' as const },
  { label: 'sessions', key: 'sessions' as const },
  { label: 'tool calls', key: 'toolCalls' as const },
  { label: 'tokens', key: 'tokens' as const },
];

function compact(n: number): string {
  const abs = Math.abs(n);
  const trim = (v: number) => (v >= 100 ? v.toFixed(0) : v.toFixed(1).replace(/\.0$/, ''));
  if (abs >= 1e9) return `${trim(n / 1e9)}B`;
  if (abs >= 1e6) return `${trim(n / 1e6)}M`;
  if (abs >= 1e4) return `${trim(n / 1e3)}k`;
  return n.toLocaleString();
}

function parseDay(key: string): Date {
  const [y, m, d] = key.split('-').map(Number);
  return new Date(y, m - 1, d);
}

function plural(n: number, word: string): string {
  return `${n.toLocaleString()} ${word}${n === 1 ? '' : 's'}`;
}

function providerColor(driver: string, isLight: boolean): string {
  const slot = PROVIDER_ORDER.indexOf(driver);
  const palette = isLight ? PROVIDER_LIGHT : PROVIDER_DARK;
  if (slot >= 0) return palette[slot];
  return isLight ? OTHER_LIGHT : OTHER_DARK;
}

interface MenuOption {
  id: string;
  label: string;
}

const Menu: React.FC<{
  value: string;
  options: MenuOption[];
  onChange: (id: string) => void;
  isLight: boolean;
}> = ({ value, options, onChange, isLight }) => {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const current = options.find((o) => o.id === value) ?? options[0];

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', close);
    return () => document.removeEventListener('mousedown', close);
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={`flex items-center gap-1 h-[22px] px-1.5 rounded-[6px] text-[11.5px] tracking-tight cursor-pointer transition-colors max-w-[160px] ${
          isLight ? 'hover:bg-black/[0.05] text-black/45 hover:text-black/75' : 'hover:bg-white/[0.06] text-white/45 hover:text-white/80'
        }`}
      >
        <span className="truncate">{current?.label}</span>
        <ChevronDown size={11} strokeWidth={2} className="shrink-0 opacity-60" />
      </button>
      {open && (
        <div
          className={`absolute left-0 top-[26px] z-20 min-w-[160px] max-h-[240px] overflow-y-auto rounded-[12px] border p-1 backdrop-blur-xl ${
            isLight
              ? 'bg-white/[0.97] border-black/[0.08] shadow-[0_12px_32px_rgba(0,0,0,0.14)]'
              : 'bg-[#1c1c1d]/[0.97] border-white/[0.09] shadow-[0_12px_32px_rgba(0,0,0,0.55)]'
          }`}
        >
          {options.map((option) => (
            <button
              key={option.id}
              type="button"
              onClick={() => {
                onChange(option.id);
                setOpen(false);
              }}
              className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-[8px] text-left text-[12px] tracking-tight cursor-pointer ${
                isLight ? 'hover:bg-black/[0.05] text-black/80' : 'hover:bg-white/[0.07] text-white/85'
              }`}
            >
              <span className="flex-1 truncate">{option.label}</span>
              {option.id === value && <Check size={12} strokeWidth={2} className="shrink-0 opacity-70" />}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

const Heatmap: React.FC<{ days: activity.Day[]; isLight: boolean }> = ({ days, isLight }) => {
  const columns = Math.ceil(days.length / 7);
  const max = days.reduce((m, d) => Math.max(m, d.count), 0);
  const scale = isLight ? HEAT_LIGHT : HEAT_DARK;
  const level = (count: number) => (count <= 0 || max === 0 ? 0 : Math.min(4, Math.ceil((count / max) * 4)));

  return (
    <div
      className="grid gap-[2px] w-full"
      style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`, gridTemplateRows: 'repeat(7, auto)' }}
    >
      {days.map((day, i) => {
        const date = parseDay(day.date).toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' });
        return (
          <div
            key={day.date}
            title={`${plural(day.count, 'message')} on ${date}`}
            className="aspect-square rounded-[2px]"
            style={{ gridColumn: Math.floor(i / 7) + 1, gridRow: (i % 7) + 1, background: scale[level(day.count)] }}
          />
        );
      })}
    </div>
  );
};

export const ActivityOverview: React.FC<{ isLight: boolean; fullscreen?: boolean }> = ({ isLight, fullscreen }) => {
  const [period, setPeriod] = useState<ActivityPeriod>('week');
  const [project, setProject] = useState('');
  const { overview, error } = useActivityOverview(period, project);
  const providers = useOrchestratorStore((state) => state.providers);
  const models = useOrchestratorStore((state) => state.models);

  const meta = PERIODS.find((p) => p.id === period) ?? PERIODS[0];
  const text = isLight ? 'text-black/80' : 'text-white/85';
  const muted = isLight ? 'text-black/40' : 'text-white/40';

  const providerName = (id: string) =>
    providers.find((p) => p.id === id)?.name || (id ? id.charAt(0).toUpperCase() + id.slice(1) : 'Unknown');
  const modelName = (id?: string) => models.find((m) => m.id === id)?.name || id || 'Unknown model';

  const projectOptions: MenuOption[] = [
    { id: '', label: 'All projects' },
    ...(overview?.projects ?? []).map((p) => ({ id: p.path, label: p.name })),
  ];

  const current = overview?.current;
  const previous = overview?.previous;
  const shares = overview?.providers ?? [];
  const shareTotal = shares.reduce((sum, s) => sum + s.tokens, 0);

  const providerTitle = (driver: string) =>
    (overview?.models ?? [])
      .filter((m) => m.driver === driver)
      .map((m) => `${modelName(m.model)} · ${plural(m.messages, 'msg')} · ${compact(m.tokens)}`)
      .join('\n');

  const summary = overview
    ? [
        `${plural(overview.yearMessages, 'message')} in the last year`,
        overview.currentStreak > 0 ? `${overview.currentStreak}-day streak` : '',
        overview.busiestWeekday ? `busiest on ${overview.busiestWeekday}s` : '',
      ]
        .filter(Boolean)
        .join(' · ')
    : '';

  return (
    <div className="absolute inset-0 overflow-y-auto flex font-(family-name:--app-font)">
      <div className={`m-auto w-full max-w-[520px] flex flex-col gap-6 px-6 pt-8 ${fullscreen ? 'pb-[var(--input-dock-h,150px)]' : 'pb-8'}`}>
        <div className="flex items-center gap-0.5 -ml-1.5">
          <Menu value={period} options={PERIODS} onChange={(id) => setPeriod(id as ActivityPeriod)} isLight={isLight} />
          <Menu value={project} options={projectOptions} onChange={setProject} isLight={isLight} />
        </div>

        {error && !overview ? (
          <span className={`text-[12px] ${muted}`}>Activity is unavailable: {error}</span>
        ) : (
          <>
            <div className="grid grid-cols-4 gap-4">
              {STATS.map((stat) => {
                const now = current?.[stat.key] ?? 0;
                const diff = now - (previous?.[stat.key] ?? 0);
                const isTokens = stat.key === 'tokens';
                return (
                  <div key={stat.key} className="min-w-0 flex flex-col gap-1.5">
                    <span className={`text-[20px] leading-none font-semibold tracking-tight tabular-nums truncate ${text}`}>
                      {overview ? (isTokens ? compact(now) : now.toLocaleString()) : '–'}
                    </span>
                    <span className={`text-[11px] tracking-tight truncate ${muted}`}>
                      {stat.label}
                      {overview && diff !== 0 && (
                        <span className="tabular-nums ml-1" style={{ color: diff > 0 ? GOOD : CRITICAL }} title={`vs ${meta.previous}`}>
                          {diff > 0 ? '+' : '−'}
                          {isTokens ? compact(Math.abs(diff)) : Math.abs(diff).toLocaleString()}
                        </span>
                      )}
                    </span>
                  </div>
                );
              })}
            </div>

            {overview && (
              <div className="flex flex-col gap-2">
                <Heatmap days={overview.days} isLight={isLight} />
                <span className={`text-[11px] tracking-tight ${muted}`}>{summary}</span>
              </div>
            )}

            {shares.length > 0 && (
              <div className="flex flex-col gap-2">
                <div className="flex w-full h-[4px] gap-[2px]">
                  {shares.map((share) => (
                    <div
                      key={share.driver}
                      className="h-full rounded-full min-w-[4px]"
                      style={{
                        flexGrow: shareTotal > 0 ? share.tokens : share.messages || 1,
                        flexBasis: 0,
                        background: providerColor(share.driver, isLight),
                      }}
                    />
                  ))}
                </div>
                <div className="flex flex-wrap gap-x-3 gap-y-1">
                  {shares.map((share) => (
                    <span
                      key={share.driver}
                      title={providerTitle(share.driver)}
                      className={`flex items-center gap-1.5 text-[11px] tracking-tight cursor-default ${muted}`}
                    >
                      <span className="w-[6px] h-[6px] rounded-full" style={{ background: providerColor(share.driver, isLight) }} />
                      <span>{providerName(share.driver)}</span>
                      <span className="tabular-nums">
                        {shareTotal > 0 ? `${Math.round((share.tokens / shareTotal) * 100)}%` : plural(share.messages, 'message')}
                      </span>
                    </span>
                  ))}
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
};
