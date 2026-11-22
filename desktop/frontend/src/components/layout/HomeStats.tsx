import React, { useState, useRef, useEffect, useMemo } from 'react';
import { ActivityPeriod, useActivityOverview } from '../activity/useActivityOverview';
import { useProjects } from '../orchestrator/useProjects';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { Check } from '../common/icons';
import { activity } from '../../../wailsjs/go/models';

export interface HomeStatsProps {
  isLight?: boolean;
}

const PERIOD_TABS: { id: ActivityPeriod; label: string; phrase: string; prevLabel: string }[] = [
  { id: 'today', label: 'Today', phrase: 'so far', prevLabel: 'yesterday' },
  { id: 'week', label: 'Week', phrase: 'this week', prevLabel: 'last week' },
  { id: 'month', label: 'Month', phrase: 'this month', prevLabel: 'last month' },
];

const WHITE_DARK = [
  '#151619',                    // Level 0: empty
  'rgba(255, 255, 255, 0.20)',  // Level 1: low
  'rgba(255, 255, 255, 0.42)',  // Level 2: medium
  'rgba(255, 255, 255, 0.68)',  // Level 3: high
  'rgba(255, 255, 255, 0.95)',  // Level 4: peak
];

const WHITE_LIGHT = [
  'rgba(0, 0, 0, 0.05)',
  'rgba(0, 0, 0, 0.20)',
  'rgba(0, 0, 0, 0.42)',
  'rgba(0, 0, 0, 0.68)',
  'rgba(0, 0, 0, 0.92)',
];

const PROVIDER_COLORS: Record<string, string> = {
  claude: '#d97757',
  antigravity: '#7c9cf5',
  codex: '#38bdf8',
  opencode: '#34d399',
};

const MONO_SHADES_DARK = [
  'rgba(255, 255, 255, 0.85)',
  'rgba(255, 255, 255, 0.45)',
  'rgba(255, 255, 255, 0.22)',
  'rgba(255, 255, 255, 0.12)',
];

const MONO_SHADES_LIGHT = [
  'rgba(0, 0, 0, 0.85)',
  'rgba(0, 0, 0, 0.45)',
  'rgba(0, 0, 0, 0.22)',
  'rgba(0, 0, 0, 0.12)',
];

function formatTokens(tokens: number): string {
  if (tokens >= 1_000_000_000) return `${(tokens / 1_000_000_000).toFixed(1).replace(/\.0$/, '')}B`;
  if (tokens >= 1_000_000) return `${Math.round(tokens / 1_000_000)}M`;
  if (tokens >= 1_000) return `${Math.round(tokens / 1_000)}k`;
  return `${tokens}`;
}

function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return '0m';
  const totalMinutes = Math.round(ms / 60_000);
  if (totalMinutes < 1) return '<1m';
  const hours = Math.floor(totalMinutes / 60);
  const mins = totalMinutes % 60;
  if (hours === 0) return `${mins}m`;
  if (mins === 0) return `${hours}h`;
  return `${hours}h ${mins}m`;
}

function formatDate(key: string): string {
  if (!key) return '';
  const [y, m, d] = key.split('-').map(Number);
  return new Date(y, m - 1, d).toLocaleDateString(undefined, {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
  });
}

function getGreeting(hour: number): string {
  if (hour >= 5 && hour < 12) return 'Good morning';
  if (hour >= 12 && hour < 17) return 'Good afternoon';
  if (hour >= 17 && hour < 21) return 'Good evening';
  return 'Late night';
}

export const HomeStats: React.FC<HomeStatsProps> = ({ isLight = false }) => {
  const [period, setPeriod] = useState<ActivityPeriod>('today');
  const [project, setProject] = useState<string>('');
  const [isProjectOpen, setIsProjectOpen] = useState(false);

  const projectMenuRef = useRef<HTMLDivElement>(null);

  const { overview } = useActivityOverview(period, project);
  const allProjects = useProjects();
  const providersList = useOrchestratorStore((s) => s.providers);

  useEffect(() => {
    if (!isProjectOpen) return;
    const handlePointerDown = (e: MouseEvent) => {
      if (projectMenuRef.current && !projectMenuRef.current.contains(e.target as Node)) {
        setIsProjectOpen(false);
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setIsProjectOpen(false);
    };
    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [isProjectOpen]);

  const projectOptions = useMemo(() => {
    const map = new Map<string, string>();
    map.set('', 'All projects');
    for (const p of overview?.projects ?? []) {
      if (p.path) map.set(p.path, p.name);
    }
    for (const p of allProjects?.projects ?? []) {
      if (p.path) map.set(p.path, p.name);
    }
    return Array.from(map.entries()).map(([id, label]) => ({ id, label }));
  }, [overview?.projects, allProjects?.projects]);

  const cols = 53;
  const days = useMemo(() => {
    const raw = overview?.days ?? [];
    const target = cols * 7;
    if (raw.length === 0) {
      return Array.from({ length: target }, () => ({ date: '', count: 0 } as activity.Day));
    }
    const list = [...raw];
    const rem = list.length % 7;
    if (rem !== 0) {
      for (let i = 0; i < 7 - rem; i++) {
        list.push({ date: '', count: 0 } as activity.Day);
      }
    }
    if (list.length < target) {
      const pad = Array.from({ length: target - list.length }, () => ({ date: '', count: 0 } as activity.Day));
      return [...pad, ...list];
    }
    return list.slice(-target);
  }, [overview?.days]);

  const max = useMemo(() => days.reduce((acc, d) => Math.max(acc, d.count), 0), [days]);

  const getLevel = (count: number) => {
    if (count <= 0 || max === 0) return 0;
    return Math.min(4, Math.ceil((count / max) * 4));
  };

  const now = new Date();
  const currentYear = now.getFullYear();
  const yearStartKey = `${currentYear}-01-01`;
  const thisYearCol = useMemo(() => {
    for (let c = 0; c < cols; c++) {
      for (let r = 0; r < 7; r++) {
        const d = days[c * 7 + r];
        if (d?.date && d.date >= yearStartKey) {
          return c;
        }
      }
    }
    return -1;
  }, [days, yearStartKey, cols]);
  const todayHeading = now.toLocaleDateString('en-US', {
    weekday: 'long',
    month: 'long',
    day: 'numeric',
  });
  const greeting = getGreeting(now.getHours());
  const userName = overview?.userName?.trim() || '';
  const greetingLine = userName ? `${greeting}, ${userName}.` : `${greeting}.`;

  const activeTab = PERIOD_TABS.find((t) => t.id === period) ?? PERIOD_TABS[0];
  const current = overview?.current;
  const previous = overview?.previous;

  const messagesCount = current?.messages ?? 0;
  const sessionsCount = current?.sessions ?? 0;
  const agentsCount = current?.agents ?? 0;
  const workingMs = current?.workingMs ?? 0;
  const tokensRaw = current?.tokens ?? 0;
  const tokensFormatted = formatTokens(tokensRaw);
  const activeProjectsCount = project ? 1 : (overview?.activeProjects ?? 0);

  const prevTokens = previous?.tokens ?? 0;
  const prevMessages = previous?.messages ?? 0;
  const tokenPctDelta =
    prevTokens > 0 && tokensRaw > 0
      ? Math.round(((tokensRaw - prevTokens) / prevTokens) * 100)
      : null;
  const messageDiff = prevMessages > 0 ? messagesCount - prevMessages : null;

  const shares = overview?.providers ?? [];
  const shareTotal = shares.reduce((sum, s) => sum + s.tokens, 0);
  const providerLabel = (id: string) =>
    providersList.find((p) => p.id === id)?.name || (id ? id.charAt(0).toUpperCase() + id.slice(1) : 'Agent');

  const palette = isLight ? WHITE_LIGHT : WHITE_DARK;
  const monoShades = isLight ? MONO_SHADES_LIGHT : MONO_SHADES_DARK;
  const strongText = isLight ? 'text-black' : 'text-white';
  const mutedText = isLight ? 'text-black/45' : 'text-white/45';
  const faintText = isLight ? 'text-black/35' : 'text-white/35';

  return (
    <div className="w-full max-w-[540px] flex flex-col select-none font-(family-name:--app-font)">
      {/* Top Row: Date + Project Filter (left) & Today / Week / Month Minimalist Toggle (right) */}
      <div className="flex items-center justify-between gap-4 mb-4">
        <div className="flex items-center gap-3 min-w-0">
          <span className={`text-[12.5px] font-normal tracking-tight truncate ${mutedText}`}>
            {todayHeading}
          </span>
          {projectOptions.length > 1 && (
            <div className="relative" ref={projectMenuRef}>
              <button
                type="button"
                onClick={() => setIsProjectOpen((prev) => !prev)}
                className={`text-[12px] tracking-tight transition-colors cursor-pointer ${
                  project !== '' || isProjectOpen
                    ? `${strongText} font-medium`
                    : `${faintText} hover:${mutedText}`
                }`}
              >
                {projectOptions.find((p) => p.id === project)?.label ?? 'All projects'}
              </button>

              {isProjectOpen && (
                <div
                  className={`absolute left-0 top-[24px] z-50 min-w-[170px] max-h-[220px] overflow-y-auto rounded-[10px] p-1 border backdrop-blur-xl shadow-2xl animate-scale-in ${
                    isLight
                      ? 'bg-white/95 border-black/10 text-black'
                      : 'bg-[#141518]/95 border-white/10 text-white'
                  }`}
                >
                  {projectOptions.map((opt) => (
                    <button
                      key={opt.id}
                      type="button"
                      onClick={() => {
                        setProject(opt.id);
                        setIsProjectOpen(false);
                      }}
                      className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-[6px] text-[12px] tracking-tight cursor-pointer ${
                        isLight ? 'hover:bg-black/[0.05]' : 'hover:bg-white/[0.08]'
                      }`}
                    >
                      <span
                        className={`truncate max-w-[130px] ${
                          opt.id === project
                            ? isLight ? 'font-medium text-black' : 'font-medium text-white'
                            : isLight ? 'text-black/60' : 'text-white/60'
                        }`}
                      >
                        {opt.label}
                      </span>
                      {opt.id === project && (
                        <Check size={12} strokeWidth={2.2} className={`${strongText} shrink-0`} />
                      )}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Today | Week | Month Minimalist Text Toggle - No borders, no boxes */}
        <div className="inline-flex items-center gap-3 shrink-0">
          {PERIOD_TABS.map((tab) => {
            const active = period === tab.id;
            return (
              <button
                key={tab.id}
                type="button"
                onClick={() => setPeriod(tab.id)}
                className={`text-[12px] tracking-tight transition-colors duration-150 cursor-pointer ${
                  active
                    ? isLight
                      ? 'text-black font-medium'
                      : 'text-white font-medium'
                    : isLight
                      ? 'text-black/40 hover:text-black/75 font-normal'
                      : 'text-white/40 hover:text-white/75 font-normal'
                }`}
              >
                {tab.label}
              </button>
            );
          })}
        </div>
      </div>

      {/* Editorial Hero Summary Text */}
      <h1
        className={`text-[28px] sm:text-[31px] font-semibold tracking-[-0.03em] leading-[1.2] mb-3 ${mutedText}`}
      >
        <span className={strongText}>{greetingLine}</span>{' '}
        You used{' '}
        <span className={strongText}>{tokensFormatted} tokens</span>{' '}
        {activeTab.phrase}, sent{' '}
        <span className={strongText}>
          {messagesCount.toLocaleString()} {messagesCount === 1 ? 'message' : 'messages'}
        </span>
        , and worked on{' '}
        <span className={strongText}>
          {activeProjectsCount} {activeProjectsCount === 1 ? 'project' : 'projects'}
        </span>
      </h1>

      {/* Comparison Deltas */}
      {(tokenPctDelta !== null && tokenPctDelta !== 0) || (messageDiff !== null && messageDiff !== 0) ? (
        <div className="flex flex-wrap items-center gap-x-5 gap-y-1 mb-7 text-[11.5px] tracking-tight">
          {tokenPctDelta !== null && tokenPctDelta !== 0 && (
            <span className={`inline-flex items-center gap-1 ${mutedText}`}>
              <span className={tokenPctDelta > 0 ? 'text-emerald-400' : 'text-rose-400'}>
                {tokenPctDelta > 0 ? '↗' : '↘'}
              </span>
              {Math.abs(tokenPctDelta)}% {tokenPctDelta > 0 ? 'more' : 'fewer'} tokens than {activeTab.prevLabel}
            </span>
          )}
          {messageDiff !== null && messageDiff !== 0 && (
            <span className={`inline-flex items-center gap-1 ${mutedText}`}>
              <span className={messageDiff > 0 ? 'text-emerald-400' : 'text-rose-400'}>
                {messageDiff > 0 ? '↗' : '↘'}
              </span>
              {Math.abs(messageDiff).toLocaleString()} {messageDiff > 0 ? 'more' : 'fewer'}{' '}
              {Math.abs(messageDiff) === 1 ? 'message' : 'messages'} than {activeTab.prevLabel}
            </span>
          )}
        </div>
      ) : (
        <div className="mb-6" />
      )}

      {/* Activity Heatmap Header */}
      <div className="flex items-center justify-between mb-1.5">
        <span className={`text-[12px] font-medium tracking-tight ${strongText}`}>
          Activity
        </span>
        <div className={`flex items-center gap-1.5 text-[10.5px] tracking-tight ${faintText}`}>
          <span>Less</span>
          <div className="flex items-center gap-[2px]">
            {palette.map((bg, idx) => (
              <span
                key={idx}
                className="w-[7px] h-[7px] rounded-[1.5px]"
                style={{ backgroundColor: bg }}
              />
            ))}
          </div>
          <span>More</span>
        </div>
      </div>

      {/* Year start indicator above the grid */}
      <div className="relative w-full h-[14px] mb-1 overflow-hidden pointer-events-none select-none">
        {thisYearCol >= 0 && (
          <div
            className="absolute top-0 text-[10px] text-white/50 font-mono tracking-tight"
            style={{
              left: `${Math.min(90, Math.max(0, (thisYearCol / cols) * 100))}%`,
            }}
          >
            ↑ {currentYear}
          </div>
        )}
      </div>

      {/* Activity Heatmap Grid: 7 rows x 53 columns */}
      <div className="flex flex-col gap-[2px] mb-5">
        {Array.from({ length: 7 }).map((_, rIdx) => (
          <div key={rIdx} className="grid gap-[2px]" style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
            {Array.from({ length: cols }).map((_, cIdx) => {
              const day = days[cIdx * 7 + rIdx];
              const count = day?.count ?? 0;
              const lvl = getLevel(count);
              const cellBg = palette[lvl];
              const dateStr = day?.date ? formatDate(day.date) : '';
              const tooltip = dateStr
                ? `${count.toLocaleString()} message${count === 1 ? '' : 's'} on ${dateStr}`
                : undefined;

              return (
                <div
                  key={cIdx}
                  title={tooltip}
                  className="aspect-square rounded-[1.5px] transition-colors duration-150"
                  style={{ backgroundColor: cellBg }}
                />
              );
            })}
          </div>
        ))}
      </div>

      {/* Bottom Metrics Row: Sessions, Agents, Working + Provider Breakdown */}
      <div className="flex flex-wrap items-end justify-between gap-6">
        <div className="flex items-baseline gap-8">
          <div>
            <div className={`text-[18px] font-semibold tracking-tight tabular-nums leading-none ${strongText}`}>
              {sessionsCount.toLocaleString()}
            </div>
            <div className={`text-[11px] font-normal tracking-tight mt-1.5 ${mutedText}`}>
              Sessions
            </div>
          </div>

          <div>
            <div className={`text-[18px] font-semibold tracking-tight tabular-nums leading-none ${strongText}`}>
              {agentsCount.toLocaleString()}
            </div>
            <div className={`text-[11px] font-normal tracking-tight mt-1.5 ${mutedText}`}>
              Agents
            </div>
          </div>

          <div>
            <div className={`text-[18px] font-semibold tracking-tight tabular-nums leading-none ${strongText}`}>
              {formatDuration(workingMs)}
            </div>
            <div className={`text-[11px] font-normal tracking-tight mt-1.5 ${mutedText}`}>
              Working
            </div>
          </div>
        </div>

        {/* Provider Share Bar & Legend */}
        {shares.length > 0 && (
          <div className="flex flex-col gap-1.5 flex-1 min-w-[160px] max-w-[230px]">
            <div className="flex w-full h-[3px] gap-[2px] rounded-full overflow-hidden">
              {shares.map((share, idx) => (
                <div
                  key={share.driver}
                  className="h-full rounded-full min-w-[3px]"
                  style={{
                    flexGrow: shareTotal > 0 ? share.tokens : share.messages || 1,
                    flexBasis: 0,
                    backgroundColor: monoShades[idx % monoShades.length],
                  }}
                />
              ))}
            </div>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5">
              {shares.slice(0, 3).map((share, idx) => {
                const pct = shareTotal > 0 ? Math.round((share.tokens / shareTotal) * 100) : 0;
                return (
                  <span
                    key={share.driver}
                    className={`inline-flex items-center gap-1 text-[10.5px] tracking-tight ${mutedText}`}
                  >
                    <span
                      className="w-[4.5px] h-[4.5px] rounded-full shrink-0"
                      style={{ backgroundColor: monoShades[idx % monoShades.length] }}
                    />
                    <span>{providerLabel(share.driver)}</span>
                    <span className="tabular-nums opacity-75">{pct}%</span>
                  </span>
                );
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
