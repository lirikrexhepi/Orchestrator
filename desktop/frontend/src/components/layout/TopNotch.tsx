import React from 'react';
import { GlassPill } from '../../ui/glass/GlassPill';
import { RotateCcw } from '../common/icons';
import antigravityLogo from '../../assets/logo/antigravity-icon-logo.png';

export interface TopNotchProps {
  onHistoryClick: () => void;
  isHistoryOpen?: boolean;
  isLight?: boolean;
  activeTitle?: string;
}

export const TopNotch: React.FC<TopNotchProps> = ({
  onHistoryClick,
  isHistoryOpen = false,
  isLight = false,
  activeTitle,
}) => {
  return (
    <div className="fixed top-3 left-1/2 -translate-x-1/2 z-40 flex items-center select-none pointer-events-auto">
      <GlassPill
        height={34}
        fill={isLight ? 'rgba(255, 255, 255, 0.85)' : 'rgba(15, 16, 18, 0.92)'}
        frost={14}
        border={{
          strokeWidth: 1,
          align: 'inside',
          light: {
            angle: 303,
            intensity: isLight ? 0.6 : 0.75,
            backIntensity: 0.6,
          },
        }}
        style={{
          padding: '0 10px',
          gap: 12,
          minWidth: 180,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          boxShadow: isLight
            ? '0 4px 16px rgba(0, 0, 0, 0.06)'
            : '0 4px 20px rgba(0, 0, 0, 0.45)',
        }}
      >
        {/* Left: History icon */}
        <button
          type="button"
          onClick={onHistoryClick}
          className={`w-[22px] h-[22px] rounded-full flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 ${
            isHistoryOpen
              ? isLight ? 'text-black' : 'text-white'
              : isLight
                ? 'text-black/50 hover:text-black/90'
                : 'text-white/50 hover:text-white/90'
          }`}
          title="Past Chats & History"
          aria-label="History"
          style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
        >
          <RotateCcw size={14} strokeWidth={2} />
        </button>

        {/* Center: Drag strip for frameless window movement */}
        <div
          className="flex-1 h-full flex items-center justify-center cursor-default min-w-[60px]"
          style={{ WebkitAppRegion: 'drag' } as React.CSSProperties}
        >
          {activeTitle ? (
            <span
              className={`text-[11.5px] font-medium tracking-tight truncate max-w-[140px] ${
                isLight ? 'text-black/60' : 'text-white/60'
              }`}
            >
              {activeTitle}
            </span>
          ) : (
            <div className="w-1.5 h-1.5 rounded-full bg-white/10" />
          )}
        </div>

        {/* Right: Antigravity colorful wave logo */}
        <div
          className="w-[20px] h-[20px] flex items-center justify-center shrink-0 cursor-default"
          style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
        >
          <img
            src={antigravityLogo}
            alt="Antigravity"
            className="w-[18px] h-[18px] object-contain select-none pointer-events-none drop-shadow-[0_0_8px_rgba(59,130,246,0.35)]"
          />
        </div>
      </GlassPill>
    </div>
  );
};
