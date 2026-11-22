import React, { useEffect, useRef, useState } from 'react';
import type { AppUpdateState } from '../sidebar/useAppUpdate';

export interface NavRailUpdateButtonProps {
  appUpdate?: AppUpdateState;
  isLight?: boolean;
  fallbackVersion?: string;
}

export const NavRailUpdateButton: React.FC<NavRailUpdateButtonProps> = ({
  appUpdate,
  isLight = false,
  fallbackVersion,
}) => {
  const [isHovered, setIsHovered] = useState(false);
  const [isPinned, setIsPinned] = useState(false);
  const cardRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  const status = appUpdate?.status;
  const isAvailable = Boolean(status?.available);
  const isInstalling = Boolean(appUpdate?.installing);
  const progress = appUpdate?.progress;
  const error = appUpdate?.error || (progress?.stage === 'error' ? progress.error : null);
  const version = status?.latestVersion || fallbackVersion || '';

  // Close pinned card when clicking outside while not actively installing.
  useEffect(() => {
    if (!isPinned || isInstalling) return;
    const handleOutsideClick = (e: MouseEvent) => {
      const target = e.target as Node | null;
      if (
        cardRef.current &&
        !cardRef.current.contains(target) &&
        buttonRef.current &&
        !buttonRef.current.contains(target)
      ) {
        setIsPinned(false);
      }
    };
    window.addEventListener('mousedown', handleOutsideClick);
    return () => window.removeEventListener('mousedown', handleOutsideClick);
  }, [isPinned, isInstalling]);

  // Keep card open while installing so user can track progress effortlessly.
  useEffect(() => {
    if (isInstalling) {
      setIsPinned(true);
    }
  }, [isInstalling]);

  if (!isAvailable && !isInstalling && !error) {
    return null;
  }

  const percent = Math.min(100, Math.max(0, progress?.percent ?? (isInstalling ? 10 : 0)));
  const stage = progress?.stage || (isInstalling ? 'downloading' : 'idle');

  let stageLabel = 'Downloading update...';
  if (stage === 'verifying') stageLabel = 'Verifying integrity...';
  if (stage === 'installing') stageLabel = 'Installing & relaunching...';
  if (stage === 'error' || error) stageLabel = 'Update failed';

  const handleButtonClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (isInstalling) {
      setIsPinned((prev) => !prev);
      return;
    }
    if (appUpdate?.install) {
      setIsPinned(true);
      void appUpdate.install();
    }
  };

  const handleInstallClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (appUpdate?.install && !isInstalling) {
      setIsPinned(true);
      void appUpdate.install();
    }
  };

  const showCard = isPinned || isHovered;

  // Circular ring constants for 20x20 SVG (r = 8)
  const radius = 8;
  const circumference = 2 * Math.PI * radius;
  const strokeDashoffset = circumference * (1 - percent / 100);

  return (
    <div
      className="relative flex items-center justify-center"
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
    >
      {/* 28x28 Nav Rail Action Button */}
      <button
        ref={buttonRef}
        type="button"
        onClick={handleButtonClick}
        aria-label={version ? `Update to v${version}` : 'Update available'}
        className={`w-[28px] h-[28px] rounded-[7px] flex items-center justify-center transition-all duration-150 relative cursor-pointer active:scale-90 ${
          isInstalling
            ? isLight
              ? 'bg-[#0A84FF]/15 text-[#0066CC] shadow-sm'
              : 'bg-[#0A84FF]/20 text-[#0A84FF] shadow-sm'
            : isLight
              ? 'bg-[#0A84FF]/10 text-[#0066CC] hover:bg-[#0A84FF]/20 border border-[#0A84FF]/25 shadow-sm'
              : 'bg-[#0A84FF]/15 text-[#0A84FF] hover:bg-[#0A84FF]/25 border border-[#0A84FF]/30 shadow-sm'
        }`}
      >
        {isInstalling ? (
          <div className="relative w-5 h-5 flex items-center justify-center">
            {/* Circular Progress Ring */}
            <svg className="w-5 h-5 -rotate-90" viewBox="0 0 20 20">
              <circle
                cx="10"
                cy="10"
                r={radius}
                className={isLight ? 'stroke-black/10' : 'stroke-white/15'}
                strokeWidth="2"
                fill="none"
              />
              <circle
                cx="10"
                cy="10"
                r={radius}
                className="stroke-[#0A84FF] transition-all duration-200"
                strokeWidth="2"
                strokeDasharray={circumference}
                strokeDashoffset={strokeDashoffset}
                strokeLinecap="round"
                fill="none"
              />
            </svg>
            <span className="absolute inset-0 flex items-center justify-center text-[7.5px] font-mono font-bold leading-none tabular-nums select-none">
              {stage === 'installing' || stage === 'verifying' ? '•••' : `${Math.round(percent)}`}
            </span>
          </div>
        ) : (
          <>
            {/* Modern Download / Update Icon */}
            <svg
              width="15"
              height="15"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              className="transition-transform duration-150 group-hover:translate-y-0.5"
            >
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="7 10 12 15 17 10" />
              <line x1="12" y1="15" x2="12" y2="3" />
            </svg>
            {/* Glowing Accent Dot */}
            <span
              className={`absolute -top-0.5 -right-0.5 w-2 h-2 rounded-full bg-[#0A84FF] ring-2 ${
                isLight ? 'ring-[#e6e6eb]' : 'ring-[#141414]'
              } animate-pulse`}
            />
          </>
        )}
      </button>

      {/* Modern Floating Update Progress Card */}
      {showCard && (
        <div
          ref={cardRef}
          role="dialog"
          aria-label="Application Update"
          className={`absolute left-[38px] bottom-0 z-50 w-[224px] rounded-xl p-3 shadow-2xl backdrop-blur-xl border select-none transition-all duration-150 ${
            isLight
              ? 'bg-white/95 border-black/10 text-black shadow-black/15'
              : 'bg-[#18181b]/95 border-white/10 text-white shadow-black/60'
          }`}
        >
          {/* Card Header */}
          <div className="flex items-center justify-between gap-2 pb-2 border-b border-white/[0.06]">
            <div className="flex items-center gap-1.5 min-w-0">
              <span className="w-5 h-5 rounded-md bg-[#0A84FF]/20 text-[#0A84FF] flex items-center justify-center shrink-0">
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2.4"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                  <polyline points="7 10 12 15 17 10" />
                  <line x1="12" y1="15" x2="12" y2="3" />
                </svg>
              </span>
              <span className="text-[12px] font-semibold tracking-tight truncate">
                {isInstalling ? 'Updating App' : 'Update Ready'}
              </span>
            </div>
            {version && (
              <span
                className={`text-[10px] font-mono px-1.5 py-0.5 rounded shrink-0 ${
                  isLight ? 'bg-black/[0.06] text-black/70' : 'bg-white/10 text-white/70'
                }`}
              >
                v{version}
              </span>
            )}
          </div>

          {/* Card Body */}
          <div className="pt-2">
            {error ? (
              <div className="space-y-2">
                <div className="text-[11px] text-red-400 font-medium leading-tight">{error}</div>
                <button
                  type="button"
                  onClick={handleInstallClick}
                  className="w-full py-1.5 px-3 rounded-lg bg-red-500/20 hover:bg-red-500/30 text-red-300 text-[11px] font-medium transition-colors cursor-pointer"
                >
                  Retry Update
                </button>
              </div>
            ) : isInstalling ? (
              <div className="space-y-2">
                <div className="flex items-center justify-between text-[11px]">
                  <span
                    className={`font-medium ${isLight ? 'text-black/60' : 'text-white/60'}`}
                  >
                    {stageLabel}
                  </span>
                  <span className="text-[#0A84FF] font-mono font-semibold tabular-nums">
                    {Math.round(percent)}%
                  </span>
                </div>

                {/* Progress Bar */}
                <div
                  className={`w-full h-1.5 rounded-full overflow-hidden ${
                    isLight ? 'bg-black/10' : 'bg-white/10'
                  }`}
                >
                  <div
                    className="h-full bg-[#0A84FF] rounded-full transition-all duration-200"
                    style={{ width: `${percent}%` }}
                  />
                </div>

                <div
                  className={`text-[10px] leading-tight ${
                    isLight ? 'text-black/40' : 'text-white/40'
                  }`}
                >
                  Orchestrator will relaunch automatically when done.
                </div>
              </div>
            ) : (
              <div className="space-y-2.5">
                <div
                  className={`text-[11px] leading-snug ${
                    isLight ? 'text-black/60' : 'text-white/60'
                  }`}
                >
                  A new release is available with performance and feature improvements.
                </div>

                <button
                  type="button"
                  onClick={handleInstallClick}
                  className="w-full py-1.5 px-3 rounded-lg bg-[#0A84FF] hover:bg-[#0077ED] active:scale-[0.98] text-white text-[12px] font-medium flex items-center justify-center gap-1.5 transition-all shadow-sm cursor-pointer"
                >
                  <svg
                    width="13"
                    height="13"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2.2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                    <polyline points="7 10 12 15 17 10" />
                    <line x1="12" y1="15" x2="12" y2="3" />
                  </svg>
                  <span>Install &amp; Relaunch</span>
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default NavRailUpdateButton;
