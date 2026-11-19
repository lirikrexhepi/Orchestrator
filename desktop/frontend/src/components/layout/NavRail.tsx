import React from 'react';
import {
  House,
  Folder,
  Settings,
  MessageSquarePlus,
  Globe,
  Terminal,
  GitBranch,
  Plus,
  MaterialIcon,
} from '../common/icons';
import type { AgentCardMode } from '../agent-session';
import { getFaviconUrl, type WebBookmark } from '../agent-session/useWebBookmarks';

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
  onGitClick?: () => void;
  hasPreview?: boolean;
  hasServers?: boolean;
  hasChanges?: boolean;
  hasTasks?: boolean;
  // Bookmarks on sidepanel
  bookmarks?: WebBookmark[];
  currentBrowserUrl?: string;
  onBookmarkClick?: (bm: WebBookmark) => void;
  onBookmarkEdit?: (bm: WebBookmark) => void;
  onAddBookmark?: () => void;
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
  onGitClick,
  hasPreview = false,
  hasServers = false,
  hasChanges = false,
  hasTasks = false,
  bookmarks = [],
  currentBrowserUrl,
  onBookmarkClick,
  onBookmarkEdit,
  onAddBookmark,
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

            {/* Preview / Browser view icon - Accessible at all times */}
            <button
              type="button"
              onClick={() => onModeSelect?.('browser')}
              className={getIconClass(cardMode === 'browser')}
              title="Web Browser"
              aria-label="Web Browser"
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

            {/* Changes / Git icon - Accessible, opens Git remote repo in built-in browser */}
            <button
              type="button"
              onClick={() => {
                if (onGitClick) {
                  onGitClick();
                } else {
                  onModeSelect?.('changes');
                }
              }}
              className={getIconClass(cardMode === 'changes')}
              title="Git Repository"
              aria-label="Git Repository"
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

        {/* Bookmarks Section (Universal & Project Bookmarks on Side Panel) */}
        {((bookmarks && bookmarks.length > 0) || onAddBookmark) && (
          <div className="flex flex-col items-center w-full">
            <div className={`w-3.5 h-[1px] my-1.5 ${isLight ? 'bg-black/10' : 'bg-white/10'}`} />

            <div className="flex flex-col items-center gap-1.5 w-full max-h-[220px] overflow-y-auto no-scrollbar py-0.5">
              {bookmarks.map((bm) => {
                const isCurrent =
                  currentBrowserUrl &&
                  currentBrowserUrl.replace(/\/+$/, '').toLowerCase() ===
                    bm.url.replace(/\/+$/, '').toLowerCase();
                return (
                  <button
                    key={bm.id}
                    type="button"
                    onClick={() => onBookmarkClick?.(bm)}
                    onContextMenu={(e) => {
                      e.preventDefault();
                      onBookmarkEdit?.(bm);
                    }}
                    title={`${bm.title}\n${bm.url}${bm.project ? `\n(Project: ${bm.project})` : '\n(Universal)'}\nRight-click to edit`}
                    className={`w-[28px] h-[28px] rounded-[7px] flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 relative group ${
                      isCurrent
                        ? isLight
                          ? 'bg-black/15 shadow-xs ring-1 ring-black/20'
                          : 'bg-white/20 shadow-xs ring-1 ring-white/30'
                        : isLight
                        ? 'hover:bg-black/[0.06]'
                        : 'hover:bg-white/[0.08]'
                    }`}
                  >
                    <img
                      src={getFaviconUrl(bm.url)}
                      alt={bm.title}
                      className="w-[15px] h-[15px] object-contain rounded-xs pointer-events-none"
                      onError={(e) => {
                        (e.currentTarget as HTMLElement).style.display = 'none';
                      }}
                    />
                    {/* Project-specific indicator dot */}
                    {bm.project && (
                      <span
                        title={`Project: ${bm.project}`}
                        className="absolute -top-0.5 -right-0.5 w-1.5 h-1.5 rounded-full bg-blue-400 ring-1 ring-[#141414]"
                      />
                    )}
                  </button>
                );
              })}

              {onAddBookmark && (
                <button
                  type="button"
                  onClick={onAddBookmark}
                  title="Add Web Bookmark"
                  aria-label="Add Web Bookmark"
                  className={`w-[28px] h-[28px] rounded-[7px] flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 ${
                    isLight
                      ? 'text-black/35 hover:text-black/80 hover:bg-black/[0.04]'
                      : 'text-white/35 hover:text-white/90 hover:bg-white/[0.04]'
                  }`}
                >
                  <Plus size={15} strokeWidth={1.8} />
                </button>
              )}
            </div>
          </div>
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
