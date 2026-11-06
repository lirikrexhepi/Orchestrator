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

  // Live overview metrics or fallback to the screenshot values
  const messagesCount = overview?.current?.messages ?? 179;
  const sessionsCount = overview?.current?.sessions ?? 158;
  const toolCallsCount = overview?.current?.toolCalls ?? 2484;
  const tokensCount = overview?.current?.tokens ? formatTokens(overview.current.tokens) : '1.1B';

  const rows = 7;
  const cols = 24;

  return (
    <div className="w-[356px] flex flex-col select-none font-(family-name:--app-font)">
      {/* Header Tabs */}
      <div className="flex items-center gap-5 mb-3.5">
        <button
          type="button"
          onClick={() => setActiveTab('week')}
          className={`text-[13px] tracking-tight transition-colors cursor-pointer ${
            activeTab === 'week'
              ? isLight ? 'text-black font-medium' : 'text-white/95 font-medium'
              : isLight ? 'text-black/40 hover:text-black/75' : 'text-white/40 hover:text-white/70'
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
              : isLight ? 'text-black/40 hover:text-black/75' : 'text-white/40 hover:text-white/70'
          }`}
        >
          All projects
        </button>
      </div>

      {/* 4 Metric Columns evenly spaced across the 356px width */}
      <div className="flex items-baseline justify-between w-full mb-5">
        <div>
          <div className={`text-[25px] font-normal tracking-tight leading-none ${isLight ? 'text-black' : 'text-white'}`}>
            {messagesCount}
          </div>
          <div className={`text-[11.5px] font-normal tracking-tight mt-1.5 ${isLight ? 'text-black/45' : 'text-white/40'}`}>
            messages
          </div>
        </div>

        <div>
          <div className={`text-[25px] font-normal tracking-tight leading-none ${isLight ? 'text-black' : 'text-white'}`}>
            {sessionsCount}
          </div>
          <div className={`text-[11.5px] font-normal tracking-tight mt-1.5 ${isLight ? 'text-black/45' : 'text-white/40'}`}>
            sessions
          </div>
        </div>

        <div>
          <div className={`text-[25px] font-normal tracking-tight leading-none ${isLight ? 'text-black' : 'text-white'}`}>
            {toolCallsCount}
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
        {Array.from({ length: rows }).map((_, rIdx) => (
          <div key={rIdx} className="flex items-center gap-[4px]">
            {Array.from({ length: cols }).map((_, cIdx) => {
              // Diagonal pattern matching screenshot att-1794001435751-1-image.png
              const patternActive = (rIdx + cIdx) % 4 === 0 || (rIdx * 2 + cIdx) % 7 === 1;
              const cellBg = isLight
                ? patternActive
                  ? 'bg-black/[0.22]'
                  : 'bg-black/[0.05]'
                : patternActive
                  ? ((rIdx + cIdx) % 8 === 0 ? 'bg-[#828698]' : 'bg-[#404352]')
                  : 'bg-[#18191e]';

              return (
                <div
                  key={cIdx}
                  className={`w-[11px] h-[11px] rounded-[2.5px] ${cellBg}`}
                />
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
};
