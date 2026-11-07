import React from 'react';
import {
  House,
  Folder,
  Settings,
  MessageSquarePlus,
  Globe,
  Terminal,
  GitBranch,
  MaterialIcon,
} from '../common/icons';
import type { AgentCardMode } from '../agent-session';

export interface NavRailProps {
  activeView: 'home' | 'chat';
  onHomeClick: () => void;
  onNewChatClick: () => void;
  onProjectsClick: () => void;
  onSettingsClick: () => void;
  isSettingsOpen?: boolean;
  isLight?: boolean;
  // Card mode controls
  cardMode?: AgentCardMode;
  onModeSelect?: (mode: AgentCardMode) => void;
  hasPreview?: boolean;
  hasServers?: boolean;
  hasChanges?: boolean;
  hasTasks?: boolean;
}

export const NavRail: React.FC<NavRailProps> = ({
  activeView,
  onHomeClick,
  onNewChatClick,
  onProjectsClick,
  onSettingsClick,
  isSettingsOpen = false,
  isLight = false,
  cardMode = 'chat',
  onModeSelect,
  hasPreview = false,
  hasServers = false,
  hasChanges = false,
  hasTasks = false,
}) => {
  const iconBase =
    'w-[28px] h-[28px] rounded-[7px] flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90';

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

  const getModeIconClass = (isActive: boolean, isAvailable: boolean) => {
    if (!isAvailable) {
      return `${iconBase} opacity-20 cursor-not-allowed pointer-events-none ${
        isLight ? 'text-black' : 'text-white'
      }`;
    }
    return getIconClass(isActive);
  };

  return (
    <aside
      className={`w-[36px] h-full shrink-0 flex flex-col items-center justify-between py-3 px-1 select-none z-30 ${
        isLight ? 'bg-[#e6e6eb]' : 'bg-[#141414]'
      }`}
    >
      {/* Top action icons */}
      <div className="flex flex-col items-center gap-1.5">
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

        {/* Mode switcher icons (only active in chat view) */}
        {activeView === 'chat' && (
          <>
            {/* Subtle Divider */}
            <div className={`w-3.5 h-[1px] my-1 ${isLight ? 'bg-black/10' : 'bg-white/10'}`} />

            {/* Chat view icon */}
            <button
              type="button"
              onClick={() => onModeSelect?.('chat')}
              className={getIconClass(cardMode === 'chat')}
              title="Chat"
              aria-label="Chat"
            >
              <MaterialIcon name="chat" size="15px" />
            </button>

            {/* Preview / Browser view icon */}
            <button
              type="button"
              disabled={!hasPreview}
              onClick={() => onModeSelect?.('browser')}
              className={getModeIconClass(cardMode === 'browser', hasPreview)}
              title={hasPreview ? 'Preview (Web Browser)' : 'Preview (no web server detected)'}
              aria-label="Preview"
            >
              <Globe size={15} strokeWidth={1.8} />
            </button>

            {/* Servers view icon */}
            <button
              type="button"
              disabled={!hasServers}
              onClick={() => onModeSelect?.('servers')}
              className={getModeIconClass(cardMode === 'servers', hasServers)}
              title={hasServers ? 'Servers' : 'Servers (none running)'}
              aria-label="Servers"
            >
              <Terminal size={15} strokeWidth={1.8} />
            </button>

            {/* Changes / Git diff icon */}
            <button
              type="button"
              disabled={!hasChanges}
              onClick={() => onModeSelect?.('changes')}
              className={getModeIconClass(cardMode === 'changes', hasChanges)}
              title={hasChanges ? 'Git Changes' : 'Git Changes (working tree clean)'}
              aria-label="Changes"
            >
              <GitBranch size={15} strokeWidth={1.8} />
            </button>

            {/* Tasks view icon */}
            <button
              type="button"
              disabled={!hasTasks}
              onClick={() => onModeSelect?.('tasklist')}
              className={getModeIconClass(cardMode === 'tasklist', hasTasks)}
              title={hasTasks ? 'Tasks' : 'Tasks (none in progress)'}
              aria-label="Tasks"
            >
              <MaterialIcon name="checklist" size="15px" />
            </button>
          </>
        )}
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
