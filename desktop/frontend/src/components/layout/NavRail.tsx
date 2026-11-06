import React from 'react';
import { House, Folder, Settings, MessageSquarePlus } from '../common/icons';

export interface NavRailProps {
  activeView: 'home' | 'chat';
  onHomeClick: () => void;
  onNewChatClick: () => void;
  onProjectsClick: () => void;
  onSettingsClick: () => void;
  isSettingsOpen?: boolean;
  isLight?: boolean;
}

export const NavRail: React.FC<NavRailProps> = ({
  activeView,
  onHomeClick,
  onNewChatClick,
  onProjectsClick,
  onSettingsClick,
  isSettingsOpen = false,
  isLight = false,
}) => {
  const iconBase = 'w-[28px] h-[28px] rounded-[7px] flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90';

  const getIconClass = (isActive: boolean) => {
    if (isLight) {
      return `${iconBase} ${
        isActive
          ? 'text-black bg-black/[0.08]'
          : 'text-black/40 hover:text-black/80 hover:bg-black/[0.04]'
      }`;
    }
    return `${iconBase} ${
      isActive
        ? 'text-white bg-white/[0.08]'
        : 'text-white/40 hover:text-white/90 hover:bg-white/[0.04]'
    }`;
  };

  return (
    <aside className="w-[36px] h-full shrink-0 flex flex-col items-center justify-between py-3 px-1 select-none z-30 bg-transparent">
      {/* Top action icons */}
      <div className="flex flex-col items-center gap-2">
        {/* Home icon */}
        <button
          type="button"
          onClick={onHomeClick}
          className={getIconClass(activeView === 'home')}
          title="Home"
          aria-label="Home"
        >
          <House size={16} strokeWidth={1.8} />
        </button>

        {/* New chat icon */}
        <button
          type="button"
          onClick={onNewChatClick}
          className={getIconClass(false)}
          title="New Chat (Ctrl+N)"
          aria-label="New Chat"
        >
          <MessageSquarePlus size={16} strokeWidth={1.8} />
        </button>

        {/* Workspaces / Folder icon -> triggers Dynamic Island projects view */}
        <button
          type="button"
          onClick={onProjectsClick}
          className={getIconClass(false)}
          title="Projects"
          aria-label="Projects"
        >
          <Folder size={16} strokeWidth={1.8} />
        </button>
      </div>

      {/* Bottom action icon: Settings only */}
      <div className="flex flex-col items-center">
        <button
          type="button"
          onClick={onSettingsClick}
          className={getIconClass(isSettingsOpen)}
          title="Settings"
          aria-label="Settings"
        >
          <Settings size={16} strokeWidth={1.8} />
        </button>
      </div>
    </aside>
  );
};
