import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { LegendList } from '@legendapp/list/react';
import { AnimatePresence, motion } from 'motion/react';
import { MaterialIcon } from '../common/icons';
import { ScrollArea } from '../common/ScrollArea';
import { MarkdownText } from './MarkdownText';
import { OrbitLoader } from './OrbitLoader';
import { SubagentBlockData, SubagentEntry } from './types';
import { renderChatIcon } from '../common/DynamicIsland';
import { subagentTotals } from './threadChanges';
import { GlassSquircle } from '../../ui/glass/GlassSquircle';

export const KNOWN_DRIVERS = new Set(['claude', 'codex', 'antigravity', 'opencode']);

export type SubagentWithDriver = SubagentBlockData & {
  driver?: string;
  model?: string;
  cwd?: string;
};

/** Resolve the real CLI driver for a subagent so icon rendering falls back to the parent driver instead of unknown agent types. */
export function resolveSubagentDriver(agent: SubagentBlockData, fallbackDriver: string): { driver: string; model?: string } {
  const sub = agent as SubagentWithDriver;
  const candidateDriver = sub.driver?.trim().toLowerCase();
  const candidateType = agent.agentType?.trim().toLowerCase();
  const resolved =
    (candidateDriver && KNOWN_DRIVERS.has(candidateDriver) ? candidateDriver : undefined) ??
    (candidateType && KNOWN_DRIVERS.has(candidateType) ? candidateType : undefined) ??
    fallbackDriver;
  return { driver: resolved, model: sub.model };
}

export interface SubagentMenuProps {
  agents: SubagentBlockData[];
  isLight: boolean;
  driver?: string;
  modelId?: string;
  dockable?: boolean;
  workspace?: React.ReactNode;
  inspector?: React.ReactNode;
  onOpenAgent?: (id: string) => void;
}

const LIST_WIDTH = 440;
const COMPACT_WIDTH = 330;
const TRANSCRIPT_WIDTH = 560;
const TRANSCRIPT_HEIGHT = 420;
const EDGE = 12;
const DOCK_WIDTH = 330;
const DOCK_MIN_WINDOW = 1100;
const DOCK_TOP = 84;
const DOCK_RIGHT = 24;

const TOOL_ICONS: Record<string, string> = {
  read: 'search',
  glob: 'search',
  grep: 'search',
  websearch: 'search',
  webfetch: 'language',
  bash: 'terminal',
  powershell: 'terminal',
  edit: 'edit',
  write: 'edit_document',
  notebookedit: 'edit_document',
  todowrite: 'checklist',
  task: 'fork_right',
  agent: 'fork_right',
};

const SPRING = { type: 'spring', stiffness: 420, damping: 34, mass: 0.8 } as const;
const REVEAL_EASE = { duration: 0.22, ease: [0.32, 0.72, 0, 1] } as const;

function isStacked(agents: SubagentBlockData[]): boolean {
  return agents.filter((agent) => agent.status !== 'running').length > 1;
}

export const STATUS_WORD: Record<SubagentBlockData['status'], string> = {
  running: 'Running',
  completed: 'Done',
  error: 'Failed',
  stopped: 'Stopped',
};

export const DOT_TONE: Record<SubagentBlockData['status'], string> = {
  running: 'bg-amber-400 animate-pulse shadow-[0_0_6px_rgba(251,191,36,0.8)]',
  completed: 'bg-emerald-400',
  error: 'bg-rose-500',
  stopped: 'bg-zinc-400',
};

export interface Tone {
  panel: string;
  ring: string;
  divider: string;
  title: string;
  body: string;
  muted: string;
  faint: string;
  hover: string;
  well: string;
  card: string;
  danger: string;
  close: string;
  stackFront: string;
  stackMid: string;
  stackBack: string;
  stackRing: string;
}

export const DARK_TONE: Tone = {
  panel: 'bg-[#0b0b0d] border-white/[0.08] text-white shadow-[0_28px_64px_-12px_rgba(0,0,0,0.75)]',
  ring: 'ring-[#0b0b0d]',
  divider: 'border-white/10',
  title: 'text-white/90',
  body: 'text-white/85',
  muted: 'text-white/45',
  faint: 'text-white/35',
  hover: 'hover:bg-white/[0.07]',
  well: 'bg-white/[0.08]',
  card: 'border-white/10 bg-white/[0.04]',
  danger: 'text-rose-400',
  close: 'text-white/50 hover:bg-white/15 hover:text-white',
  stackFront: 'bg-[#161619] border-white/10 group-hover:bg-[#1c1c20]',
  stackMid: 'bg-[#121215] border-white/[0.08]',
  stackBack: 'bg-[#0f0f11] border-white/[0.06]',
  stackRing: 'ring-[#161619]',
};

export const LIGHT_TONE: Tone = {
  panel: 'bg-white border-black/10 text-[#111] shadow-[0_24px_60px_-12px_rgba(0,0,0,0.25)]',
  ring: 'ring-white',
  divider: 'border-black/[0.08]',
  title: 'text-[#111]',
  body: 'text-black/80',
  muted: 'text-black/45',
  faint: 'text-black/35',
  hover: 'hover:bg-black/[0.05]',
  well: 'bg-black/[0.06]',
  card: 'border-black/10 bg-black/[0.03]',
  danger: 'text-rose-600',
  close: 'text-black/50 hover:bg-black/10 hover:text-black',
  stackFront: 'bg-white border-black/10 shadow-[0_1px_3px_rgba(0,0,0,0.06)] group-hover:bg-[#f7f7f9]',
  stackMid: 'bg-[#f4f4f6] border-black/[0.08]',
  stackBack: 'bg-[#ececef] border-black/[0.06]',
  stackRing: 'ring-white',
};

const tickListeners = new Set<(now: number) => void>();
let tickTimer = 0;

export function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    tickListeners.add(setNow);
    if (!tickTimer) {
      tickTimer = window.setInterval(() => {
        const t = Date.now();
        tickListeners.forEach((listener) => listener(t));
      }, 1000);
    }
    return () => {
      tickListeners.delete(setNow);
      if (tickListeners.size === 0) {
        window.clearInterval(tickTimer);
        tickTimer = 0;
      }
    };
  }, [active]);
  return now;
}

export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000));
  if (total < 60) return `${total}s`;
  const minutes = Math.floor(total / 60);
  if (minutes < 60) return `${minutes}m ${String(total % 60).padStart(2, '0')}s`;
  return `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, '0')}m`;
}

export function elapsedOf(agent: SubagentBlockData, now: number): string {
  if (!agent.startedAt) return '';
  const end = agent.status === 'running' ? now : (agent.endedAt ?? now);
  return formatElapsed(end - agent.startedAt);
}

function totalElapsedMs(agents: SubagentBlockData[], now: number): number {
  return agents.reduce((sum, agent) => {
    if (agent.startedAt == null) return sum;
    const end = agent.status === 'running' ? now : (agent.endedAt ?? now);
    const diff = end - agent.startedAt;
    return sum + (diff > 0 ? diff : 0);
  }, 0);
}

function toolCount(agent: SubagentBlockData): number {
  const seen = agent.entries.filter((entry) => entry.kind === 'tool').length;
  return Math.max(seen, agent.toolCount ?? 0);
}

const DiffStatBadge: React.FC<{ additions: number; deletions: number }> = ({ additions, deletions }) => {
  if (additions <= 0 && deletions <= 0) return null;
  return (
    <span className="flex shrink-0 items-center gap-1.5 text-[11.5px] tabular-nums font-(family-name:--app-font)">
      {additions > 0 && <span className="text-emerald-500">+{additions}</span>}
      {deletions > 0 && <span className="text-rose-500">−{deletions}</span>}
    </span>
  );
};

export const StatusMark: React.FC<{ status: SubagentBlockData['status']; size?: number }> = ({ status, size = 13 }) => {
  if (status === 'running') return <OrbitLoader size={size} />;
  const tone =
    status === 'completed' ? 'text-emerald-500' : status === 'error' ? 'text-rose-500' : 'text-zinc-400';
  const icon = status === 'completed' ? 'check_circle' : status === 'error' ? 'error' : 'block';
  return (
    <span className={`inline-flex ${tone}`}>
      <MaterialIcon name={icon} size={size + 1} />
    </span>
  );
};

export const EntryRow: React.FC<{ entry: SubagentEntry; tone: Tone }> = ({ entry, tone }) => {
  if (entry.kind === 'text') {
    return (
      <div className={`text-[12px] leading-relaxed select-text pl-0.5 ${tone.body}`}>
        <MarkdownText content={entry.text} />
      </div>
    );
  }
  const icon = TOOL_ICONS[entry.name.toLowerCase()] || 'build';
  const color = entry.status === 'error' ? tone.danger : entry.status === 'running' ? tone.body : tone.muted;
  return (
    <div className={`flex items-center gap-2 text-[11.5px] font-(family-name:--app-font) ${color}`}>
      <span className="shrink-0 inline-flex">
        {entry.status === 'running' ? <OrbitLoader size={11} /> : <MaterialIcon name={icon} size={13} />}
      </span>
      <span className="shrink-0 font-medium">{entry.name}</span>
      <span className="min-w-0 flex-1 truncate font-mono text-[10.5px] opacity-80">{entry.target.split('\n')[0]}</span>
    </div>
  );
};

const Transcript: React.FC<{
  agent: SubagentBlockData;
  now: number;
  tone: Tone;
  onBack: () => void;
}> = ({ agent, now, tone, onBack }) => {
  const [briefOpen, setBriefOpen] = useState(false);

  const stats = subagentTotals(agent);
  const meta = [
    agent.agentType,
    STATUS_WORD[agent.status],
    (stats.additions > 0 || stats.deletions > 0) && `+${stats.additions} −${stats.deletions} lines`,
    `${toolCount(agent)} tool calls`,
    elapsedOf(agent, now),
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <div className="flex flex-col" style={{ height: TRANSCRIPT_HEIGHT }}>
      <div className={`flex items-center gap-2 border-b px-3 py-2.5 ${tone.divider}`}>
        <button
          type="button"
          onClick={onBack}
          title="Back to subagents"
          className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full cursor-pointer ${tone.close}`}
        >
          <MaterialIcon name="arrow_back" size={14} />
        </button>
        <StatusMark status={agent.status} />
        <div className="min-w-0 flex-1">
          <div className={`truncate text-[12.5px] font-medium font-(family-name:--app-font) ${tone.title}`}>
            {agent.title}
          </div>
          <div className={`truncate text-[10.5px] font-(family-name:--app-font) tabular-nums ${tone.faint}`}>{meta}</div>
        </div>
      </div>
      <LegendList
        className="flex-1 min-h-0 custom-scrollbar"
        style={{ flex: 1, minHeight: 0 }}
        contentContainerStyle={{ paddingTop: 12, paddingBottom: 12, paddingLeft: 14, paddingRight: 14 }}
        data={agent.entries}
        keyExtractor={(entry) => entry.id}
        renderItem={({ item }) => (
          <div className="pb-2.5">
            <EntryRow entry={item} tone={tone} />
          </div>
        )}
        extraData={tone}
        estimatedItemSize={28}
        drawDistance={600}
        recycleItems={false}
        initialScrollAtEnd
        maintainScrollAtEnd
        maintainScrollAtEndThreshold={0.1}
        ListHeaderComponent={
          <div className="flex flex-col gap-2.5 pb-2.5">
            {agent.prompt && (
              <button
                type="button"
                onClick={() => setBriefOpen((value) => !value)}
                className={`rounded-lg border px-2.5 py-2 text-left cursor-pointer ${tone.card}`}
              >
                <div className={`text-[9.5px] uppercase tracking-wider font-(family-name:--app-font) ${tone.faint}`}>
                  Brief
                </div>
                <div
                  className={`mt-0.5 whitespace-pre-wrap text-[11.5px] leading-relaxed ${tone.muted} ${
                    briefOpen ? '' : 'line-clamp-2'
                  }`}
                >
                  {agent.prompt}
                </div>
              </button>
            )}
            {agent.entries.length === 0 && agent.status === 'running' && (
              <div className={`text-[11.5px] font-(family-name:--app-font) ${tone.muted}`}>Waiting for the first step…</div>
            )}
          </div>
        }
        ListFooterComponent={
          agent.result && agent.status !== 'running' ? (
            <div className="mt-1 rounded-lg border border-emerald-400/20 bg-emerald-400/[0.06] px-2.5 py-2">
              <div className="text-[9.5px] uppercase tracking-wider text-emerald-500/70 font-(family-name:--app-font)">
                Result
              </div>
              <div className={`mt-0.5 text-[12px] leading-relaxed select-text ${tone.body}`}>
                <MarkdownText content={agent.result} />
              </div>
            </div>
          ) : undefined
        }
      />
    </div>
  );
};

const AgentRow: React.FC<{
  agent: SubagentBlockData;
  now: number;
  tone: Tone;
  driver: string;
  modelId?: string;
  isLight: boolean;
  faded?: boolean;
  onSelect: () => void;
}> = ({ agent, now, tone, driver, modelId, isLight, faded = false, onSelect }) => {
  const stats = subagentTotals(agent);
  const resolved = resolveSubagentDriver(agent, driver);
  return (
    <button
      type="button"
      onClick={onSelect}
      title={[
        [
          (stats.additions > 0 || stats.deletions > 0) && `+${stats.additions} −${stats.deletions} lines`,
          stats.files > 0 && `${stats.files} file${stats.files === 1 ? '' : 's'}`,
          `${toolCount(agent)} tool calls`,
        ]
          .filter(Boolean)
          .join(' · '),
        agent.prompt?.slice(0, 280),
      ]
        .filter(Boolean)
        .join(' - ')}
      className={`flex h-8 w-full items-center gap-2.5 rounded-[9px] px-2 text-left transition-[background-color,opacity] cursor-pointer ${tone.hover} ${
        faded ? 'opacity-70 hover:opacity-100' : ''
      }`}
    >
      <span className="relative flex h-4 w-4 shrink-0 items-center justify-center">
        {renderChatIcon(resolved.driver, resolved.model || modelId, isLight, 'w-4 h-4')}
        <span
          className={`absolute -bottom-0.5 -right-0.5 h-1.5 w-1.5 rounded-full ring-[1.5px] ${tone.ring} ${DOT_TONE[agent.status]}`}
        />
      </span>
      <span className={`min-w-0 flex-1 truncate text-[12px] font-medium tracking-tight font-(family-name:--app-font) ${tone.title}`}>
        {agent.title}
      </span>
      <DiffStatBadge additions={stats.additions} deletions={stats.deletions} />
      <span className={`shrink-0 text-[11px] tabular-nums font-(family-name:--app-font) ${tone.muted}`}>
        {elapsedOf(agent, now)}
      </span>
      <span
        className={`w-[44px] shrink-0 text-right text-[11px] font-(family-name:--app-font) ${
          agent.status === 'error' ? tone.danger : tone.faint
        }`}
      >
        {STATUS_WORD[agent.status]}
      </span>
    </button>
  );
};

function settledParts(agents: SubagentBlockData[]): { text: string; danger?: boolean }[] {
  const done = agents.filter((agent) => agent.status === 'completed').length;
  const failed = agents.filter((agent) => agent.status === 'error').length;
  const stopped = agents.filter((agent) => agent.status === 'stopped').length;
  const parts: { text: string; danger?: boolean }[] = [];
  if (done > 0) parts.push({ text: `${done} done` });
  if (failed > 0) parts.push({ text: `${failed} failed`, danger: true });
  if (stopped > 0) parts.push({ text: `${stopped} stopped` });
  return parts;
}

const FinishedRow: React.FC<{
  agents: SubagentBlockData[];
  now: number;
  tone: Tone;
  driver: string;
  modelId?: string;
  isLight: boolean;
  expanded: boolean;
  onToggle: () => void;
}> = ({ agents, now, tone, driver, modelId, isLight, expanded, onToggle }) => {
  const parts = settledParts(agents);
  const resolved = agents.length > 0 ? resolveSubagentDriver(agents[0], driver) : { driver, model: undefined };
  const topStatus: SubagentBlockData['status'] = agents.some((a) => a.status === 'error')
    ? 'error'
    : agents.some((a) => a.status === 'stopped')
      ? 'stopped'
      : 'completed';
  const hasNonCompleted =
    parts.some((p) => p.danger) ||
    parts.length > 1 ||
    agents.some((agent) => agent.status !== 'completed');
  const totalMs = totalElapsedMs(agents, now);
  const summaryParts =
    hasNonCompleted || totalMs <= 0 ? parts : [{ text: formatElapsed(totalMs), danger: false }];

  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={expanded}
      title={`${expanded ? 'Collapse' : 'Show'} ${agents.length} finished subagents`}
      className={`group flex h-8 w-full items-center gap-2.5 rounded-[10px] px-2 text-left transition-colors cursor-pointer select-none ${tone.hover}`}
    >
      <span className="relative flex h-4 w-4 shrink-0 items-center justify-center">
        {renderChatIcon(resolved.driver, resolved.model || modelId, isLight, 'w-4 h-4')}
        <span
          className={`absolute -bottom-0.5 -right-0.5 h-1.5 w-1.5 rounded-full ring-[1.5px] ${tone.ring} ${DOT_TONE[topStatus]}`}
        />
      </span>
      <span className={`min-w-0 flex-1 truncate whitespace-nowrap text-[12.5px] font-medium tracking-tight font-(family-name:--app-font) ${tone.title}`}>
        {agents.length} finished
      </span>
      <span className="flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[11.5px] tabular-nums font-(family-name:--app-font)">
        {summaryParts.map((part, index) => (
          <React.Fragment key={part.text}>
            {index > 0 && <span className={tone.faint}>·</span>}
            <span className={part.danger ? tone.danger : tone.muted}>{part.text}</span>
          </React.Fragment>
        ))}
      </span>
      <span className={`-mr-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-full ${tone.close}`}>
        <MaterialIcon
          name="expand_more"
          size={15}
          className={`transition-transform duration-200 ${expanded ? 'rotate-180' : ''}`}
        />
      </span>
    </button>
  );
};

const AgentList: React.FC<{
  agents: SubagentBlockData[];
  now: number;
  tone: Tone;
  driver: string;
  modelId?: string;
  isLight: boolean;
  expanded: boolean;
  onExpandedChange: (expanded: boolean) => void;
  onSelect: (id: string) => void;
}> = ({ agents, now, tone, driver, modelId, isLight, expanded, onExpandedChange, onSelect }) => {
  const live = agents.filter((agent) => agent.status === 'running');
  const settled = agents.filter((agent) => agent.status !== 'running');
  const stacks = isStacked(agents);
  const row = (agent: SubagentBlockData, faded: boolean) => (
    <AgentRow
      agent={agent}
      now={now}
      tone={tone}
      driver={driver}
      modelId={modelId}
      isLight={isLight}
      faded={faded}
      onSelect={() => onSelect(agent.id)}
    />
  );

  if (!stacks) {
    return (
      <div className="flex flex-col gap-0.5">
        {agents.map((agent) => (
          <React.Fragment key={agent.id}>{row(agent, agent.status !== 'running' && live.length > 0)}</React.Fragment>
        ))}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-0.5">
      {live.map((agent) => (
        <React.Fragment key={agent.id}>{row(agent, false)}</React.Fragment>
      ))}
      <FinishedRow
        agents={settled}
        now={now}
        tone={tone}
        driver={driver}
        modelId={modelId}
        isLight={isLight}
        expanded={expanded}
        onToggle={() => onExpandedChange(!expanded)}
      />
      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            key="settled-list"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={REVEAL_EASE}
            className="flex flex-col gap-0.5 overflow-hidden pl-[26px]"
          >
            {settled.map((agent) => (
              <React.Fragment key={agent.id}>{row(agent, live.length > 0)}</React.Fragment>
            ))}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
};

const PILL_DARK = 'bg-[#2a2a2a] hover:bg-[#363636] text-white/90 border-white/15';
const PILL_LIGHT = 'bg-white hover:bg-[#efeff2] text-[#111] border-black/10 shadow-[0_1px_2px_rgba(0,0,0,0.05)]';

export const SubagentMenu: React.FC<SubagentMenuProps> = ({ agents, isLight, driver = 'claude', modelId, dockable = false, workspace, inspector, onOpenAgent }) => {
  const [open, setOpen] = useState(false);
  const [openId, setOpenId] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [anchor, setAnchor] = useState({ top: 0, right: 0, side: false });
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const tone = isLight ? LIGHT_TONE : DARK_TONE;
  const [windowWidth, setWindowWidth] = useState(() => window.innerWidth);
  useEffect(() => {
    const onResize = () => setWindowWidth(window.innerWidth);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);
  const docked = dockable && windowWidth >= DOCK_MIN_WINDOW;
  const runningCount = agents.filter((agent) => agent.status === 'running').length;
  const failedCount = agents.filter((agent) => agent.status === 'error').length;
  const now = useNow(runningCount > 0);
  const totalMs = totalElapsedMs(agents, now);
  const totalLabel = totalMs > 0 ? `${formatElapsed(totalMs)} total` : '';
  const pillTitle = totalLabel
    ? `${agents.length} subagent${agents.length === 1 ? '' : 's'} · ${totalLabel}`
    : `${agents.length} subagent${agents.length === 1 ? '' : 's'}`;
  const selected = useMemo(() => agents.find((agent) => agent.id === openId) ?? null, [agents, openId]);
  const compact = runningCount === 0 && !expanded && isStacked(agents);
  const listWidth = compact ? COMPACT_WIDTH : LIST_WIDTH;

  const measure = useCallback(() => {
    const button = buttonRef.current;
    if (!button) return;
    const box = button.getBoundingClientRect();
    const card = button.closest('[data-agent-card]')?.getBoundingClientRect();
    const wanted = Math.min(selected ? TRANSCRIPT_WIDTH : listWidth, window.innerWidth - EDGE * 2);
    if (card && window.innerWidth - card.right >= wanted + EDGE * 2) {
      setAnchor({ top: Math.max(EDGE, card.top + 8), right: window.innerWidth - card.right - wanted - EDGE, side: true });
      return;
    }
    setAnchor({ top: box.bottom + 8, right: Math.max(EDGE, window.innerWidth - box.right), side: false });
  }, [selected, listWidth]);

  const close = useCallback(() => {
    setOpen(false);
    setOpenId(null);
    setExpanded(false);
  }, []);

  useEffect(() => {
    if (openId && !selected) setOpenId(null);
  }, [openId, selected]);

  useLayoutEffect(() => {
    if (open) measure();
  }, [open, measure]);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.stopPropagation();
      event.preventDefault();
      close();
    };
    const onPointer = (event: PointerEvent) => {
      const target = event.target as Node | null;
      if (!target) return;
      if (panelRef.current?.contains(target) || buttonRef.current?.contains(target)) return;
      close();
    };
    window.addEventListener('keydown', onKey, true);
    document.addEventListener('pointerdown', onPointer, true);
    window.addEventListener('resize', measure);
    return () => {
      window.removeEventListener('keydown', onKey, true);
      document.removeEventListener('pointerdown', onPointer, true);
      window.removeEventListener('resize', measure);
    };
  }, [open, close, measure]);

  if (agents.length === 0 && !workspace && !inspector) return null;

  const width = Math.min(selected ? TRANSCRIPT_WIDTH : listWidth, window.innerWidth - EDGE * 2);
  const list = (
    <AgentList
      agents={agents}
      now={now}
      tone={tone}
      driver={driver}
      modelId={modelId}
      isLight={isLight}
      expanded={expanded}
      onExpandedChange={setExpanded}
      onSelect={onOpenAgent ?? setOpenId}
    />
  );

  if (docked) {
    const hasInspector = Boolean(inspector);
    const dockWidth = selected
      ? TRANSCRIPT_WIDTH
      : agents.length === 0
        ? (hasInspector ? 300 : 280)
        : DOCK_WIDTH;
    return createPortal(
      <div
        onClick={(event) => event.stopPropagation()}
        style={{
          position: 'fixed',
          top: DOCK_TOP,
          right: DOCK_RIGHT,
          width: dockWidth,
          maxHeight: `calc(100vh - ${DOCK_TOP}px - var(--input-dock-h, 150px))`,
          zIndex: 45,
          transition: 'width 220ms cubic-bezier(0.32, 0.72, 0, 1)',
        }}
        className="pointer-events-auto flex flex-col"
      >
        <GlassSquircle
          radius={20}
          fill={isLight ? 'rgba(255, 255, 255, 0.75)' : 'linear-gradient(135deg, #0B0A09 0%, #191616 100%)'}
          frost={16}
          border={{
            strokeWidth: 1,
            align: 'inside',
            gradient: isLight
              ? 'linear-gradient(135deg, rgba(0, 0, 0, 0.25) 0%, rgba(0, 0, 0, 0.06) 25%, rgba(0, 0, 0, 0) 45%, rgba(0, 0, 0, 0) 55%, rgba(0, 0, 0, 0.04) 75%, rgba(0, 0, 0, 0.18) 100%)'
              : 'linear-gradient(135deg, rgba(255, 255, 255, 0.45) 0%, rgba(255, 255, 255, 0.10) 25%, rgba(255, 255, 255, 0) 45%, rgba(255, 255, 255, 0) 55%, rgba(255, 255, 255, 0.08) 75%, rgba(255, 255, 255, 0.32) 100%)',
          }}
          style={{
            borderRadius: 20,
            boxShadow: isLight
              ? '0 10px 30px rgba(0, 0, 0, 0.08)'
              : '0 14px 40px rgba(0, 0, 0, 0.55)',
            overflow: 'hidden',
          }}
          className={`flex flex-col w-full ${isLight ? 'text-black' : 'text-white'}`}
        >
          {selected ? (
            <Transcript agent={selected} now={now} tone={tone} onBack={() => setOpenId(null)} />
          ) : (
            <div className="flex min-h-0 flex-1 flex-col overflow-y-auto overflow-x-hidden custom-scrollbar">
              {workspace}
              {agents.length > 0 && (
                <div className="flex min-h-0 flex-col px-1.5">
                  {workspace && <div className={`mx-2 my-1 border-t ${tone.divider}`} />}
                  <ScrollArea maxHeight="calc(100vh - 220px)" className="flex flex-col">
                    {list}
                  </ScrollArea>
                </div>
              )}
              {inspector}
            </div>
          )}
        </GlassSquircle>
      </div>,
      document.body,
    );
  }

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          if (open) close();
          else setOpen(true);
        }}
        title={agents.length > 0 ? pillTitle : 'Session inspector'}
        aria-label={agents.length > 0 ? 'Subagents' : 'Session inspector'}
        aria-expanded={open}
        className={`pointer-events-auto relative flex h-[28px] shrink-0 items-center gap-1.5 rounded-full border px-3 text-[12px] font-medium tabular-nums font-(family-name:--app-font) transition-colors cursor-pointer active:scale-95 ${
          isLight ? PILL_LIGHT : PILL_DARK
        }`}
      >
        {agents.length > 0 ? (
          <>
            <span>Subagents {agents.length}</span>
            {runningCount > 0 && (
              <span className="h-1.5 w-1.5 rounded-full bg-amber-400 animate-pulse shadow-[0_0_6px_rgba(251,191,36,0.9)]" />
            )}
            {runningCount === 0 && failedCount > 0 && <span className="h-1.5 w-1.5 rounded-full bg-rose-500" />}
          </>
        ) : (
          <>
            <MaterialIcon name="stacks" size={13} />
            <span>Inspector</span>
          </>
        )}
      </button>
      {createPortal(
        <AnimatePresence>
          {open && (
            <motion.div
              ref={panelRef}
              key="subagent-panel"
              initial={{ opacity: 0, scale: 0.9, y: -8 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.94, y: -6 }}
              transition={SPRING}
              onClick={(event) => event.stopPropagation()}
              style={{
                position: 'fixed',
                top: anchor.top,
                right: anchor.right,
                width,
                maxHeight: `calc(100vh - ${anchor.top + EDGE}px)`,
                transformOrigin: anchor.side ? 'top left' : 'top right',
                zIndex: 2000,
                transition: 'width 220ms cubic-bezier(0.32, 0.72, 0, 1)',
              }}
              className="pointer-events-auto flex flex-col"
            >
              <GlassSquircle
                radius={18}
                fill={isLight ? 'rgba(255, 255, 255, 0.75)' : 'rgba(14, 15, 18, 0.72)'}
                frost={16}
                border={{
                  strokeWidth: 1,
                  align: 'inside',
                  light: {
                    angle: 303,
                    intensity: 0.38,
                    backIntensity: 0.22,
                  },
                }}
                style={{
                  boxShadow: isLight
                    ? '0 10px 30px rgba(0, 0, 0, 0.08)'
                    : '0 14px 40px rgba(0, 0, 0, 0.55)',
                  overflow: 'hidden',
                }}
                className={`flex flex-col w-full ${isLight ? 'text-black' : 'text-white'}`}
              >
                {selected ? (
                  <Transcript agent={selected} now={now} tone={tone} onBack={() => setOpenId(null)} />
                ) : (
                  <ScrollArea maxHeight={`calc(100vh - ${anchor.top + EDGE * 2}px)`} className="flex min-h-0 flex-col">
                    {workspace}
                    {agents.length > 0 && (
                      <div className="flex min-h-0 flex-col px-1.5">
                        {workspace && <div className={`mx-2 my-1 border-t ${tone.divider}`} />}
                        {list}
                      </div>
                    )}
                    {inspector}
                  </ScrollArea>
                )}
              </GlassSquircle>
            </motion.div>
          )}
        </AnimatePresence>,
        document.body,
      )}
    </>
  );
};

export default SubagentMenu;
