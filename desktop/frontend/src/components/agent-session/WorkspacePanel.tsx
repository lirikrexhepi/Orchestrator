import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { MaterialIcon } from '../common/icons';
import { ScrollArea } from '../common/ScrollArea';
import { MarkdownText } from './MarkdownText';
import { formatModelBadge, renderChatIcon } from '../common/DynamicIsland';
import { SubagentBlockData } from './types';
import { DARK_TONE, DOT_TONE, EntryRow, LIGHT_TONE, STATUS_WORD, Tone, elapsedOf, useNow } from './SubagentMenu';
import { AvailableEditors, GitPullRequestURL, GitPush, OpenInEditor } from '../../../wailsjs/go/main/App';
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';
import { domain } from '../../../wailsjs/go/models';
import { useTheme } from '../../themes';

const EASE = { duration: 0.22, ease: [0.32, 0.72, 0, 1] } as const;
const SLIDE = { type: 'spring', stiffness: 420, damping: 38, mass: 0.8 } as const;

const EDITOR_LABEL: Record<string, string> = {
  vscode: 'VS Code',
  cursor: 'Cursor',
  explorer: 'File Explorer',
};

let editorsCache: Promise<string[]> | null = null;

function loadEditors(): Promise<string[]> {
  editorsCache ??= AvailableEditors()
    .then((list) => [...(list ?? []), 'explorer'])
    .catch(() => ['explorer']);
  return editorsCache;
}

const number = new Intl.NumberFormat('en-US');

export function laneTotals(lane?: domain.WorktreeChanges | null): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const file of lane?.files ?? []) {
    added += file.insertions || 0;
    removed += file.deletions || 0;
  }
  return { added, removed };
}

const Row: React.FC<{
  tone: Tone;
  icon: React.ReactNode;
  label: React.ReactNode;
  trailing?: React.ReactNode;
  open?: boolean;
  onClick?: () => void;
  onToggle?: () => void;
  title?: string;
}> = ({ tone, icon, label, trailing, open, onClick, onToggle, title }) => (
  <div className={`group flex h-8 items-center gap-2.5 rounded-[10px] px-2 ${tone.hover}`} title={title}>
    <button
      type="button"
      onClick={onClick ?? onToggle}
      className="flex min-w-0 flex-1 items-center gap-2.5 text-left cursor-pointer"
    >
      <span className={`flex w-4 shrink-0 justify-center ${tone.muted}`}>{icon}</span>
      <span className={`min-w-0 flex-1 truncate whitespace-nowrap text-[12.5px] font-medium tracking-tight font-(family-name:--app-font) ${tone.title}`}>
        {label}
      </span>
      {trailing}
    </button>
    {onToggle && (
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className={`-mr-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-full cursor-pointer ${tone.close}`}
      >
        <MaterialIcon name="expand_more" size={15} className={`transition-transform duration-200 ${open ? 'rotate-180' : ''}`} />
      </button>
    )}
  </div>
);

const SubRow: React.FC<{ tone: Tone; label: string; onClick: () => void; busy?: boolean }> = ({ tone, label, onClick, busy }) => (
  <button
    type="button"
    onClick={onClick}
    disabled={busy}
    className={`flex h-7 w-full items-center gap-2 rounded-[8px] pl-[34px] pr-2 text-left text-[12px] whitespace-nowrap font-(family-name:--app-font) cursor-pointer disabled:opacity-50 ${tone.hover} ${tone.body}`}
  >
    <span className="min-w-0 flex-1 truncate">{label}</span>
    {busy && <MaterialIcon name="refresh" size={13} className="animate-spin" />}
  </button>
);

const Reveal: React.FC<{ open: boolean; children: React.ReactNode }> = ({ open, children }) => (
  <AnimatePresence initial={false}>
    {open && (
      <motion.div
        initial={{ height: 0, opacity: 0 }}
        animate={{ height: 'auto', opacity: 1 }}
        exit={{ height: 0, opacity: 0 }}
        transition={EASE}
        className="overflow-hidden"
      >
        {children}
      </motion.div>
    )}
  </AnimatePresence>
);

export interface WorkspaceSectionProps {
  isLight: boolean;
  projectName?: string;
  path?: string;
  lane?: domain.WorktreeChanges | null;
  onOpenChanges?: () => void;
}

export const WorkspaceSection = React.memo(function WorkspaceSection({
  isLight,
  projectName,
  path,
  lane,
  onOpenChanges,
}: WorkspaceSectionProps) {
  const tone = isLight ? LIGHT_TONE : DARK_TONE;
  const [menu, setMenu] = useState<'editor' | 'git' | null>(null);
  const [editors, setEditors] = useState<string[]>(['explorer']);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const root = lane?.path || path || '';
  const { added, removed } = useMemo(() => laneTotals(lane), [lane]);

  useEffect(() => {
    let alive = true;
    void loadEditors().then((list) => alive && setEditors(list));
    return () => {
      alive = false;
    };
  }, []);

  const run = useCallback(async (key: string, action: () => Promise<unknown>) => {
    setBusy(key);
    setError(null);
    try {
      await action();
      setMenu(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(null);
    }
  }, []);

  const primaryEditor = editors.find((id) => id !== 'explorer') ?? 'explorer';
  const toggle = (next: 'editor' | 'git') => setMenu((current) => (current === next ? null : next));
  const target = lane?.upstream?.replace(/^origin\//, '') || lane?.base || '';

  return (
    <div className="flex flex-col gap-0.5 p-1.5">
      <Row
        tone={tone}
        icon={<MaterialIcon name="folder" size={15} />}
        label={projectName || root.split(/[\\/]/).filter(Boolean).pop() || 'Project'}
        title={root}
        onClick={() => root && void run('reveal', () => OpenInEditor(root, 'explorer'))}
      />
      {root && (
        <>
          <Row
            tone={tone}
            icon={<MaterialIcon name="open_in_new" size={15} />}
            label={`Open in ${EDITOR_LABEL[primaryEditor]}`}
            open={menu === 'editor'}
            onClick={() => void run('editor', () => OpenInEditor(root, primaryEditor))}
            onToggle={editors.length > 1 ? () => toggle('editor') : undefined}
          />
          <Reveal open={menu === 'editor'}>
            {editors
              .filter((id) => id !== primaryEditor)
              .map((id) => (
                <SubRow key={id} tone={tone} label={EDITOR_LABEL[id] ?? id} busy={busy === id} onClick={() => void run(id, () => OpenInEditor(root, id))} />
              ))}
          </Reveal>
        </>
      )}
      {lane && (
        <>
          <div className={`mx-2 my-1 border-t ${tone.divider}`} />
          <Row
            tone={tone}
            icon={<MaterialIcon name="fork_right" size={15} />}
            title={target ? `${lane.branch} → ${target}` : lane.branch}
            label={
              <span className="flex min-w-0 items-center gap-1.5">
                <span className="min-w-0 truncate">{lane.branch || 'detached'}</span>
                {target && target !== lane.branch && (
                  <>
                    <MaterialIcon name="arrow_forward" size={12} className={`shrink-0 ${tone.faint}`} />
                    <span className={`min-w-0 truncate ${tone.muted}`}>{target}</span>
                  </>
                )}
              </span>
            }
            onClick={onOpenChanges}
          />
          <Row
            tone={tone}
            icon={<MaterialIcon name="commit" size={15} />}
            label="Commit, push & PR"
            open={menu === 'git'}
            onToggle={() => toggle('git')}
            trailing={
              lane.unpushed > 0 ? (
                <span className={`shrink-0 whitespace-nowrap text-[11px] tabular-nums ${tone.muted}`}>{lane.unpushed} to push</span>
              ) : undefined
            }
          />
          <Reveal open={menu === 'git'}>
            <SubRow tone={tone} label="Commit…" onClick={() => onOpenChanges?.()} />
            <SubRow tone={tone} label="Push" busy={busy === 'push'} onClick={() => void run('push', () => GitPush(lane.path))} />
            <SubRow
              tone={tone}
              label="Create pull request"
              busy={busy === 'pr'}
              onClick={() => void run('pr', async () => BrowserOpenURL(await GitPullRequestURL(lane.path)))}
            />
          </Reveal>
          <Row
            tone={tone}
            icon={<MaterialIcon name="difference" size={15} />}
            label="Changes"
            onClick={onOpenChanges}
            trailing={
              <span className="flex shrink-0 items-center gap-1.5 whitespace-nowrap font-mono text-[11px] tabular-nums">
                <span className="text-emerald-500">+{number.format(added)}</span>
                <span className="text-rose-500">-{number.format(removed)}</span>
              </span>
            }
          />
        </>
      )}
      {error && <div className={`truncate whitespace-nowrap px-2 pt-1 text-[11px] ${tone.danger}`} title={error}>{error}</div>}
    </div>
  );
});

function currentStep(agent: SubagentBlockData): string {
  for (let index = agent.entries.length - 1; index >= 0; index--) {
    const entry = agent.entries[index];
    if (entry.kind === 'tool') return `${entry.name} ${entry.target.split('\n')[0]}`.trim();
    if (entry.text.trim()) return entry.text.trim().split('\n')[0];
  }
  return agent.status === 'running' ? 'Starting' : agent.result?.trim().split('\n')[0] ?? '';
}

export const SubagentFeedRow = React.memo(function SubagentFeedRow({
  agent,
  driver = 'claude',
  modelId,
  onOpen,
}: {
  agent: SubagentBlockData;
  driver?: string;
  modelId?: string;
  onOpen: (id: string) => void;
}) {
  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';
  const tone = isLight ? LIGHT_TONE : DARK_TONE;
  const now = useNow(agent.status === 'running');
  const step = currentStep(agent);
  return (
    <button
      type="button"
      onClick={() => onOpen(agent.id)}
      className={`group my-1 flex w-full items-center gap-3 rounded-xl px-2 py-1.5 text-left transition-colors cursor-pointer ${tone.hover}`}
    >
      <span className="relative flex h-[22px] w-[22px] shrink-0 items-center justify-center">
        {renderChatIcon(driver, modelId, isLight, 'w-[18px] h-[18px]')}
        <span className={`absolute -bottom-0.5 -right-0.5 h-2 w-2 rounded-full ring-2 ${tone.ring} ${DOT_TONE[agent.status]}`} />
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="flex min-w-0 items-center gap-2 whitespace-nowrap">
          <span className={`min-w-0 truncate text-[12.5px] font-semibold tracking-tight font-(family-name:--app-font) ${tone.title}`}>
            {agent.title}
          </span>
          <span className={`shrink-0 text-[11px] font-(family-name:--app-font) ${agent.status === 'error' ? tone.danger : tone.muted}`}>
            {STATUS_WORD[agent.status]}
          </span>
        </span>
        {step && (
          <span className={`min-w-0 truncate whitespace-nowrap text-[11px] font-(family-name:--app-font) ${tone.faint}`}>{step}</span>
        )}
      </span>
      <span className={`shrink-0 whitespace-nowrap text-[11.5px] tabular-nums font-(family-name:--app-font) ${tone.muted}`}>
        {elapsedOf(agent, now)}
      </span>
      <MaterialIcon name="chevron_right" size={16} className={`shrink-0 transition-transform group-hover:translate-x-0.5 ${tone.faint}`} />
    </button>
  );
});

export interface SubagentFullViewProps {
  agent: SubagentBlockData;
  isLight: boolean;
  parentTitle: string;
  projectName?: string;
  driver?: string;
  modelId?: string;
  effort?: string;
  onBack: () => void;
}

export const SubagentFullView: React.FC<SubagentFullViewProps> = ({
  agent,
  isLight,
  parentTitle,
  projectName,
  driver = 'claude',
  modelId,
  effort,
  onBack,
}) => {
  const tone = isLight ? LIGHT_TONE : DARK_TONE;
  const running = agent.status === 'running';
  const now = useNow(running);
  const [fullPrompt, setFullPrompt] = useState(false);
  const elapsed = elapsedOf(agent, now);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.stopPropagation();
      onBack();
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [onBack]);

  const crumb = `min-w-0 truncate whitespace-nowrap cursor-pointer hover:underline ${tone.muted}`;

  return (
    <motion.div
      initial={{ opacity: 0, x: 28 }}
      animate={{ opacity: 1, x: 0 }}
      exit={{ opacity: 0, x: 28 }}
      transition={SLIDE}
      className={`absolute inset-0 z-30 flex flex-col ${isLight ? 'bg-white' : 'bg-[#0b0b0d]'}`}
      onClick={(event) => event.stopPropagation()}
    >
      <div className={`flex h-10 shrink-0 items-center gap-1.5 border-b px-4 text-[12px] font-(family-name:--app-font) ${tone.divider}`}>
        {projectName && (
          <>
            <button type="button" onClick={onBack} className={`shrink-0 ${crumb}`}>{projectName}</button>
            <span className={tone.faint}>/</span>
          </>
        )}
        <button type="button" onClick={onBack} className={crumb}>{parentTitle}</button>
        <span className={tone.faint}>/</span>
        <span className={`min-w-0 truncate whitespace-nowrap font-medium ${tone.title}`}>{agent.title}</span>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <div className="mx-auto flex w-full max-w-[720px] flex-col gap-4 px-5 pb-6 pt-4">
          <button
            type="button"
            onClick={onBack}
            className={`mx-auto flex max-w-full items-center gap-1.5 whitespace-nowrap rounded-full border px-3 py-1 text-[11px] font-(family-name:--app-font) cursor-pointer ${tone.card} ${tone.muted}`}
          >
            <MaterialIcon name="account_tree" size={12} className="shrink-0" />
            <span className="shrink-0">Subagent of</span>
            <span className={`min-w-0 truncate ${tone.body}`}>{parentTitle}</span>
          </button>

          {agent.prompt && (
            <div className="flex flex-col items-end gap-1">
              <span className={`whitespace-nowrap text-[10.5px] font-(family-name:--app-font) ${tone.faint}`}>Sent by another agent</span>
              <div className={`max-w-[85%] rounded-2xl border px-3.5 py-2.5 text-[12.5px] leading-relaxed select-text ${tone.card} ${tone.body}`}>
                <div className={fullPrompt ? '' : 'line-clamp-6'}>
                  <MarkdownText content={agent.prompt} />
                </div>
                {agent.prompt.length > 400 && (
                  <button
                    type="button"
                    onClick={() => setFullPrompt((value) => !value)}
                    className={`mt-1.5 w-full text-right text-[11px] cursor-pointer ${tone.muted}`}
                  >
                    {fullPrompt ? 'Show less' : 'Show full message'}
                  </button>
                )}
              </div>
            </div>
          )}

          <div className={`flex items-center gap-2 border-b pb-2 text-[11.5px] whitespace-nowrap font-(family-name:--app-font) ${tone.divider} ${tone.muted}`}>
            <span>{running ? 'Working for' : 'Worked for'} {elapsed}</span>
          </div>

          <div className="flex flex-col gap-2">
            {agent.entries.map((entry, index) => (
              <EntryRow key={index} entry={entry} tone={tone} />
            ))}
          </div>

          {agent.result && !running && (
            <div className={`text-[12.5px] leading-relaxed select-text ${tone.body}`}>
              <MarkdownText content={agent.result} />
            </div>
          )}
        </div>
      </ScrollArea>

      <div className="flex shrink-0 justify-center px-4 pb-4">
        <div className={`flex h-10 w-full max-w-[720px] items-center gap-2.5 rounded-full border px-4 text-[12px] whitespace-nowrap font-(family-name:--app-font) ${tone.card}`}>
          <span className="flex shrink-0 items-center">{renderChatIcon(driver, modelId, isLight, 'w-[14px] h-[14px]')}</span>
          {modelId && <span className={`min-w-0 truncate font-medium ${tone.title}`}>{formatModelBadge(modelId, driver)}</span>}
          {effort && <span className={`shrink-0 ${tone.muted}`}>{effort}</span>}
          <span className={`shrink-0 ${tone.muted}`}>{running ? `Working ${elapsed}` : STATUS_WORD[agent.status]}</span>
          <span className="flex-1" />
          <span className={`shrink-0 ${tone.faint}`}>Runs on its own</span>
          <button
            type="button"
            onClick={onBack}
            className={`flex shrink-0 items-center gap-1 font-medium cursor-pointer ${tone.title}`}
          >
            <MaterialIcon name="arrow_back" size={13} />
            Open parent
          </button>
        </div>
      </div>
    </motion.div>
  );
};

