import { MaterialIcon } from '../common/icons';
import React, { useEffect, useRef, useState } from 'react';
import { WorkRow, formatElapsed } from './WorkRow';
import { ROW_DOT_Y, TimelineDot, useInTimeline } from './Timeline';
import { TextShimmer } from './TextShimmer';

export interface ThinkingBlockProps {
  isThinking?: boolean;
  thoughtText?: string;
  durationSeconds?: number;
  defaultExpanded?: boolean;
  className?: string;
}

const ThinkingBlockImpl: React.FC<ThinkingBlockProps> = ({
  isThinking = false,
  thoughtText = '',
  durationSeconds = 0,
  defaultExpanded = false,
}) => {
  const startedAt = useRef(0);
  const [settled, setSettled] = useState<number | null>(null);

  useEffect(() => {
    if (isThinking) {
      startedAt.current = Date.now();
      setSettled(null);
      return;
    }
    if (startedAt.current > 0) {
      setSettled((Date.now() - startedAt.current) / 1000);
      startedAt.current = 0;
    }
  }, [isThinking]);

  const seconds = durationSeconds > 0 ? durationSeconds : settled;
  const label = isThinking ? 'Thinking' : seconds && seconds >= 1 ? `Thought for ${formatElapsed(seconds)}` : 'Thought';
  const text = thoughtText.trim();
  const inTimeline = useInTimeline();
  const [expanded, setExpanded] = useState(defaultExpanded);
  const long = text.length > 150 || text.split(/\n/).length > 2;

  if (text) {
    return (
      <div className="relative flex flex-col min-w-0 font-(family-name:--app-font) py-[3px]">
        {inTimeline && <TimelineDot y={ROW_DOT_Y} tone="row" />}
        <div className="text-[11px] font-medium tracking-tight text-current/40 leading-[18px]">
          {isThinking ? <TextShimmer duration={1.6}>{label}</TextShimmer> : label}
        </div>
        <div
          onClick={() => long && setExpanded((value) => !value)}
          className={`mt-0.5 text-[13px] leading-[1.65] tracking-tight text-current/90 whitespace-pre-wrap select-text ${
            expanded ? '' : 'line-clamp-2'
          } ${long ? 'cursor-pointer' : ''}`}
        >
          {text}
        </div>
        {long && (
          <button
            type="button"
            onClick={() => setExpanded((value) => !value)}
            className="self-start mt-0.5 text-[11px] text-current/35 hover:text-current/70 transition-colors cursor-pointer"
          >
            {expanded ? 'Show less' : 'Show more'}
          </button>
        )}
      </div>
    );
  }

  return (
    <WorkRow
      label={label}
      running={isThinking}
      defaultOpen={defaultExpanded}
    >
      {text ? (
        <div className="border-l border-current/[0.12] pl-3 text-[12px] leading-[1.65] tracking-tight text-current/50 whitespace-pre-wrap select-text max-h-[320px] overflow-y-auto custom-scrollbar">
          {text}
        </div>
      ) : undefined}
    </WorkRow>
  );
};

export const ThinkingBlock = React.memo(ThinkingBlockImpl);
ThinkingBlock.displayName = 'ThinkingBlock';

export default ThinkingBlock;
