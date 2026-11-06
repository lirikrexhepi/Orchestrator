import React from 'react';
import { House, Folder, Settings, MessageSquarePlus } from '../common/icons';

export interface NavRailProps {
  activeView: 'home' | 'chat';
  hasActiveTasks: boolean;
  onHomeClick: () => void;
  onNewChatClick: () => void;
  onProjectsClick: () => void;
  onSettingsClick: () => void;
  isProjectsOpen?: boolean;
  isSettingsOpen?: boolean;
  isLight?: boolean;
}

export const NavRail: React.FC<NavRailProps> = ({
  activeView,
  hasActiveTasks,
  onHomeClick,
  onNewChatClick,
  onProjectsClick,
  onSettingsClick,
  isProjectsOpen = false,
  isSettingsOpen = false,
  isLight = false,
}) => {
  const iconBase = 'w-[38px] h-[38px] rounded-xl flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-95';
  const getIconClass = (isActive: boolean) => {
    if (isLight) {
      return `${iconBase} ${
        isActive
          ? 'text-black bg-black/[0.08]'
          : 'text-black/45 hover:text-black/80 hover:bg-black/[0.04]'
      }`;
    }
    return `${iconBase} ${
      isActive
        ? 'text-white bg-white/[0.12] shadow-[0_0_12px_rgba(255,255,255,0.06)]'
        : 'text-white/40 hover:text-white/85 hover:bg-white/[0.05]'
    }`;
  };

  return (
    <aside
      className={`w-[58px] h-full shrink-0 flex flex-col items-center justify-between py-4 select-none z-30 transition-colors ${
        isLight ? 'bg-[#f4f4f6] border-r border-black/[0.06]' : 'bg-[#0d0e10] border-r border-white/[0.05]'
      }`}
    >
      {/* Top action icons */}
      <div className="flex flex-col items-center gap-2">
        {/* Home icon */}
        <button
          type="button"
          onClick={onHomeClick}
          className={getIconClass(activeView === 'home')}
          title="Home / Activity"
          aria-label="Home"
        >
          <House size={20} strokeWidth={1.8} />
        </button>

        {/* New chat icon */}
        <button
          type="button"
          onClick={onNewChatClick}
          className={getIconClass(false)}
          title="New Chat (Ctrl+N)"
          aria-label="New Chat"
        >
          <MessageSquarePlus size={20} strokeWidth={1.8} />
        </button>

        {/* Workspaces / Folder icon */}
        <button
          type="button"
          onClick={onProjectsClick}
          className={getIconClass(isProjectsOpen)}
          title="Projects & Workspaces"
          aria-label="Projects"
        >
          <Folder size={20} strokeWidth={1.8} />
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
          <Settings size={20} strokeWidth={1.8} />
        </button>
      </div>
    </aside>
  );
};
