import React, { useState, useRef, useEffect, useMemo } from 'react';
import { ActivityPeriod, useActivityOverview } from '../activity/useActivityOverview';
import { useProjects } from '../orchestrator/useProjects';
import { Check } from '../common/icons';
import { activity } from '../../../wailsjs/go/models';

export interface HomeStatsProps {
  isLight?: boolean;
}

const PERIOD_OPTIONS: { id: ActivityPeriod; label: string }[] = [
  { id: 'week', label: 'This week' },
  { id: 'month', label: 'This month' },
  { id: 'year', label: 'This year' },
];

const WHITE_DARK = [
  '#18191e',                    // Level 0: empty
  'rgba(255, 255, 255, 0.22)', // Level 1: low
  'rgba(255, 255, 255, 0.45)', // Level 2: medium
  'rgba(255, 255, 255, 0.70)', // Level 3: high
  'rgba(255, 255, 255, 0.95)', // Level 4: peak
];

const WHITE_LIGHT = [
  'rgba(0, 0, 0, 0.05)',
  'rgba(0, 0, 0, 0.22)',
  'rgba(0, 0, 0, 0.45)',
  'rgba(0, 0, 0, 0.70)',
  'rgba(0, 0, 0, 0.92)',
];

function formatTokens(tokens: number): string {
  if (tokens >= 1_000_000_000) return `${(tokens / 1_000_000_000).toFixed(1)}B`;
  if (tokens >= 1_000_000) return `${Math.round(tokens / 1_000_000)}M`;
  if (tokens >= 1_000) return `${Math.round(tokens / 1_000)}k`;
  return `${tokens}`;
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

export const HomeStats: React.FC<HomeStatsProps> = ({ isLight = false }) => {
  const [period, setPeriod] = useState<ActivityPeriod>('week');
  const [project, setProject] = useState<string>('');
  const [isPeriodOpen, setIsPeriodOpen] = useState(false);
  const [isProjectOpen, setIsProjectOpen] = useState(false);

  const periodMenuRef = useRef<HTMLDivElement>(null);
  const projectMenuRef = useRef<HTMLDivElement>(null);

  const { overview } = useActivityOverview(period, project);
  const allProjects = useProjects();

  // Close menus on outside click or Escape
  useEffect(() => {
    if (!isPeriodOpen && !isProjectOpen) return;

    const handlePointerDown = (e: MouseEvent) => {
      if (
        periodMenuRef.current &&
        !periodMenuRef.current.contains(e.target as Node)
      ) {
        setIsPeriodOpen(false);
      }
      if (
        projectMenuRef.current &&
        !projectMenuRef.current.contains(e.target as Node)
      ) {
        setIsProjectOpen(false);
      }
    };

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setIsPeriodOpen(false);
        setIsProjectOpen(false);
      }
    };

    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [isPeriodOpen, isProjectOpen]);

  // Project filter options
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

  // Format activity days into 24 columns x 7 rows
  const days = useMemo(() => {
    const raw = overview?.days ?? [];
    if (raw.length === 0) {
      return Array.from({ length: 168 }, () => ({ date: '', count: 0 } as activity.Day));
    }
    const list = [...raw];
    const rem = list.length % 7;
    if (rem !== 0) {
      for (let i = 0; i < 7 - rem; i++) {
        list.push({ date: '', count: 0 } as activity.Day);
      }
    }
    const target = 24 * 7;
    if (list.length < target) {
      const pad = Array.from({ length: target - list.length }, () => ({ date: '', count: 0 } as activity.Day));
      return [...pad, ...list];
    }
    return list.slice(-target);
  }, [overview?.days]);

  // Maximum commits/messages in any day for scaling
  const max = useMemo(() => {
    return days.reduce((acc, d) => Math.max(acc, d.count), 0);
  }, [days]);

  const getLevel = (count: number) => {
    if (count <= 0 || max === 0) return 0;
    return Math.min(4, Math.ceil((count / max) * 4));
  };

  // Metrics from overview
  const current = overview?.current;
  const messagesCount = current?.messages ?? 0;
  const sessionsCount = current?.sessions ?? 0;
  const toolCallsCount = current?.toolCalls ?? 0;
  const tokensCount = current?.tokens ? formatTokens(current.tokens) : '0';

  const palette = isLight ? WHITE_LIGHT : WHITE_DARK;

  return (
    <div className="w-[356px] flex flex-col select-none font-(family-name:--app-font)">
      {/* Header Filters: "This week" & "All projects" */}
      <div className="flex items-center gap-5 mb-3.5">
        {/* Period Filter Dropdown */}
        <div className="relative" ref={periodMenuRef}>
          <button
            type="button"
            onClick={() => {
              setIsPeriodOpen((prev) => !prev);
              setIsProjectOpen(false);
            }}
            className={`text-[13px] tracking-tight transition-colors cursor-pointer ${
              period !== 'week' || isPeriodOpen
                ? isLight ? 'text-black font-medium' : 'text-white font-medium'
                : isLight ? 'text-black/85 font-medium' : 'text-white/95 font-medium'
            }`}
          >
            {PERIOD_OPTIONS.find((p) => p.id === period)?.label ?? 'This week'}
          </button>

          {isPeriodOpen && (
            <div
              className={`absolute left-0 top-[26px] z-50 min-w-[140px] rounded-[10px] p-1 border backdrop-blur-xl shadow-2xl animate-scale-in ${
                isLight
                  ? 'bg-white/95 border-black/10 text-black'
                  : 'bg-[#141518]/95 border-white/10 text-white'
              }`}
            >
              {PERIOD_OPTIONS.map((opt) => (
                <button
                  key={opt.id}
                  type="button"
                  onClick={() => {
                    setPeriod(opt.id);
                    setIsPeriodOpen(false);
                  }}
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-[6px] text-[12px] tracking-tight cursor-pointer ${
                    isLight ? 'hover:bg-black/[0.05]' : 'hover:bg-white/[0.08]'
                  }`}
                >
                  <span className={opt.id === period ? 'font-medium text-white' : 'text-white/60'}>
                    {opt.label}
                  </span>
                  {opt.id === period && <Check size={12} strokeWidth={2.2} className="text-white shrink-0" />}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Project Filter Dropdown */}
        <div className="relative" ref={projectMenuRef}>
          <button
            type="button"
            onClick={() => {
              setIsProjectOpen((prev) => !prev);
              setIsPeriodOpen(false);
            }}
            className={`text-[13px] tracking-tight transition-colors cursor-pointer ${
              project !== '' || isProjectOpen
                ? isLight ? 'text-black font-medium' : 'text-white font-medium'
                : isLight ? 'text-black/40 hover:text-black/75' : 'text-white/40 hover:text-white/70'
            }`}
          >
            {projectOptions.find((p) => p.id === project)?.label ?? 'All projects'}
          </button>

          {isProjectOpen && (
            <div
              className={`absolute left-0 top-[26px] z-50 min-w-[170px] max-h-[220px] overflow-y-auto rounded-[10px] p-1 border backdrop-blur-xl shadow-2xl animate-scale-in ${
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
                  <span className={`truncate max-w-[130px] ${opt.id === project ? 'font-medium text-white' : 'text-white/60'}`}>
                    {opt.label}
                  </span>
                  {opt.id === project && <Check size={12} strokeWidth={2.2} className="text-white shrink-0" />}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* 4 Metric Columns evenly spaced across the 356px width */}
      <div className="flex items-baseline justify-between w-full mb-5">
        <div>
          <div className={`text-[25px] font-normal tracking-tight leading-none ${isLight ? 'text-black' : 'text-white'}`}>
            {messagesCount.toLocaleString()}
          </div>
          <div className={`text-[11.5px] font-normal tracking-tight mt-1.5 ${isLight ? 'text-black/45' : 'text-white/40'}`}>
            messages
          </div>
        </div>

        <div>
          <div className={`text-[25px] font-normal tracking-tight leading-none ${isLight ? 'text-black' : 'text-white'}`}>
            {sessionsCount.toLocaleString()}
          </div>
          <div className={`text-[11.5px] font-normal tracking-tight mt-1.5 ${isLight ? 'text-black/45' : 'text-white/40'}`}>
            sessions
          </div>
        </div>

        <div>
          <div className={`text-[25px] font-normal tracking-tight leading-none ${isLight ? 'text-black' : 'text-white'}`}>
            {toolCallsCount.toLocaleString()}
          </div>
          <div className={`text-[11.5px] font-normal tracking-tight mt-1.5 ${isLight ? 'text-black/45' : 'text-white/40'}`}>
            tool calls
          </div>
        </div>

        <div>
          <div className={`text-[25px] font-normal tracking-tight leading-none ${isLight ? 'text-black' : 'text-white'}`}>
            {tokensCount}
          </div>
          <div className={`text-[11.5px] font-normal tracking-tight mt-1.5 ${isLight ? 'text-black/45' : 'text-white/40'}`}>
            tokens
          </div>
        </div>
      </div>

      {/* Activity Heatmap Grid: 7 rows x 24 columns */}
      <div className="flex flex-col gap-[4px]">
        {Array.from({ length: 7 }).map((_, rIdx) => (
          <div key={rIdx} className="flex items-center gap-[4px]">
            {Array.from({ length: 24 }).map((_, cIdx) => {
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
                  className="w-[11px] h-[11px] rounded-[2.5px] transition-colors duration-150"
                  style={{
                    backgroundColor: cellBg,
                  }}
                />
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
};
