import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Reorder, useDragControls, motion, AnimatePresence } from 'motion/react';
import { Archive, ArchiveRestore, ChevronLeft, ChevronRight, GripVertical, Plus, Search, X, MaterialIcon } from './icons';
import { ScrollArea } from './ScrollArea';
import { ProjectGlyph } from '../orchestrator/ProjectGlyph';
import { providerIcon } from '../orchestrator/providerIcons';
import type { Project, ProjectsState } from '../orchestrator/useProjects';
import { claudeimport } from '../../../wailsjs/go/models';
import type { ChatHistoryItem } from './DynamicIsland';
import { beginChatDrag, ChatDragPayload, isChatDrag, readChatDrag } from './chatDrag';
import { setChatDragHint } from './ChatDragLayer';

export interface IslandTone {
  isLight: boolean;
  fg: string;
  sub: string;
  faint: string;
  hover: string;
  active: string;
  divider: string;
}

export function islandTone(isLight: boolean): IslandTone {
  return isLight
    ? {
        isLight,
        fg: 'text-black/90',
        sub: 'text-black/45',
        faint: 'text-black/30',
        hover: 'hover:bg-black/[0.045]',
        active: 'bg-black/[0.07]',
        divider: 'bg-black/[0.07]',
      }
    : {
        isLight,
        fg: 'text-white',
        sub: 'text-white/45',
        faint: 'text-white/25',
        hover: 'hover:bg-white/[0.06]',
        active: 'bg-white/[0.1]',
        divider: 'bg-white/[0.07]',
      };
}

export function IconButton({
  title,
  onClick,
  disabled,
  tone,
  children,
}: {
  title: string;
  onClick: () => void;
  disabled?: boolean;
  tone: IslandTone;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      disabled={disabled}
      onClick={(event) => {
        event.stopPropagation();
        onClick();
      }}
      className={`w-[26px] h-[26px] rounded-full flex items-center justify-center ${tone.sub} ${tone.hover} ${
        tone.isLight ? 'hover:text-black' : 'hover:text-white'
      } active:scale-90 transition-all duration-150 cursor-pointer disabled:opacity-40 disabled:cursor-default shrink-0`}
    >
      {children}
    </button>
  );
}

const SPRING_OPEN_MS = 450;

export function IslandHeader({
  tone,
  title,
  icon,
  onBack,
  backLabel,
  actions,
  springBack = false,
}: {
  tone: IslandTone;
  title: string;
  icon?: React.ReactNode;
  onBack?: () => void;
  backLabel?: string;
  actions?: React.ReactNode;
  springBack?: boolean;
}) {
  const timer = useRef<number | null>(null);
  const [armed, setArmed] = useState(false);
  const disarm = () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = null;
    setArmed(false);
  };
  useEffect(() => disarm, []);
  const spring = springBack && onBack
    ? {
        onDragEnter: (event: React.DragEvent) => {
          if (!isChatDrag(event.dataTransfer) || timer.current !== null) return;
          setArmed(true);
          setChatDragHint(`Open ${backLabel ?? 'back'}…`);
          timer.current = window.setTimeout(() => {
            timer.current = null;
            setArmed(false);
            setChatDragHint(null);
            onBack();
          }, SPRING_OPEN_MS);
        },
        onDragOver: (event: React.DragEvent) => {
          if (isChatDrag(event.dataTransfer)) event.preventDefault();
        },
        onDragLeave: () => {
          disarm();
          setChatDragHint(null);
        },
        onDrop: () => disarm(),
      }
    : {};
  return (
    <div className="relative flex items-center justify-between h-[30px] mb-1.5 px-0.5">
      {onBack ? (
        <button
          type="button"
          onClick={onBack}
          {...spring}
          className={`flex items-center gap-0.5 h-[26px] pl-0.5 pr-2 rounded-full text-[12px] font-medium ${tone.sub} ${tone.hover} ${
            armed ? (tone.isLight ? 'bg-[#0A84FF]/15 text-[#0A84FF]' : 'bg-[#0A84FF]/25 text-white') : ''
          } transition-colors cursor-pointer z-[1]`}
        >
          <ChevronLeft size={15} strokeWidth={2} />
          {backLabel}
        </button>
      ) : (
        <span className={`text-[13px] font-semibold tracking-tight ${tone.fg} pl-1.5`}>{title}</span>
      )}
      {onBack && (
        <span
          className={`absolute inset-x-0 mx-auto w-fit max-w-[150px] flex items-center gap-1.5 text-[12.5px] font-semibold tracking-tight truncate ${tone.fg} pointer-events-none`}
        >
          {icon}
          <span className="truncate">{title}</span>
        </span>
      )}
      <div className="flex items-center gap-0.5 z-[1]">{actions}</div>
    </div>
  );
}

function EmptyState({ tone, children }: { tone: IslandTone; children: React.ReactNode }) {
  return <div className={`py-9 text-center text-[12px] ${tone.sub}`}>{children}</div>;
}

const ProjectRow = React.memo(function ProjectRow({
  project,
  tone,
  isActive,
  chatCount,
  canRemove,
  onSelect,
  onOpenChats,
  onRemove,
  onDragEnd,
  onChatDrop,
}: {
  project: Project;
  tone: IslandTone;
  isActive: boolean;
  chatCount: number;
  canRemove: boolean;
  onSelect: () => void;
  onOpenChats: () => void;
  onRemove: () => void;
  onDragEnd: () => void;
  onChatDrop?: (payload: ChatDragPayload) => void;
}) {
  const controls = useDragControls();
  const [confirming, setConfirming] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [dropping, setDropping] = useState(false);
  const depth = useRef(0);
  const dropTarget = onChatDrop
    ? {
        onDragEnter: (event: React.DragEvent) => {
          if (!isChatDrag(event.dataTransfer)) return;
          depth.current += 1;
          setDropping(true);
          setChatDragHint(`Move to ${project.name}`);
        },
        onDragOver: (event: React.DragEvent) => {
          if (!isChatDrag(event.dataTransfer)) return;
          event.preventDefault();
          event.dataTransfer.dropEffect = 'move';
        },
        onDragLeave: () => {
          depth.current = Math.max(0, depth.current - 1);
          if (depth.current === 0) {
            setDropping(false);
            setChatDragHint(null);
          }
        },
        onDrop: (event: React.DragEvent) => {
          depth.current = 0;
          setDropping(false);
          const payload = readChatDrag(event.dataTransfer);
          if (!payload) return;
          event.preventDefault();
          onChatDrop(payload);
        },
      }
    : {};

  return (
    <Reorder.Item
      value={project.id}
      dragListener={false}
      dragControls={controls}
      onDragStart={() => setDragging(true)}
      onDragEnd={() => {
        setDragging(false);
        onDragEnd();
      }}
      layout="position"
      transition={{ type: 'spring', stiffness: 520, damping: 40, mass: 0.7 }}
      whileDrag={{ scale: 1.025 }}
      {...dropTarget}
      className={`group relative flex items-center h-[44px] rounded-[13px] select-none ${
        dragging ? (tone.isLight ? 'bg-white shadow-[0_8px_24px_rgba(0,0,0,0.12)]' : 'bg-[#1d1d20] shadow-[0_10px_30px_rgba(0,0,0,0.6)]') : ''
      } ${dropping ? (tone.isLight ? 'bg-[#0A84FF]/12 ring-1 ring-[#0A84FF]/50' : 'bg-[#0A84FF]/20 ring-1 ring-[#0A84FF]/60') : ''} ${
        !dragging && !dropping && isActive ? tone.active : ''
      } ${!dragging ? tone.hover : ''} transition-colors duration-150`}
      style={{ zIndex: dragging ? 5 : 0 }}
    >
      <span
        onPointerDown={(event) => {
          event.preventDefault();
          setConfirming(false);
          controls.start(event);
        }}
        title="Drag to reorder"
        className={`w-[22px] h-full flex items-center justify-center shrink-0 cursor-grab active:cursor-grabbing ${tone.faint} opacity-0 group-hover:opacity-100 transition-opacity duration-150 touch-none`}
      >
        <GripVertical size={13} strokeWidth={2} />
      </span>

      <AnimatePresence mode="popLayout" initial={false}>
        {confirming ? (
          <motion.div
            key="confirm"
            initial={{ opacity: 0, x: 8 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: 8 }}
            transition={{ duration: 0.14 }}
            className="flex-1 min-w-0 flex items-center gap-1.5 pr-1.5"
          >
            <span className={`text-[12px] font-medium tracking-tight truncate flex-1 ${tone.fg}`}>
              Remove {project.name}?
            </span>
            <button
              type="button"
              onClick={() => {
                setConfirming(false);
                onRemove();
              }}
              className="h-[26px] px-3 rounded-full bg-rose-500/85 hover:bg-rose-500 text-white text-[11.5px] font-medium active:scale-95 transition-all cursor-pointer shrink-0"
            >
              Remove
            </button>
            <button
              type="button"
              onClick={() => setConfirming(false)}
              className={`h-[26px] px-3 rounded-full text-[11.5px] font-medium active:scale-95 transition-all cursor-pointer shrink-0 ${
                tone.isLight ? 'bg-black/[0.06] hover:bg-black/[0.1] text-black/80' : 'bg-white/[0.08] hover:bg-white/[0.14] text-white/80'
              }`}
            >
              Keep
            </button>
          </motion.div>
        ) : (
          <motion.div
            key="row"
            initial={{ opacity: 0, x: -8 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: -8 }}
            transition={{ duration: 0.14 }}
            className="flex-1 min-w-0 flex items-center h-full"
          >
            <button
              type="button"
              onClick={onSelect}
              className="flex-1 min-w-0 h-full flex items-center gap-2.5 text-left cursor-pointer"
            >
              <span className="w-[18px] flex items-center justify-center shrink-0">
                <ProjectGlyph
                  projectId={project.id}
                  size={16}
                  glyph={<MaterialIcon name="folder" className={`text-[16px] ${tone.sub}`}/>}
                />
              </span>
              <span className="flex flex-col min-w-0 gap-[1px]">
                <span className={`flex items-center gap-1.5 text-[12.5px] font-medium tracking-tight leading-[15px] ${tone.fg}`}>
                  <span className="truncate">{project.name}</span>
                  {isActive && <span className="w-[5px] h-[5px] rounded-full bg-[#0A84FF] shrink-0" />}
                </span>
                <span className={`text-[10.5px] font-['Geist_Mono',monospace] truncate leading-[13px] ${tone.sub}`} title={project.path}>
                  {shortenPath(project.path)}
                </span>
              </span>
            </button>
            {canRemove && (
              <button
                type="button"
                title="Remove project"
                onClick={() => setConfirming(true)}
                className={`w-[26px] h-[26px] rounded-full flex items-center justify-center ${tone.sub} ${tone.hover} opacity-0 group-hover:opacity-100 focus:opacity-100 transition-all duration-150 cursor-pointer shrink-0`}
              >
                <X size={13} strokeWidth={2} />
              </button>
            )}
            <button
              type="button"
              title={`${chatCount} chat${chatCount === 1 ? '' : 's'}`}
              onClick={onOpenChats}
              className={`h-[30px] ml-0.5 mr-1.5 pl-2.5 pr-1.5 rounded-full flex items-center gap-1 ${tone.sub} ${tone.hover} transition-colors duration-150 cursor-pointer shrink-0`}
            >
              <span className="text-[11.5px] tabular-nums font-medium">{chatCount}</span>
              <ChevronRight size={14} strokeWidth={2} />
            </button>
          </motion.div>
        )}
      </AnimatePresence>
    </Reorder.Item>
  );
});

export function shortenPath(path: string): string {
  const home = path.match(/^([a-zA-Z]:\\Users\\[^\\]+|\/(?:home|Users)\/[^/]+)/);
  const trimmed = home ? `~${path.slice(home[0].length)}` : path;
  return trimmed.length > 34 ? `…${trimmed.slice(-33)}` : trimmed;
}

export function ProjectsView({
  tone,
  projects,
  chatCounts,
  claudeCount,
  recentClaude = [],
  relativeTime,
  onOpenClaudeSession,
  onClose,
  onPicked,
  onOpenProjectChats,
  onOpenClaude,
  onMoveChat,
}: {
  tone: IslandTone;
  projects?: ProjectsState;
  chatCounts: Record<string, number>;
  claudeCount: number;
  recentClaude?: claudeimport.ExternalSession[];
  relativeTime?: (timestamp: number) => string;
  onOpenClaudeSession?: (session: claudeimport.ExternalSession) => void;
  onClose: () => void;
  onPicked: () => void;
  onOpenProjectChats: (project: Project) => void;
  onOpenClaude: () => void;
  onMoveChat?: (payload: ChatDragPayload, project: Project) => void;
}) {
  const list = projects?.projects ?? [];
  const [order, setOrder] = useState<string[]>(() => list.map((p) => p.id));
  const orderRef = useRef(order);
  orderRef.current = order;

  useEffect(() => {
    setOrder(list.map((p) => p.id));
  }, [list]);

  const byId = useMemo(() => new Map(list.map((p) => [p.id, p])), [list]);

  const commit = (id: string) => {
    const to = orderRef.current.indexOf(id);
    const from = list.findIndex((p) => p.id === id);
    if (to >= 0 && from >= 0 && to !== from) void projects?.move(id, to);
  };

  return (
    <div className="flex flex-col p-2.5 pt-2">
      <IslandHeader
        tone={tone}
        title="Projects"
        actions={
          <>
            <IconButton
              tone={tone}
              title="Add project folder"
              disabled={projects?.isChoosing}
              onClick={() => {
                void projects?.choose();
                onClose();
              }}
            >
              <Plus size={15} strokeWidth={2} />
            </IconButton>
            <IconButton tone={tone} title="Close" onClick={onClose}>
              <X size={14} strokeWidth={2} />
            </IconButton>
          </>
        }
      />
      <ScrollArea maxHeight={300} className="-mr-1 pr-1">
        {list.length === 0 ? (
          <EmptyState tone={tone}>No projects yet</EmptyState>
        ) : (
          <Reorder.Group axis="y" values={order} onReorder={setOrder} className="flex flex-col gap-0.5" layoutScroll>
            {order.map((id) => {
              const project = byId.get(id);
              if (!project) return null;
              return (
                <ProjectRow
                  key={id}
                  project={project}
                  tone={tone}
                  isActive={project.id === projects?.active?.id}
                  chatCount={chatCounts[project.id] ?? 0}
                  canRemove={list.length > 1}
                  onSelect={() => {
                    void projects?.select(project.id);
                    onPicked();
                  }}
                  onOpenChats={() => onOpenProjectChats(project)}
                  onRemove={() => void projects?.remove(project.id)}
                  onDragEnd={() => commit(project.id)}
                  onChatDrop={onMoveChat ? (payload) => onMoveChat(payload, project) : undefined}
                />
              );
            })}
          </Reorder.Group>
        )}
        <div className={`h-px mx-2 my-1.5 ${tone.divider}`} />
        <button
          type="button"
          onClick={onOpenClaude}
          className={`w-full flex items-center gap-2.5 h-[44px] pl-[22px] pr-1.5 rounded-[13px] text-left ${tone.hover} transition-colors duration-150 cursor-pointer`}
        >
          <span className="w-[18px] flex items-center justify-center shrink-0">
            <img src={providerIcon('claude', tone.isLight)} alt="" draggable={false} className="w-[16px] h-[16px] object-contain" />
          </span>
          <span className="flex flex-col min-w-0 flex-1 gap-[1px]">
            <span className={`text-[12.5px] font-medium tracking-tight leading-[15px] ${tone.fg}`}>CLI chats</span>
            <span className={`text-[10.5px] leading-[13px] truncate ${tone.sub}`}>Claude Code, Antigravity, OpenCode</span>
          </span>
          <span className={`h-[30px] pl-2.5 pr-0 flex items-center gap-1 ${tone.sub} shrink-0`}>
            {claudeCount > 0 && <span className="text-[11.5px] tabular-nums font-medium">{claudeCount}</span>}
            <ChevronRight size={14} strokeWidth={2} />
          </span>
        </button>
        {recentClaude.length > 0 && (
          <div className="flex flex-col pl-[22px] pr-1.5 pb-1">
            {recentClaude.map((chat) => (
              <button
                key={chat.filePath}
                type="button"
                onClick={() => onOpenClaudeSession?.(chat)}
                className={`w-full flex items-center gap-2.5 h-[32px] pl-[28px] pr-2 rounded-[11px] text-left ${tone.hover} transition-colors duration-150 cursor-pointer`}
              >
                <span className={`flex-1 min-w-0 truncate text-[12px] tracking-tight ${tone.fg}`}>{chat.title || 'Untitled chat'}</span>
                {relativeTime && (
                  <span className={`text-[10.5px] tabular-nums shrink-0 ${tone.sub}`}>{relativeTime(chat.updatedAt)}</span>
                )}
              </button>
            ))}
          </div>
        )}
      </ScrollArea>
    </div>
  );
}

export interface ChatGroup {
  label: string;
  chats: ChatHistoryItem[];
}

export function ProjectChatsView({
  tone,
  project,
  groups,
  archived,
  runningThreadIds,
  renderIcon,
  subtitle,
  onBack,
  onClose,
  onNewChat,
  onOpen,
  onSetArchived,
}: {
  tone: IslandTone;
  project: Project;
  groups: ChatGroup[];
  archived: ChatHistoryItem[];
  runningThreadIds: Set<string>;
  renderIcon: (chat: ChatHistoryItem) => React.ReactNode;
  subtitle: (chat: ChatHistoryItem) => string;
  onBack: () => void;
  onClose: () => void;
  onNewChat: () => void;
  onOpen: (chat: ChatHistoryItem) => void;
  onSetArchived: (chat: ChatHistoryItem, archived: boolean) => void;
}) {
  const [showArchived, setShowArchived] = useState(false);
  const empty = groups.length === 0;

  const row = (chat: ChatHistoryItem, isArchived: boolean) => {
    const running = runningThreadIds.has(chat.threadId);
    return (
      <motion.div
        key={chat.id}
        draggable
        onDragStartCapture={(event: React.DragEvent) =>
          beginChatDrag(event, {
            workspaceId: chat.workspaceId,
            threadId: chat.threadId,
            title: chat.title,
            subtitle: subtitle(chat),
            driver: chat.driver,
            model: chat.model,
            projectPath: project.path,
          })
        }
        title="Drag into a message to give an agent this chat, or onto another project to move it"
        layout="position"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0, height: 0 }}
        transition={{ duration: 0.16 }}
        onClick={() => onOpen(chat)}
        className={`group flex items-center gap-2.5 h-[42px] px-2 rounded-[12px] cursor-pointer transition-colors duration-150 ${tone.hover}`}
      >
        <span className={`shrink-0 ${isArchived ? 'opacity-50' : ''}`}>{renderIcon(chat)}</span>
        <span className="flex flex-col min-w-0 flex-1 gap-[1px]">
          <span className={`text-[12.5px] font-medium tracking-tight truncate leading-[15px] ${isArchived ? tone.sub : tone.fg}`}>
            {chat.title}
          </span>
          <span className={`flex items-center gap-1.5 text-[10.5px] leading-[13px] min-w-0 ${tone.sub}`}>
            {running && (
              <span className="flex items-center gap-1 text-emerald-400 font-medium shrink-0">
                <span className="w-[5px] h-[5px] rounded-full bg-emerald-400 animate-pulse" />
                Running
              </span>
            )}
            <span className="truncate">{subtitle(chat)}</span>
          </span>
        </span>
        <span className="opacity-0 group-hover:opacity-100 transition-opacity duration-150">
          <IconButton
            tone={tone}
            title={isArchived ? 'Restore' : 'Archive'}
            onClick={() => onSetArchived(chat, !isArchived)}
          >
            {isArchived ? <ArchiveRestore size={14} strokeWidth={1.9} /> : <Archive size={14} strokeWidth={1.9} />}
          </IconButton>
        </span>
      </motion.div>
    );
  };

  return (
    <div className="flex flex-col p-2.5 pt-2">
      <IslandHeader
        tone={tone}
        title={project.name}
        onBack={onBack}
        backLabel="Projects"
        springBack
        actions={
          <>
            <IconButton tone={tone} title={`New chat in ${project.name}`} onClick={onNewChat}>
              <Plus size={15} strokeWidth={2} />
            </IconButton>
            <IconButton tone={tone} title="Close" onClick={onClose}>
              <X size={14} strokeWidth={2} />
            </IconButton>
          </>
        }
      />
      <ScrollArea maxHeight={320} className="-mr-1 pr-1">
        {empty && !showArchived ? <EmptyState tone={tone}>No chats yet</EmptyState> : null}
        <div className="flex flex-col gap-2">
          {groups.map((group) => (
            <div key={group.label} className="flex flex-col">
              <span className={`px-2 pt-1 pb-1 text-[10.5px] font-medium tracking-tight ${tone.faint}`}>{group.label}</span>
              <AnimatePresence initial={false}>{group.chats.map((chat) => row(chat, false))}</AnimatePresence>
            </div>
          ))}
        </div>
        {archived.length > 0 && (
          <div className="flex flex-col mt-1">
            <button
              type="button"
              onClick={() => setShowArchived((value) => !value)}
              className={`flex items-center gap-1.5 h-[30px] px-2 rounded-[10px] text-[11.5px] font-medium ${tone.sub} ${tone.hover} transition-colors cursor-pointer self-start`}
            >
              <Archive size={12.5} strokeWidth={1.9} />
              Archived · {archived.length}
              <ChevronRight
                size={13}
                strokeWidth={2}
                className={`transition-transform duration-200 ${showArchived ? 'rotate-90' : ''}`}
              />
            </button>
            <AnimatePresence initial={false}>
              {showArchived && (
                <motion.div
                  key="archived"
                  initial={{ opacity: 0, height: 0 }}
                  animate={{ opacity: 1, height: 'auto' }}
                  exit={{ opacity: 0, height: 0 }}
                  transition={{ type: 'spring', stiffness: 420, damping: 38 }}
                  className="overflow-hidden flex flex-col"
                >
                  <AnimatePresence initial={false}>{archived.map((chat) => row(chat, true))}</AnimatePresence>
                </motion.div>
              )}
            </AnimatePresence>
          </div>
        )}
      </ScrollArea>
    </div>
  );
}

const ENTRYPOINT_LABEL: Record<string, string> = {
  'claude-desktop': 'Desktop',
  cli: 'CLI',
  'claude-vscode': 'VS Code',
};

function LoadMoreSentinel({ busy, onVisible }: { busy: boolean; onVisible: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const node = ref.current;
    if (!node || busy) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) onVisible();
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [busy, onVisible]);
  return <div ref={ref} className="h-6 flex items-center justify-center" aria-hidden="true" />;
}

type ChatSourceId = 'claude' | 'antigravity' | 'opencode';

const CHAT_SOURCES: Array<{ id: ChatSourceId; label: string }> = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'antigravity', label: 'Antigravity' },
  { id: 'opencode', label: 'OpenCode' },
];

const FIRST_VISIBLE = 5;
const VISIBLE_STEP = 8;

export function ClaudeChatsView({
  tone,
  sessions,
  loading,
  loadingMore,
  exhausted,
  importing,
  error,
  relativeTime,
  onBack,
  onClose,
  onOpen,
  onLoadMore,
}: {
  tone: IslandTone;
  sessions: claudeimport.ExternalSession[];
  loading?: boolean;
  loadingMore?: boolean;
  exhausted?: Record<ChatSourceId, boolean>;
  onLoadMore?: (providers: ChatSourceId[]) => void;
  importing?: boolean;
  error?: string | null;
  relativeTime: (at: number) => string;
  onBack: () => void;
  onClose: () => void;
  onOpen: (session: claudeimport.ExternalSession) => void;
}) {
  const [query, setQuery] = useState('');
  const [source, setSource] = useState<ChatSourceId | null>(null);
  const [shown, setShown] = useState(FIRST_VISIBLE);

  const matching = useMemo(() => {
    const q = query.trim().toLowerCase();
    return sessions.filter((s) => {
      if (source && (s.provider || 'claude') !== source) return false;
      if (!q) return true;
      return (s.title || '').toLowerCase().includes(q) || (s.cwd || '').toLowerCase().includes(q);
    });
  }, [sessions, query, source]);

  const rows = matching.slice(0, shown);
  const targets: ChatSourceId[] = source ? [source] : CHAT_SOURCES.map((entry) => entry.id);
  const fetchable = targets.some((id) => !exhausted?.[id]);
  const hasMore = matching.length > shown || fetchable;

  const revealMore = () => {
    setShown((current) => current + VISIBLE_STEP);
    if (matching.length - shown < VISIBLE_STEP * 2) onLoadMore?.(targets);
  };

  return (
    <div className="flex flex-col p-2.5 pt-2">
      <IslandHeader
        tone={tone}
        title="CLI chats"
        onBack={onBack}
        backLabel="Projects"
        actions={
          <IconButton tone={tone} title="Close" onClick={onClose}>
            <X size={14} strokeWidth={2} />
          </IconButton>
        }
      />
      <div className="flex items-center gap-1.5 mb-1.5">
        <label
          className={`flex flex-1 min-w-0 items-center gap-2 h-[32px] px-2.5 rounded-[11px] ${
            tone.isLight ? 'bg-black/[0.045]' : 'bg-white/[0.06]'
          }`}
        >
          <Search size={13} strokeWidth={2} className={tone.sub} />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search chats"
            className={`flex-1 min-w-0 bg-transparent outline-none border-0 text-[12px] ${tone.fg} ${
              tone.isLight ? 'placeholder:text-black/35' : 'placeholder:text-white/35'
            }`}
          />
        </label>
        <div
          className={`flex items-center gap-0.5 h-[32px] p-[3px] rounded-[11px] shrink-0 ${
            tone.isLight ? 'bg-black/[0.045]' : 'bg-white/[0.06]'
          }`}
        >
          {CHAT_SOURCES.map((entry) => {
            const active = source === entry.id;
            return (
              <button
                key={entry.id}
                type="button"
                title={active ? `Showing ${entry.label} only` : entry.label}
                aria-pressed={active}
                onClick={() => {
                  setSource(active ? null : entry.id);
                  setShown(FIRST_VISIBLE);
                }}
                className={`w-[30px] h-[26px] rounded-[8px] flex items-center justify-center transition-all duration-150 cursor-pointer ${
                  active
                    ? tone.isLight
                      ? 'bg-white shadow-sm'
                      : 'bg-white/[0.16]'
                    : `${tone.hover} ${source ? 'opacity-45' : ''}`
                }`}
              >
                <img
                  src={providerIcon(entry.id, tone.isLight)}
                  alt=""
                  draggable={false}
                  className="w-[14px] h-[14px] object-contain"
                />
              </button>
            );
          })}
        </div>
      </div>
      {error ? <div className="mx-1 mb-1.5 px-3 py-2 rounded-[10px] bg-rose-500/10 text-[11.5px] text-rose-300">{error}</div> : null}
      <ScrollArea maxHeight={300} className="-mr-1 pr-1">
        {loading && sessions.length === 0 ? (
          <EmptyState tone={tone}>Reading chats…</EmptyState>
        ) : rows.length === 0 && !hasMore ? (
          <EmptyState tone={tone}>{query ? 'No matching chats' : 'No chats yet'}</EmptyState>
        ) : (
          <div className="flex flex-col">
            {rows.map((s) => {
              const folder = (s.cwd || '').replace(/\\/g, '/').split('/').filter(Boolean).pop();
              const provider = (s.provider || 'claude') as ChatSourceId;
              const sourceLabel = ENTRYPOINT_LABEL[s.entrypoint || ''];
              return (
                <button
                  key={s.filePath}
                  type="button"
                  disabled={importing}
                  title={provider === 'antigravity' ? "Opening Antigravity chats isn't supported yet" : undefined}
                  onClick={() => onOpen(s)}
                  className={`w-full flex items-center gap-2.5 h-[42px] px-2 rounded-[12px] text-left transition-colors duration-150 cursor-pointer disabled:opacity-60 disabled:cursor-default ${tone.hover}`}
                >
                  <img
                    src={providerIcon(provider, tone.isLight)}
                    alt=""
                    draggable={false}
                    className="w-[14px] h-[14px] object-contain shrink-0 opacity-80"
                  />
                  <span className="flex flex-col min-w-0 flex-1 gap-[1px]">
                    <span className={`text-[12.5px] font-medium tracking-tight truncate leading-[15px] ${tone.fg}`}>
                      {s.title || 'Untitled chat'}
                    </span>
                    <span className={`text-[10.5px] truncate leading-[13px] ${tone.sub}`}>
                      {[relativeTime(s.updatedAt), folder, sourceLabel].filter(Boolean).join(' · ')}
                    </span>
                  </span>
                  <ChevronRight size={14} strokeWidth={2} className={`${tone.faint} shrink-0`} />
                </button>
              );
            })}
          </div>
        )}
        {hasMore && !loading ? (
          <>
            <LoadMoreSentinel busy={Boolean(loadingMore)} onVisible={revealMore} />
            {loadingMore ? <EmptyState tone={tone}>Loading more…</EmptyState> : null}
          </>
        ) : null}
      </ScrollArea>
    </div>
  );
}
