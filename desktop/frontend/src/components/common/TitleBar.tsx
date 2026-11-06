import React, { useCallback, useEffect, useState } from 'react';
import {
  Quit,
  WindowIsMaximised,
  WindowMinimise,
  WindowToggleMaximise,
} from '../../../wailsjs/runtime/runtime';

/**
 * Window chrome for a frameless window.
 *
 * The strip itself is transparent so the wallpaper runs to the top edge; only
 * the controls are drawn. Everything else in the strip is a drag region, which
 * is what replaces the title bar the OS no longer provides.
 */
export interface TitleBarProps {
  children?: React.ReactNode;
}

export const TitleBar: React.FC<TitleBarProps> = ({ children }) => {
  const [isMaximised, setMaximised] = useState(false);

  const syncMaximised = useCallback(() => {
    void WindowIsMaximised()
      .then(setMaximised)
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    syncMaximised();
    // The window can also be maximised by snapping or a double-click on the
    // drag strip, neither of which routes through the button below.
    window.addEventListener('resize', syncMaximised);
    return () => window.removeEventListener('resize', syncMaximised);
  }, [syncMaximised]);

  const toggleMaximise = useCallback(() => {
    WindowToggleMaximise();
    // The runtime reports the old value if asked immediately, so the read waits
    // for the resize to land.
    window.setTimeout(syncMaximised, 60);
  }, [syncMaximised]);

  return (
    <div className="fixed top-0 left-0 right-0 h-[52px] z-[60] flex items-center pointer-events-none select-none">
      {/* Full-width drag surface (stopping before the traffic lights). Double-click toggles maximise. */}
      <div
        className="absolute left-[60px] right-[110px] top-0 h-[48px] pointer-events-auto"
        style={{ ['--wails-draggable' as string]: 'drag' }}
        onDoubleClick={toggleMaximise}
      />

      {/* Center: Top Dynamic Island */}
      {children && (
        <div
          className="absolute left-1/2 -translate-x-1/2 top-3 z-20 pointer-events-auto flex items-center justify-center"
          style={{ ['--wails-draggable' as string]: 'no-drag' }}
        >
          {children}
        </div>
      )}

      {/* Traffic lights: right-aligned and vertically centered with the 34px pill (Y ~ 14px from top) */}
      <div
        className="absolute right-4 top-[14px] flex items-center gap-2 pointer-events-auto z-50"
        style={{ ['--wails-draggable' as string]: 'no-drag' }}
      >
        <TrafficLight
          label={isMaximised ? 'Restore' : 'Maximise'}
          onClick={toggleMaximise}
          className="bg-[#28c840] hover:bg-[#34d94c]"
        />
        <TrafficLight
          label="Minimise"
          onClick={WindowMinimise}
          className="bg-[#febc2e] hover:bg-[#ffc846]"
        />
        <TrafficLight
          label="Close"
          onClick={Quit}
          className="bg-[#ff5f57] hover:bg-[#ff6f68]"
        />
      </div>
    </div>
  );
};

interface TrafficLightProps {
  label: string;
  className: string;
  onClick: () => void;
}

/**
 * One 12px macOS-style window control.
 */
const TrafficLight: React.FC<TrafficLightProps> = ({ label, className, onClick }) => (
  <button
    type="button"
    title={label}
    aria-label={label}
    onClick={(e) => {
      e.stopPropagation();
      onClick();
    }}
    style={{ ['--wails-draggable' as string]: 'no-drag' }}
    className={`w-[12px] h-[12px] rounded-full transition-all duration-150 active:scale-90 cursor-pointer ring-1 ring-inset ring-black/20 ${className}`}
  />
);

export default TitleBar;
