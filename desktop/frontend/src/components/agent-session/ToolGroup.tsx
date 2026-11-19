import { MaterialIcon } from '../common/icons';
import React, { useEffect, useRef, useState } from 'react';
import { Chevron, Collapse, OutputPanel, WorkRow } from './WorkRow';
import { ROW_DOT_Y, TimelineDot, useInTimeline } from './Timeline';
import { TextShimmer } from './TextShimmer';

export interface ToolGroupItem {
  id?: string;
  type: 'read' | 'bash' | 'search' | 'edit' | 'write' | 'git' | 'generic';
  action: string;
  target: string;
  details?: string;
  status?: 'running' | 'completed' | 'error';
  driver?: string;
}

export interface ToolGroupProps {
  title?: string;
  summary?: string;
  items: ToolGroupItem[];
  defaultExpanded?: boolean;
  className?: string;
  driver?: string;
}

const ICONS: Record<ToolGroupItem['type'], string> = {
  read: 'search',
  search: 'search',
  bash: 'terminal',
  git: 'commit',
  edit: 'edit',
  write: 'edit_document',
  generic: 'build',
};

const FILE_TYPES = new Set<ToolGroupItem['type']>(['read', 'edit', 'write']);

const EXT_BADGES: Record<string, { label: string; tone: string }> = {
  ts: { label: 'TS', tone: 'text-sky-400 bg-sky-400/10 border-sky-400/25' },
  tsx: { label: 'React', tone: 'text-cyan-400 bg-cyan-400/10 border-cyan-400/25' },
  jsx: { label: 'React', tone: 'text-cyan-400 bg-cyan-400/10 border-cyan-400/25' },
  js: { label: 'JS', tone: 'text-yellow-400 bg-yellow-400/10 border-yellow-400/25' },
  mjs: { label: 'JS', tone: 'text-yellow-400 bg-yellow-400/10 border-yellow-400/25' },
  cjs: { label: 'JS', tone: 'text-yellow-400 bg-yellow-400/10 border-yellow-400/25' },
  go: { label: 'Go', tone: 'text-blue-400 bg-blue-400/10 border-blue-400/25' },
  py: { label: 'Py', tone: 'text-emerald-400 bg-emerald-400/10 border-emerald-400/25' },
  rs: { label: 'Rust', tone: 'text-amber-500 bg-amber-500/10 border-amber-500/25' },
  json: { label: 'JSON', tone: 'text-amber-400 bg-amber-400/10 border-amber-400/25' },
  css: { label: 'CSS', tone: 'text-pink-400 bg-pink-400/10 border-pink-400/25' },
  scss: { label: 'CSS', tone: 'text-pink-400 bg-pink-400/10 border-pink-400/25' },
  html: { label: 'HTML', tone: 'text-orange-400 bg-orange-400/10 border-orange-400/25' },
  md: { label: 'MD', tone: 'text-purple-400 bg-purple-400/10 border-purple-400/25' },
  yaml: { label: 'YAML', tone: 'text-amber-300 bg-amber-300/10 border-amber-300/25' },
  yml: { label: 'YAML', tone: 'text-amber-300 bg-amber-300/10 border-amber-300/25' },
  sql: { label: 'SQL', tone: 'text-indigo-400 bg-indigo-400/10 border-indigo-400/25' },
};

function parseAntigravityTarget(rawTarget: string) {
  const target = rawTarget.trim();
  if (!target) return null;
  const lineMatch = target.match(/^(.*?):(\d+(?:-\d+)?)$/);
  const pathPart = lineMatch ? lineMatch[1] : target;
  const lines = lineMatch ? `#L${lineMatch[2]}` : undefined;
  const base = pathPart.split(/[\\/]/).filter(Boolean).pop() || pathPart;
  const ext = base.split('.').pop()?.toLowerCase() || '';
  const badge = EXT_BADGES[ext];
  return { base, lines, badge };
}

function displayTarget(item: ToolGroupItem): string {
  const target = item.target.trim();
  if (!target) return '';
  if (FILE_TYPES.has(item.type) || item.driver === 'antigravity') {
    const base = target.split(/[\\/]/).filter(Boolean).pop();
    return base || target;
  }
  const line = target.split('\n').find((candidate) => candidate.trim()) || target;
  return line.trim();
}

const PRESENT: Record<string, string> = {
  Read: 'Reading',
  Ran: 'Running',
  Inspect: 'Inspecting',
  Glob: 'Searching',
  Grep: 'Searching',
  Search: 'Searching',
  Fetch: 'Fetching',
  Edit: 'Editing',
  Write: 'Writing',
};

export const ToolItemRow = React.memo(function ToolItemRow({
  item,
  isAntigravity,
}: {
  item: ToolGroupItem;
  isAntigravity?: boolean;
}) {
  const details = item.details?.trim();
  const running = item.status === 'running';

  if (isAntigravity && (item.type === 'read' || item.action === 'Read' || item.action === 'Analyzed')) {
    const parsed = parseAntigravityTarget(item.target);
    if (parsed) {
      const verb = running ? 'Reading' : 'Analyzed';
      const labelNode = (
        <span className="flex items-center gap-1.5 min-w-0">
          <span className="text-current/40 font-normal">{verb}</span>
          {parsed.badge && (
            <span
              className={`px-1 py-[0.5px] rounded-[4px] text-[10px] font-semibold tracking-tight border ${parsed.badge.tone}`}
            >
              {parsed.badge.label}
            </span>
          )}
          <span className="text-current/90 font-medium truncate">{parsed.base}</span>
          {parsed.lines && (
            <span className="text-current/40 font-normal shrink-0">{parsed.lines}</span>
          )}
        </span>
      );

      return (
        <WorkRow
          label=""
          target=""
          meta={labelNode}
          running={running}
          error={item.status === 'error'}
        >
          {details ? <OutputPanel text={details} /> : undefined}
        </WorkRow>
      );
    }
  }

  return (
    <WorkRow
      icon={ICONS[item.type]}
      label={running ? PRESENT[item.action] ?? item.action : item.action}
      target={displayTarget(item)}
      running={running}
      error={item.status === 'error'}
    >
      {details ? <OutputPanel text={details} /> : undefined}
    </WorkRow>
  );
});

const COLLAPSE_AT = 4;

const ToolGroupImpl: React.FC<ToolGroupProps> = ({
  items = [],
  defaultExpanded,
  className = '',
  driver,
}) => {
  const inTimeline = useInTimeline();
  const isAntigravity = driver === 'antigravity' || items.some((item) => item.driver === 'antigravity');
  const anyRunning = items.some((item) => item.status === 'running');
  const runningItem = items.find((item) => item.status === 'running');
  const [open, setOpen] = useState(isAntigravity ? (defaultExpanded ?? false) : (defaultExpanded ?? anyRunning));
  const touched = useRef(false);

  useEffect(() => {
    if (touched.current) return;
    if (!isAntigravity) {
      setOpen(anyRunning);
    }
  }, [anyRunning, isAntigravity]);

  if (items.length < COLLAPSE_AT) {
    return (
      <div className={`flex flex-col ${className}`}>
        {items.map((item, index) => (
          <ToolItemRow key={item.id || index} item={item} isAntigravity={isAntigravity} />
        ))}
      </div>
    );
  }

  const label = `Used ${items.length} tools`;
  return (
    <div className={`relative flex flex-col font-(family-name:--app-font) ${className}`}>
      {inTimeline && <TimelineDot y={ROW_DOT_Y} tone={anyRunning ? 'running' : 'row'} />}
      <button
        type="button"
        onClick={() => {
          touched.current = true;
          setOpen((value) => !value);
        }}
        className="group/work flex items-center gap-2 self-start py-[3px] text-[12.5px] tracking-tight leading-[18px] text-current/55 hover:text-current/85 transition-colors duration-150 cursor-pointer"
      >
        {anyRunning ? <TextShimmer duration={1.6}>{label}</TextShimmer> : <span>{label}</span>}
        <Chevron open={open} direction="down" />
      </button>

      {/* For Antigravity: when the group is collapsed and a tool is running, show the in-flight item right below */}
      {isAntigravity && !open && anyRunning && runningItem && (
        <div className="pl-2 pt-0.5">
          <ToolItemRow item={runningItem} isAntigravity={true} />
        </div>
      )}

      <Collapse open={open}>
        <div className="flex flex-col">
          {items.map((item, index) => (
            <ToolItemRow key={item.id || index} item={item} isAntigravity={isAntigravity} />
          ))}
        </div>
      </Collapse>
    </div>
  );
};

export const ToolGroup = React.memo(ToolGroupImpl);
ToolGroup.displayName = 'ToolGroup';

export default ToolGroup;
