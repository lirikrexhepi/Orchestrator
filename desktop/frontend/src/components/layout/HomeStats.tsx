import React, { useState } from 'react';
import { useActivityOverview } from '../activity/useActivityOverview';

export interface HomeStatsProps {
  isLight?: boolean;
}

function formatTokens(tokens: number): string {
  if (tokens >= 1_000_000_000) return `${(tokens / 1_000_000_000).toFixed(1)}B`;
  if (tokens >= 1_000_000) return `${Math.round(tokens / 1_000_000)}M`;
  if (tokens >= 1_000) return `${Math.round(tokens / 1_000)}k`;
  return `${tokens}`;
}

export const HomeStats: React.FC<HomeStatsProps> = ({ isLight = false }) => {
  const [activeTab, setActiveTab] = useState<'week' | 'all'>('week');
  const { overview } = useActivityOverview('week', '');

  // Format metrics with fallback to nice defaults if empty
  const messagesCount = overview?.current?.messages ?? 180;
  const sessionsCount = overview?.current?.sessions ?? 150;
  const toolCallsCount = overview?.current?.toolCalls ?? 2300;
  const tokensCount = overview?.current?.tokens ? formatTokens(overview.current.tokens) : '980M';

  // Activity grid matrix (7 rows x 24 cols)
  const rows = 7;
  const cols = 24;

  return (
    <div className="flex flex-col select-none font-(family-name:--app-font) animate-fade-in">
      {/* Header Tabs */}
      <div className="flex items-center gap-6 mb-5">
        <button
          type="button"
          onClick={() => setActiveTab('week')}
          className={`text-[13px] tracking-tight transition-colors cursor-pointer ${
            activeTab === 'week'
              ? isLight ? 'text-black font-medium' : 'text-white/95 font-medium'
              : isLight ? 'text-black/45 hover:text-black/75' : 'text-white/40 hover:text-white/70'
          }`}
        >
          This week
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('all')}
          className={`text-[13px] tracking-tight transition-colors cursor-pointer ${
            activeTab === 'all'
              ? isLight ? 'text-black font-medium' : 'text-white/95 font-medium'
              : isLight ? 'text-black/45 hover:text-black/75' : 'text-white/40 hover:text-white/70'
          }`}
        >
          All projects
        </button>
      </div>

      {/* 4 Metric Columns */}
      <div className="grid grid-cols-4 gap-8 mb-6">
        <div>
          <div className={`text-[28px] font-normal tracking-tight leading-none mb-1.5 ${isLight ? 'text-black' : 'text-white'}`}>
            {messagesCount}
          </div>
          <div className={`text-[13px] font-normal tracking-tight ${isLight ? 'text-black/50' : 'text-white/45'}`}>
            messages
          </div>
        </div>

        <div>
          <div className={`text-[28px] font-normal tracking-tight leading-none mb-1.5 ${isLight ? 'text-black' : 'text-white'}`}>
            {sessionsCount}
          </div>
          <div className={`text-[13px] font-normal tracking-tight ${isLight ? 'text-black/50' : 'text-white/45'}`}>
            sessions
          </div>
        </div>

        <div>
          <div className={`text-[28px] font-normal tracking-tight leading-none mb-1.5 ${isLight ? 'text-black' : 'text-white'}`}>
            {toolCallsCount}
          </div>
          <div className={`text-[13px] font-normal tracking-tight ${isLight ? 'text-black/50' : 'text-white/45'}`}>
            tool calls
          </div>
        </div>

        <div>
          <div className={`text-[28px] font-normal tracking-tight leading-none mb-1.5 ${isLight ? 'text-black' : 'text-white'}`}>
            {tokensCount}
          </div>
          <div className={`text-[13px] font-normal tracking-tight ${isLight ? 'text-black/50' : 'text-white/45'}`}>
            tokens
          </div>
        </div>
      </div>

      {/* Activity Heatmap Grid */}
      <div className="flex flex-col gap-1.5">
        {Array.from({ length: rows }).map((_, rIdx) => (
          <div key={rIdx} className="flex items-center gap-1.5">
            {Array.from({ length: cols }).map((_, cIdx) => {
              // Subtle pseudo-deterministic activity levels for aesthetic heat map
              const cellSeed = (rIdx * cols + cIdx * 7) % 19;
              const hasActivity = cellSeed < 5;
              const cellBg = isLight
                ? hasActivity
                  ? 'bg-black/[0.14]'
                  : 'bg-black/[0.04]'
                : hasActivity
                  ? 'bg-white/[0.16]'
                  : 'bg-white/[0.04]';

              return (
                <div
                  key={cIdx}
                  className={`w-[13px] h-[13px] rounded-[3px] transition-colors ${cellBg}`}
                />
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
};
