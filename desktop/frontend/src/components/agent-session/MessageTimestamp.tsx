import { MaterialIcon } from '../common/icons';
import React, { useState, useCallback } from 'react';
import { formatModelBadge, renderChatIcon } from '../common/DynamicIsland';
import { useTheme } from '../../themes';

export interface MessageTimestampProps {
  timestamp?: number;
  content?: string;
  className?: string;
  align?: 'left' | 'right';
  driver?: string;
  model?: string;
  isStreaming?: boolean;
  isLight?: boolean;
}

const MONTH_FORMAT = new Intl.DateTimeFormat('en-US', { month: 'short' });
const TIME_FORMAT = new Intl.DateTimeFormat('en-US', {
  hour: 'numeric',
  minute: '2-digit',
  hour12: true,
});
const FULL_DATE_FORMAT = new Intl.DateTimeFormat();

/**
 * Formats a message timestamp matching Cursor / Composer style:
 * e.g. "Sep 18 at 10:03 PM" (or with year if different year).
 */
export function formatMessageTime(timestamp?: number | string | Date): string {
  if (!timestamp) return '';
  const date = typeof timestamp === 'number' || typeof timestamp === 'string'
    ? new Date(timestamp)
    : timestamp;
  if (isNaN(date.getTime())) return '';

  const now = new Date();
  const isSameYear = date.getFullYear() === now.getFullYear();

  const month = MONTH_FORMAT.format(date);
  const day = date.getDate();
  const time = TIME_FORMAT.format(date);

  if (isSameYear) {
    return `${month} ${day} at ${time}`;
  }
  return `${month} ${day}, ${date.getFullYear()} at ${time}`;
}

/**
 * MessageTimestamp Component
 * Clean, subtle message timestamp, current model indicator, and copy action.
 */
const MessageTimestampImpl: React.FC<MessageTimestampProps> = ({
  timestamp,
  content,
  className = '',
  align = 'left',
  driver,
  model,
  isStreaming = false,
  isLight: propIsLight,
}) => {
  const { currentTheme } = useTheme();
  const isLight = propIsLight ?? (currentTheme?.id === 'light' || currentTheme?.id === 'white');
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback((e: React.MouseEvent) => {
    e.stopPropagation();
    if (!content) return;
    navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }, [content]);

  const modelLabel = model || driver ? formatModelBadge(model, driver) : null;
  const formattedTime = React.useMemo(() => timestamp ? formatMessageTime(timestamp) : '', [timestamp]);
  const fullDateTitle = React.useMemo(() => timestamp ? FULL_DATE_FORMAT.format(new Date(timestamp)) : '', [timestamp]);

  if (!formattedTime && !modelLabel) return null;

  return (
    <div
      className={`flex items-center gap-1.5 pt-1.5 text-[11px] font-(family-name:--app-font) ${
        isLight ? 'text-black/45' : 'text-white/40'
      } select-none ${
        align === 'right' ? 'justify-end' : 'justify-start'
      } ${className}`}
    >
      {modelLabel && (
        <div
          className={`inline-flex items-center gap-1.5 transition-colors ${
            isLight ? 'text-black/60 hover:text-black/90' : 'text-white/60 hover:text-white/90'
          }`}
          title={driver ? `${driver}${model ? `: ${model}` : ''}` : model}
        >
          {renderChatIcon(driver, model, isLight, 'w-[12px] h-[12px]')}
          <span className="font-medium tracking-tight">{modelLabel}</span>
        </div>
      )}

      {modelLabel && formattedTime && (
        <span className="opacity-40">·</span>
      )}

      {formattedTime && (
        <span
          title={fullDateTitle}
          className={`tracking-tight transition-colors ${
            isLight ? 'hover:text-black/75' : 'hover:text-white/70'
          }`}
        >
          {formattedTime}
        </span>
      )}

      {content && !isStreaming && (
        <button
          type="button"
          title={copied ? 'Copied!' : 'Copy message'}
          onClick={handleCopy}
          className={`w-[18px] h-[18px] rounded flex items-center justify-center transition-all cursor-pointer active:scale-90 ${
            isLight
              ? 'text-black/35 hover:text-black/90 hover:bg-black/[0.06]'
              : 'text-white/35 hover:text-white/90 hover:bg-white/10'
          }`}
        >
          <MaterialIcon name={copied ? 'check' : 'content_copy'} className="text-[13px]"/>
        </button>
      )}
    </div>
  );
};

export const MessageTimestamp = React.memo(MessageTimestampImpl);
MessageTimestamp.displayName = 'MessageTimestamp';

export default MessageTimestamp;
