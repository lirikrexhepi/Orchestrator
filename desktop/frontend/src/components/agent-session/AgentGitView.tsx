import React, { useEffect, useMemo, useState } from 'react';
import { domain, files } from '../../../wailsjs/go/models';
import { DiffView } from '../git/DiffView';
import { GitMenu } from '../git/GitMenu';
import { GitState } from '../git/useGit';
import { useGitActions } from '../git/useGitActions';
import { FilePreview } from '../files/FilePreview';
import { FileTree } from '../files/FileTree';
import { repoPath } from '../files/status';
import { useProjectTree } from '../files/useProjectTree';
import { fileIconForPath } from '../common/fileIcon';
import { changeDiffFile, changeDiffText, ThreadFileChange } from './threadChanges';
import { BreakdownHover } from './ChangeBreakdown';
import {
  ArrowDown,
  ArrowUp,
  Check,
  ChevronDown,
  Copy,
  FolderGit2,
  FolderOpen,
  GitBranch,
  Loader2,
  Plus,
  RefreshCw,
  Search,
} from '../common/icons';
import { ClipboardSetText } from '../../../wailsjs/runtime/runtime';

type LeftTab = 'thread' | 'worktree' | 'commits' | 'files';

export interface AgentGitViewProps {
  threadId?: string;
  branch?: string;
  git?: GitState;
  threadChanges?: ThreadFileChange[];
  className?: string;
}

const STATUS_MARKS: Record<string, { glyph: string; tone: string; label: string; size: string }> = {
  added: { glyph: '+', tone: 'text-emerald-300', label: 'Added', size: 'text-[15px]' },
  untracked: { glyph: '+', tone: 'text-emerald-300', label: 'New', size: 'text-[15px]' },
  copied: { glyph: '+', tone: 'text-sky-300', label: 'Copied', size: 'text-[15px]' },
  modified: { glyph: '●', tone: 'text-amber-300/90', label: 'Modified', size: 'text-[8px]' },
  deleted: { glyph: '−', tone: 'text-rose-300', label: 'Deleted', size: 'text-[15px]' },
  renamed: { glyph: '→', tone: 'text-sky-300', label: 'Renamed', size: 'text-[13px]' },
  conflicted: { glyph: '!', tone: 'text-rose-400', label: 'Conflicted', size: 'text-[13px]' },
};

const FONT = 'font-(family-name:--app-font)';

function fileName(path: string): string {
  const parts = path.split('/');
  return parts[parts.length - 1] ?? path;
}

function directory(path: string): string {
  const cut = path.lastIndexOf('/');
  return cut > 0 ? path.slice(0, cut) : '';
}

function whenAgo(at: number): string {
  if (!at) return '';
  const minutes = Math.floor((Date.now() - at) / 60_000);
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? `${hours}h` : `${Math.floor(hours / 24)}d`;
}

const Pill: React.FC<{
  icon: React.ReactNode;
  label: string;
  caption?: string;
  open?: boolean;
  onClick: () => void;
  title?: string;
}> = ({ icon, label, caption, open, onClick, title }) => (
  <button
    type="button"
    title={title}
    onClick={onClick}
    className={`h-[34px] pl-2.5 pr-2 rounded-[10px] flex items-center gap-2 min-w-0 max-w-[230px] transition-all duration-150 cursor-pointer active:scale-[0.98] ${
      open ? 'bg-white/[0.12]' : 'bg-white/[0.05] hover:bg-white/[0.09]'
    }`}
  >
    <span className="text-white/55 shrink-0 flex">{icon}</span>
    <span className="flex flex-col items-start min-w-0 leading-none">
      {caption && <span className={`text-[9px] uppercase tracking-wider text-white/35 ${FONT}`}>{caption}</span>}
      <span className={`text-[12px] font-medium text-white/90 tracking-tight truncate max-w-full ${FONT} ${caption ? 'mt-0.5' : ''}`}>
        {label}
      </span>
    </span>
    <ChevronDown size={13} className={`text-white/40 shrink-0 transition-transform duration-150 ${open ? 'rotate-180' : ''}`} />
  </button>
);

const MenuRow: React.FC<{
  active?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}> = ({ active, onClick, children }) => (
  <button
    type="button"
    onClick={onClick}
    className={`w-full text-left px-2.5 py-1.5 rounded-[8px] flex items-center gap-2 transition-colors cursor-pointer ${
      active ? 'bg-white/[0.12] text-white' : 'text-white/75 hover:bg-white/[0.07] hover:text-white'
    }`}
  >
    {children}
  </button>
);

const Checkbox: React.FC<{ checked: boolean; onChange: () => void; disabled?: boolean }> = ({
  checked,
  onChange,
  disabled,
}) => (
  <span
    role="checkbox"
    aria-checked={checked}
    onClick={(event) => {
      event.stopPropagation();
      if (!disabled) onChange();
    }}
    className={`w-[15px] h-[15px] rounded-[5px] border flex items-center justify-center shrink-0 transition-all duration-150 cursor-pointer ${
      checked ? 'bg-[#3b82f6] border-[#3b82f6]' : 'border-white/25 hover:border-white/50 bg-transparent'
    } ${disabled ? 'opacity-50' : ''}`}
  >
    {checked && <Check size={11} strokeWidth={3} className="text-white" />}
  </span>
);

interface FileRowProps {
  file: domain.FileChange;
  selected: boolean;
  onSelect: () => void;
  onToggle: () => void;
  busy: boolean;
}

const FileRow = React.memo<FileRowProps>(({ file, selected, onSelect, onToggle, busy }) => {
  const badge = STATUS_MARKS[file.status] ?? STATUS_MARKS.modified;
  const dir = directory(file.path);
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={(event) => {
        if (event.key === 'Enter') onSelect();
      }}
      className={`w-full text-left px-2 py-1.5 rounded-[8px] flex items-center gap-2 transition-colors cursor-pointer ${
        selected ? 'bg-white/[0.14] text-white' : 'text-white/70 hover:bg-white/[0.06] hover:text-white'
      }`}
    >
      <Checkbox checked={file.staged} onChange={onToggle} disabled={busy} />
      <div className="flex-1 min-w-0">
        <div className={`text-[12px] font-medium truncate ${FONT}`}>{fileName(file.path)}</div>
        {dir && <div className="text-[10px] text-white/35 font-mono truncate">{dir}</div>}
      </div>
      <span
        title={badge.label}
        className={`w-4 h-4 flex items-center justify-center font-semibold leading-none shrink-0 ${badge.size} ${badge.tone}`}
      >
        {badge.glyph}
      </span>
    </div>
  );
});
FileRow.displayName = 'GitFileRow';

function splitPath(path: string): { dir: string; base: string } {
  const parts = path.split(/[\\/]/).filter(Boolean);
  const base = parts.pop() || path;
  return { dir: parts.length ? `${parts.slice(-2).join('/')}/` : '', base };
}

const ThreadRow = React.memo<{
  change: ThreadFileChange;
  selected: boolean;
  onSelect: () => void;
}>(({ change, selected, onSelect }) => {
  const { dir, base } = splitPath(change.path);
  return (
    <button
      type="button"
      onClick={onSelect}
      title={change.path}
      className={`w-full text-left px-2 py-1.5 rounded-[8px] flex items-center gap-2 transition-colors cursor-pointer ${
        selected ? 'bg-white/[0.14] text-white' : 'text-white/70 hover:bg-white/[0.06] hover:text-white'
      }`}
    >
      <img src={fileIconForPath(change.path)} alt="" draggable={false} className="w-[14px] h-[14px] shrink-0" />
      <div className="flex-1 min-w-0">
        <div className="text-[12px] font-medium truncate">
          <span className="text-white/35">{dir}</span>
          {base}
        </div>
        {change.edits > 1 && <div className="text-[10px] text-white/30 font-mono truncate">{change.edits} edits</div>}
      </div>
      <span className="shrink-0 flex items-center gap-1.5 text-[10.5px] tabular-nums">
        {change.additions > 0 && <span className="text-emerald-300">+{change.additions}</span>}
        {change.deletions > 0 && <span className="text-rose-300">−{change.deletions}</span>}
      </span>
    </button>
  );
});
ThreadRow.displayName = 'ThreadChangeRow';

const SectionHeader: React.FC<{
  label: string;
  count: number;
  action: string;
  onAction: () => void;
  busy: boolean;
}> = ({ label, count, action, onAction, busy }) => (
  <div className="flex items-center justify-between px-2 pt-2 pb-1">
    <span className={`text-[10px] font-semibold uppercase tracking-wider text-white/35 ${FONT}`}>
      {label} · {count}
    </span>
    <button
      type="button"
      disabled={busy}
      onClick={onAction}
      className={`text-[10.5px] font-medium text-white/45 hover:text-white transition-colors cursor-pointer disabled:opacity-40 ${FONT}`}
    >
      {action}
    </button>
  </div>
);

export const AgentGitView: React.FC<AgentGitViewProps> = ({ threadId, branch, git, threadChanges = [], className = '' }) => {
  const [copied, setCopied] = useState(false);
  const [leftTab, setLeftTab] = useState<LeftTab>('thread');
  const [threadKey, setThreadKey] = useState<string | null>(null);
  const [treeFile, setTreeFile] = useState<string | null>(null);
  const [treeMode, setTreeMode] = useState<'diff' | 'file'>('diff');
  const [summary, setSummary] = useState('');
  const [description, setDescription] = useState('');
  const [branchQuery, setBranchQuery] = useState('');
  const [showDescription, setShowDescription] = useState(false);
  const [pendingBranch, setPendingBranch] = useState<string | null>(null);

  const selectedChange = useMemo(
    () => threadChanges.find((change) => change.key === threadKey) ?? threadChanges[0] ?? null,
    [threadChanges, threadKey],
  );
  const selectedDiffFile = useMemo(
    () => (selectedChange ? changeDiffFile(selectedChange) : null),
    [selectedChange],
  );

  const matchingLane =
    git?.lanes.find(
      (l) =>
        (threadId && l.threadId === threadId) ||
        (branch && l.branch === branch) ||
        (l.title && threadId && l.title.includes(threadId)),
    ) ||
    git?.lanes.find((l) => l.isMain) ||
    git?.activeLane ||
    git?.lanes[0] ||
    null;

  const [pickedLanePath, setPickedLanePath] = useState<string | null>(null);
  const lane = (pickedLanePath && git?.lanes.find((l) => l.path === pickedLanePath)) || matchingLane;

  useEffect(() => {
    if (git && lane && git.activeLanePath !== lane.path) {
      git.selectLane(lane.path);
    }
  }, [lane?.path, git?.activeLanePath, git?.selectLane]);

  const actions = useGitActions(lane?.path ?? null, git?.refresh ?? (async () => undefined));

  const treeRoot = lane?.path ?? null;
  const tree = useProjectTree(treeRoot, leftTab === 'files');
  useEffect(() => setTreeFile(null), [treeRoot]);

  const changedFiles = lane?.files ?? [];
  const commits = lane?.commits ?? [];
  const staged = useMemo(() => changedFiles.filter((f) => f.staged), [changedFiles]);
  const unstaged = useMemo(() => changedFiles.filter((f) => !f.staged), [changedFiles]);

  if (!git) {
    return (
      <div className="flex-1 min-h-0 flex items-center justify-center text-white/40 text-[12px]">
        Loading git status...
      </div>
    );
  }

  const busy = actions.busy !== null;
  const treeStatus = treeFile ? tree.status?.files?.[treeFile] : undefined;
  const treeHasDiff = !!treeStatus && treeStatus !== 'deleted';

  const openTreeFile = (entry: files.Entry) => {
    setTreeFile(entry.path);
    const status = tree.status?.files?.[entry.path];
    if (status && status !== 'deleted') {
      setTreeMode('diff');
      git.selectFile({
        path: repoPath(tree.status?.prefix, entry.path),
        staged: !!tree.status?.staged?.[entry.path],
      });
    } else {
      setTreeMode('file');
    }
  };

  const tabClass = (on: boolean) =>
    `flex-1 py-1 rounded-[7px] text-[11px] font-medium ${FONT} text-center transition-all cursor-pointer ${
      on ? 'bg-white/15 text-white shadow-sm font-semibold' : 'text-white/45 hover:text-white/80'
    }`;

  const copyDiff = async () => {
    const text = git.diffs
      .map((diff) =>
        (diff.hunks ?? [])
          .map(
            (hunk) =>
              `${hunk.header}\n` +
              (hunk.lines ?? [])
                .map((line) => `${line.kind === 'added' ? '+' : line.kind === 'removed' ? '-' : ' '}${line.content}`)
                .join('\n'),
          )
          .join('\n'),
      )
      .join('\n\n');
    if (!text) return;
    await ClipboardSetText(text);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  };

  const copyThreadDiff = async () => {
    if (!selectedChange) return;
    const text = changeDiffText(selectedChange);
    if (!text) return;
    await ClipboardSetText(text);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  };

  const submitCommit = async () => {
    const trimmed = summary.trim();
    if (!trimmed) return;
    const stageFirst = staged.length === 0 ? Array.from(new Set(unstaged.map((f) => f.path))) : [];
    const ok = await actions.commit(trimmed, description, stageFirst);
    if (ok) {
      setSummary('');
      setDescription('');
    }
  };

  const syncState = (() => {
    if (!lane) return { label: 'Fetch origin', caption: 'Up to date', icon: <RefreshCw size={14} />, run: actions.fetchOrigin, kind: 'fetch' as const };
    if (!lane.upstream)
      return { label: 'Publish branch', caption: 'Not on origin', icon: <ArrowUp size={14} />, run: actions.push, kind: 'push' as const };
    if (lane.unpulled > 0)
      return { label: 'Pull origin', caption: `${lane.unpulled} to pull`, icon: <ArrowDown size={14} />, run: actions.pull, kind: 'pull' as const };
    if (lane.unpushed > 0)
      return { label: 'Push origin', caption: `${lane.unpushed} to push`, icon: <ArrowUp size={14} />, run: actions.push, kind: 'push' as const };
    return { label: 'Fetch origin', caption: 'Up to date', icon: <RefreshCw size={14} />, run: actions.fetchOrigin, kind: 'fetch' as const };
  })();

  const syncBusy = actions.busy === syncState.kind;
  const filteredBranches = actions.branches.filter((b) =>
    b.name.toLowerCase().includes(branchQuery.trim().toLowerCase()),
  );
  const exactBranch = actions.branches.some((b) => b.name === branchQuery.trim());
  const commitDisabled = busy || !summary.trim() || changedFiles.length === 0;

  return (
    <div className={`flex flex-col h-full w-full min-h-0 select-none ${className}`}>
      <div className="flex items-center gap-2 shrink-0 pb-2.5 px-0.5">
        <GitMenu
          width={280}
          trigger={(open, toggle) => (
            <Pill
              icon={<FolderGit2 size={15} />}
              caption="Worktree"
              label={lane?.title ?? 'Project'}
              open={open}
              onClick={toggle}
              title={lane?.path}
            />
          )}
        >
          {(close) => (
            <div className="p-1 max-h-[300px] overflow-y-auto custom-scrollbar">
              {lane && (
                <MenuRow
                  onClick={() => {
                    void git.reveal(lane.path);
                    close();
                  }}
                >
                  <FolderOpen size={14} className="text-white/45 shrink-0" />
                  <span className={`text-[12px] ${FONT}`}>Reveal in File Explorer</span>
                </MenuRow>
              )}
              <div className="h-px bg-white/[0.07] my-1 mx-1" />
              {git.lanes.map((l) => (
                <MenuRow
                  key={l.path}
                  active={l.path === lane?.path}
                  onClick={() => {
                    setPickedLanePath(l.path);
                    close();
                  }}
                >
                  <FolderGit2 size={14} className="text-white/45 shrink-0" />
                  <span className="flex flex-col min-w-0 flex-1">
                    <span className={`text-[12px] font-medium truncate ${FONT}`}>{l.title}</span>
                    <span className="text-[10px] text-white/35 font-mono truncate">{l.branch}</span>
                  </span>
                  {(l.files?.length ?? 0) > 0 && (
                    <span className="text-[10px] tabular-nums text-white/45 shrink-0">{l.files.length}</span>
                  )}
                  {l.path === lane?.path && <Check size={13} className="text-white/70 shrink-0" />}
                </MenuRow>
              ))}
            </div>
          )}
        </GitMenu>

        <GitMenu
          width={290}
          onOpen={() => {
            setBranchQuery('');
            setPendingBranch(null);
            void actions.loadBranches();
          }}
          trigger={(open, toggle) => (
            <Pill
              icon={<GitBranch size={15} />}
              caption="Branch"
              label={lane?.branch || branch || 'main'}
              open={open}
              onClick={toggle}
            />
          )}
        >
          {(close) => pendingBranch ? (
            <div className="p-3 flex flex-col gap-2.5">
              <p className={`text-[12px] leading-relaxed text-white/80 ${FONT}`}>
                You have {changedFiles.length} uncommitted change{changedFiles.length === 1 ? '' : 's'} on{' '}
                <span className="font-semibold text-white">{lane?.branch}</span>. What should happen to {changedFiles.length === 1 ? 'it' : 'them'} when switching to{' '}
                <span className="font-semibold text-white">{pendingBranch}</span>?
              </p>
              <button
                type="button"
                onClick={() => {
                  void actions.checkout(pendingBranch, 'leave');
                  setPendingBranch(null);
                  close();
                }}
                className={`h-[30px] rounded-[8px] bg-white/[0.08] hover:bg-white/[0.14] text-[12px] font-medium text-white transition-colors cursor-pointer ${FONT}`}
              >
                Leave them on {lane?.branch}
              </button>
              <button
                type="button"
                onClick={() => {
                  void actions.checkout(pendingBranch, 'bring');
                  setPendingBranch(null);
                  close();
                }}
                className={`h-[30px] rounded-[8px] bg-[#3b82f6] hover:bg-[#4f8ff7] text-[12px] font-semibold text-white transition-colors cursor-pointer ${FONT}`}
              >
                Bring them to {pendingBranch}
              </button>
              <button
                type="button"
                onClick={() => setPendingBranch(null)}
                className={`h-[26px] text-[11.5px] text-white/50 hover:text-white transition-colors cursor-pointer ${FONT}`}
              >
                Cancel
              </button>
            </div>
          ) : (
            <div className="flex flex-col">
              <div className="flex items-center gap-2 px-3 py-2 border-b border-white/[0.07]">
                <Search size={13} className="text-white/35 shrink-0" />
                <input
                  autoFocus
                  value={branchQuery}
                  onChange={(event) => setBranchQuery(event.target.value)}
                  placeholder="Filter or create a branch"
                  className={`flex-1 min-w-0 bg-transparent outline-none text-[12px] text-white placeholder:text-white/30 ${FONT}`}
                />
              </div>
              <div className="p-1 max-h-[280px] overflow-y-auto custom-scrollbar">
                {branchQuery.trim() && !exactBranch && (
                  <MenuRow
                    onClick={() => {
                      void actions.createBranch(branchQuery.trim());
                      close();
                    }}
                  >
                    <Plus size={14} className="text-emerald-300 shrink-0" />
                    <span className={`text-[12px] truncate ${FONT}`}>
                      Create <span className="font-semibold text-white">{branchQuery.trim()}</span>
                    </span>
                  </MenuRow>
                )}
                {filteredBranches.map((b) => (
                  <MenuRow
                    key={`${b.remote ? 'r' : 'l'}-${b.name}`}
                    active={b.current}
                    onClick={() => {
                      if (b.current) {
                        close();
                        return;
                      }
                      const target = b.remote ? b.name.slice(b.name.indexOf('/') + 1) : b.name;
                      if (changedFiles.length > 0) {
                        setPendingBranch(target);
                        return;
                      }
                      void actions.checkout(target);
                      close();
                    }}
                  >
                    <GitBranch size={13} className="text-white/40 shrink-0" />
                    <span className={`text-[12px] truncate flex-1 ${FONT}`}>{b.name}</span>
                    {b.remote && <span className="text-[9px] uppercase tracking-wide text-white/30 shrink-0">remote</span>}
                    {b.current && <Check size={13} className="text-white/70 shrink-0" />}
                  </MenuRow>
                ))}
                {filteredBranches.length === 0 && !branchQuery.trim() && (
                  <p className={`px-3 py-3 text-[11.5px] text-white/35 ${FONT}`}>No branches found.</p>
                )}
              </div>
            </div>
          )}
        </GitMenu>

        <div className="flex-1 min-w-0 flex items-center justify-end gap-1.5">
          {actions.notice && (
            <span
              title={actions.notice.text}
              className={`text-[11px] font-medium truncate max-w-[260px] ${FONT} ${
                actions.notice.tone === 'error' ? 'text-rose-300' : 'text-emerald-300/90'
              }`}
            >
              {actions.notice.text}
            </span>
          )}

          <button
            type="button"
            disabled={busy}
            onClick={() => void syncState.run()}
            className="h-[34px] px-3 rounded-[10px] bg-white/[0.08] hover:bg-white/[0.14] active:scale-[0.98] disabled:opacity-60 flex items-center gap-2 transition-all duration-150 cursor-pointer"
          >
            <span className={`text-white/80 flex ${syncBusy ? 'animate-spin' : ''}`}>
              {syncBusy ? <Loader2 size={14} /> : syncState.icon}
            </span>
            <span className="flex flex-col items-start leading-none">
              <span className={`text-[12px] font-medium text-white/90 tracking-tight ${FONT}`}>{syncState.label}</span>
              <span className={`text-[9.5px] text-white/40 mt-0.5 ${FONT}`}>{syncState.caption}</span>
            </span>
          </button>

          <button
            type="button"
            title="Refresh"
            onClick={() => {
              void git.refresh();
              if (leftTab === 'files') void tree.refresh();
            }}
            className="w-[34px] h-[34px] rounded-[10px] flex items-center justify-center text-white/55 hover:text-white bg-white/[0.05] hover:bg-white/[0.1] active:scale-95 transition-all cursor-pointer"
          >
            <RefreshCw size={14} className={git.isLoading ? 'animate-spin' : ''} />
          </button>
        </div>
      </div>

      <div className="flex-1 min-h-0 flex gap-2.5">
        <div className="w-[290px] shrink-0 flex flex-col min-h-0 rounded-xl bg-black/40 overflow-hidden">
          <div className="flex items-center p-1 bg-white/[0.02] shrink-0">
            <button
              type="button"
              onClick={() => setLeftTab('thread')}
              className={tabClass(leftTab === 'thread')}
            >
              Thread {threadChanges.length > 0 && `(${threadChanges.length})`}
            </button>
            <button
              type="button"
              onClick={() => {
                setLeftTab('worktree');
                git.selectView({ kind: 'changes' });
              }}
              className={tabClass(leftTab === 'worktree')}
            >
              Worktree {changedFiles.length > 0 && `(${changedFiles.length})`}
            </button>
            <button
              type="button"
              onClick={() => {
                setLeftTab('commits');
                if (commits[0]) git.selectView({ kind: 'commit', sha: commits[0].sha });
              }}
              className={tabClass(leftTab === 'commits')}
            >
              History
            </button>
            <button type="button" onClick={() => setLeftTab('files')} className={tabClass(leftTab === 'files')}>
              Files
            </button>
          </div>

          <div className="flex-1 min-h-0 overflow-y-auto custom-scrollbar p-1">
            {leftTab === 'files' ? (
              <FileTree tree={tree} selected={treeFile} onSelect={openTreeFile} />
            ) : leftTab === 'thread' ? (
              threadChanges.length === 0 ? (
                <div className="flex flex-col items-center justify-center text-center p-5 h-full">
                  <div className="w-9 h-9 rounded-xl bg-white/[0.05] flex items-center justify-center mb-2">
                    <Check size={16} className="text-white/35" />
                  </div>
                  <span className={`text-[12px] font-medium text-white/55 mb-0.5 ${FONT}`}>No edits in this chat</span>
                  <span className={`text-[10.5px] text-white/35 max-w-[190px] ${FONT}`}>
                    Every file this chat writes or edits shows up here.
                  </span>
                </div>
              ) : (
                threadChanges.map((change) => (
                  <ThreadRow
                    key={change.key}
                    change={change}
                    selected={selectedChange?.key === change.key}
                    onSelect={() => setThreadKey(change.key)}
                  />
                ))
              )
            ) : leftTab === 'worktree' ? (
              changedFiles.length === 0 ? (
                <div className="flex flex-col items-center justify-center text-center p-5 h-full">
                  <div className="w-9 h-9 rounded-xl bg-white/[0.05] flex items-center justify-center mb-2">
                    <Check size={16} className="text-white/35" />
                  </div>
                  <span className={`text-[12px] font-medium text-white/55 mb-0.5 ${FONT}`}>No local changes</span>
                  <span className={`text-[10.5px] text-white/35 max-w-[180px] ${FONT}`}>
                    {lane?.unpushed ? `${lane.unpushed} commit${lane.unpushed === 1 ? '' : 's'} ready to push.` : 'Everything is committed.'}
                  </span>
                </div>
              ) : (
                <>
                  {staged.length > 0 && (
                    <>
                      <SectionHeader
                        label="Staged"
                        count={staged.length}
                        action="Unstage all"
                        busy={busy}
                        onAction={() => void actions.unstage(Array.from(new Set(staged.map((f) => f.path))))}
                      />
                      {staged.map((file) => (
                        <FileRow
                          key={`s-${file.path}`}
                          file={file}
                          busy={busy}
                          selected={git.selectedFile?.path === file.path && git.selectedFile?.staged === true}
                          onSelect={() => git.selectFile({ path: file.path, staged: true })}
                          onToggle={() => void actions.unstage([file.path])}
                        />
                      ))}
                    </>
                  )}
                  {unstaged.length > 0 && (
                    <>
                      <SectionHeader
                        label="Changes"
                        count={unstaged.length}
                        action="Stage all"
                        busy={busy}
                        onAction={() => void actions.stage(Array.from(new Set(unstaged.map((f) => f.path))))}
                      />
                      {unstaged.map((file) => (
                        <FileRow
                          key={`w-${file.path}`}
                          file={file}
                          busy={busy}
                          selected={git.selectedFile?.path === file.path && git.selectedFile?.staged === false}
                          onSelect={() => git.selectFile({ path: file.path, staged: false })}
                          onToggle={() => void actions.stage([file.path])}
                        />
                      ))}
                    </>
                  )}
                </>
              )
            ) : (
              commits.map((c) => {
                const isSelected = git.view.kind === 'commit' && git.view.sha === c.sha;
                return (
                  <button
                    key={c.sha}
                    type="button"
                    onClick={() => git.selectView({ kind: 'commit', sha: c.sha })}
                    className={`w-full text-left px-2 py-1.5 rounded-[8px] flex flex-col gap-0.5 transition-colors cursor-pointer mb-0.5 ${
                      isSelected ? 'bg-white/[0.14] text-white' : 'text-white/70 hover:bg-white/[0.06] hover:text-white'
                    }`}
                  >
                    <div className={`text-[12px] font-medium truncate ${FONT} ${c.onBase ? 'text-white/50' : ''}`}>
                      {c.subject}
                    </div>
                    <div className="flex items-center justify-between gap-1 text-[10px] font-mono text-white/35">
                      <span className="truncate">
                        {c.short} · {c.author}
                      </span>
                      <span className="shrink-0">{whenAgo(c.at)}</span>
                    </div>
                  </button>
                );
              })
            )}
          </div>

          {leftTab === 'worktree' && (
            <div className="shrink-0 p-2 flex flex-col gap-1.5 border-t border-white/[0.07]">
              <input
                value={summary}
                onChange={(event) => setSummary(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && !commitDisabled) void submitCommit();
                }}
                placeholder="Commit message"
                className={`h-[30px] px-2.5 rounded-[8px] bg-white/[0.06] focus:bg-white/[0.09] outline-none text-[12px] text-white placeholder:text-white/30 transition-colors select-text ${FONT}`}
              />
              {showDescription && (
                <textarea
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                  placeholder="Description"
                  rows={2}
                  className={`px-2.5 py-1.5 rounded-[8px] bg-white/[0.06] focus:bg-white/[0.09] outline-none text-[12px] text-white placeholder:text-white/30 resize-none transition-colors select-text custom-scrollbar ${FONT}`}
                />
              )}
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  title={showDescription ? 'Hide description' : 'Add description'}
                  onClick={() => setShowDescription((value) => !value)}
                  className={`h-[28px] w-[28px] rounded-[8px] flex items-center justify-center shrink-0 transition-colors cursor-pointer ${
                    showDescription ? 'bg-white/[0.12] text-white' : 'bg-white/[0.05] text-white/50 hover:text-white hover:bg-white/[0.1]'
                  }`}
                >
                  <Plus size={14} />
                </button>
                <button
                  type="button"
                  disabled={commitDisabled}
                  onClick={() => void submitCommit()}
                  className={`flex-1 min-w-0 h-[28px] rounded-[8px] text-[12px] font-semibold text-white bg-[#3b82f6] hover:bg-[#4f8ff7] active:scale-[0.98] disabled:bg-white/[0.07] disabled:text-white/30 flex items-center justify-center gap-1.5 transition-all duration-150 cursor-pointer disabled:cursor-default ${FONT}`}
                >
                  {actions.busy === 'commit' && <Loader2 size={13} className="animate-spin" />}
                  <span className="truncate px-2">
                    {staged.length > 0
                      ? `Commit ${staged.length} to ${lane?.branch ?? 'branch'}`
                      : `Commit all to ${lane?.branch ?? 'branch'}`}
                  </span>
                </button>
              </div>
            </div>
          )}
        </div>

        <div className="flex-1 min-h-0 rounded-xl bg-[#0d0f12] overflow-hidden flex flex-col">
          {leftTab === 'thread' ? (
            !selectedChange ? (
              <div className="flex-1 min-h-0 grid place-items-center p-6">
                <p className={`text-[12px] text-white/35 text-center ${FONT} max-w-[260px] leading-relaxed`}>
                  No code changes in this chat yet. What the agent writes and edits shows up here, along the whole thread.
                </p>
              </div>
            ) : (
              <>
                <div className="flex items-center gap-2 px-3 h-[32px] border-b border-white/[0.07] shrink-0">
                  <span className={`text-[11.5px] font-medium text-white/80 tracking-tight truncate flex-1 min-w-0 ${FONT}`}>
                    {selectedChange.path}
                  </span>
                  <span className={`text-[10.5px] text-white/35 tabular-nums shrink-0 ${FONT}`}>
                    {selectedChange.edits} edit{selectedChange.edits === 1 ? '' : 's'}
                  </span>
                  <span className={`text-[10.5px] tabular-nums shrink-0 ${FONT}`}>
                    <span className="text-emerald-300/80">+{selectedChange.additions}</span>{' '}
                    <span className="text-rose-300/80">−{selectedChange.deletions}</span>
                  </span>
                  {selectedDiffFile && (
                    <button
                      type="button"
                      title="Copy diff"
                      onClick={() => void copyThreadDiff()}
                      className="w-[24px] h-[24px] rounded-[7px] flex items-center justify-center text-white/45 hover:text-white hover:bg-white/10 active:scale-90 transition-all cursor-pointer shrink-0"
                    >
                      {copied ? <Check size={13} className="text-emerald-400" /> : <Copy size={13} />}
                    </button>
                  )}
                </div>
                {selectedDiffFile ? (
                  <DiffView
                    diffs={[selectedDiffFile]}
                    isLoading={false}
                    error={null}
                    placeholder="No diff to display."
                  />
                ) : (
                  <div className="flex-1 min-h-0 grid place-items-center p-6">
                    <p className={`text-[12px] text-white/40 text-center ${FONT} max-w-[240px] leading-relaxed`}>
                      {selectedChange.groups.some((group) => group.status === 'running')
                        ? 'The agent is editing this file — the diff appears once the edit lands.'
                        : 'No diff captured for this edit (it may have come from a subagent).'}
                    </p>
                  </div>
                )}
              </>
            )
          ) : leftTab === 'files' ? (
            !treeFile || !treeRoot ? (
              <div className="flex-1 min-h-0 grid place-items-center p-6">
                <p className={`text-[12px] text-white/35 text-center ${FONT}`}>
                  Select a file to open it. Changed files open on their diff.
                </p>
              </div>
            ) : (
              <>
                <div className="flex items-center gap-2 px-3 py-1.5 border-b border-white/[0.07] shrink-0">
                  <span className={`text-[11.5px] font-medium text-white/80 tracking-tight truncate flex-1 min-w-0 ${FONT}`}>
                    {treeFile}
                  </span>
                  {treeHasDiff && (
                    <div className="flex items-center gap-0.5 shrink-0">
                      {(['diff', 'file'] as const).map((mode) => (
                        <button
                          key={mode}
                          type="button"
                          onClick={() => setTreeMode(mode)}
                          className={`h-[20px] px-2 rounded-[5px] text-[10.5px] font-medium transition-all cursor-pointer ${FONT} ${
                            treeMode === mode ? 'bg-white/15 text-white' : 'text-white/45 hover:text-white/80'
                          }`}
                        >
                          {mode === 'diff' ? 'Diff' : 'File'}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
                {treeHasDiff && treeMode === 'diff' ? (
                  <DiffView diffs={git.diffs} isLoading={git.isDiffLoading} error={git.diffError} placeholder="Loading diff…" />
                ) : treeStatus === 'deleted' ? (
                  <div className="flex-1 min-h-0 grid place-items-center">
                    <p className={`text-[12px] text-white/40 ${FONT}`}>This file was deleted.</p>
                  </div>
                ) : (
                  <FilePreview root={treeRoot} path={treeFile} />
                )}
              </>
            )
          ) : (
            <>
              {git.diffs.length > 0 && (
                <div className="flex items-center gap-2 px-3 h-[32px] border-b border-white/[0.07] shrink-0">
                  <span className={`text-[11.5px] font-medium text-white/80 tracking-tight truncate flex-1 min-w-0 ${FONT}`}>
                    {git.diffs.length === 1 ? git.diffs[0].path : `${git.diffs.length} files`}
                  </span>
                  <BreakdownHover
                    files={git.diffs.map((d) => ({ path: d.path, additions: d.insertions, deletions: d.deletions }))}
                    className="shrink-0"
                  >
                    <span className={`text-[10.5px] tabular-nums shrink-0 ${FONT}`}>
                      <span className="text-emerald-300/80">+{git.diffs.reduce((n, d) => n + d.insertions, 0)}</span>{' '}
                      <span className="text-rose-300/80">−{git.diffs.reduce((n, d) => n + d.deletions, 0)}</span>
                    </span>
                  </BreakdownHover>
                  <button
                    type="button"
                    title="Copy diff"
                    onClick={() => void copyDiff()}
                    className="w-[24px] h-[24px] rounded-[7px] flex items-center justify-center text-white/45 hover:text-white hover:bg-white/10 active:scale-90 transition-all cursor-pointer shrink-0"
                  >
                    {copied ? <Check size={13} className="text-emerald-400" /> : <Copy size={13} />}
                  </button>
                </div>
              )}
              <DiffView
                diffs={git.diffs}
                isLoading={git.isDiffLoading}
                error={git.diffError}
                placeholder={changedFiles.length === 0 ? 'No modified files to display.' : 'Select a file to inspect its diff.'}
              />
            </>
          )}
        </div>
      </div>
    </div>
  );
};

export default AgentGitView;
