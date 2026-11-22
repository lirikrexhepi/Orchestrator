import { MaterialIcon } from '../common/icons';
import React, { useState } from 'react';
import { fileIconForPath } from '../common/fileIcon';
import { Chevron, Collapse, OutputPanel, WorkRow } from './WorkRow';
import { ROW_DOT_Y, TimelineDot, useInTimeline } from './Timeline';
import { TextShimmer } from './TextShimmer';

export interface ToolGroupItem {
  id?: string;
  type: 'read' | 'bash' | 'search' | 'edit' | 'write' | 'git' | 'mcp' | 'generic';
  action: string;
  target: string;
  mcpServer?: string;
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
  mcp: 'hub',
  generic: 'build',
};

const FILE_TYPES = new Set<ToolGroupItem['type']>(['read', 'edit', 'write']);

function parseFileTarget(rawTarget: string) {
  const target = rawTarget.trim();
  if (!target) return null;
  const lineMatch = target.match(/^(.*?)(?::|#L)(\d+(?:-\d+)?)$/);
  const pathPart = (lineMatch ? lineMatch[1] : target).trim();
  const lines = lineMatch ? `#L${lineMatch[2]}` : undefined;
  const base = pathPart.split(/[\\/]/).filter(Boolean).pop() || pathPart;
  return { path: pathPart, base, lines };
}

function displayTarget(item: ToolGroupItem): string {
  const target = item.target.trim();
  if (!target) return '';
  if (FILE_TYPES.has(item.type)) {
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
  Delegated: 'Delegating',
  'Checked Orchestrator': 'Checking Orchestrator',
  'Listed threads': 'Listing threads',
  'Read thread': 'Reading thread',
  'Messaged thread': 'Messaging thread',
  'Stopped thread': 'Stopping thread',
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

  if (FILE_TYPES.has(item.type) || item.action === 'Read' || item.action === 'Analyzed') {
    const parsed = parseFileTarget(item.target);
    if (parsed) {
      const verb =
        isAntigravity && (item.type === 'read' || item.action === 'Read' || item.action === 'Analyzed')
          ? running
            ? 'Reading'
            : 'Analyzed'
          : running
            ? PRESENT[item.action] ?? item.action
            : item.action;

      return (
        <WorkRow
          iconNode={
            <img src={fileIconForPath(parsed.path)} alt="" draggable={false} className="w-[14px] h-[14px]" />
          }
          label={verb}
          target={parsed.base}
          meta={
            parsed.lines ? (
              <span className="text-current/40 font-normal shrink-0">{parsed.lines}</span>
            ) : undefined
          }
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
  summary,
  items = [],
  defaultExpanded,
  className = '',
  driver,
}) => {
  const inTimeline = useInTimeline();
  const isAntigravity = driver === 'antigravity' || items.some((item) => item.driver === 'antigravity');
  const anyRunning = items.some((item) => item.status === 'running');
  const runningItem = items.find((item) => item.status === 'running');
  const [open, setOpen] = useState(defaultExpanded ?? false);

  if (items.length < COLLAPSE_AT) {
    return (
      <div className={`flex flex-col ${className}`}>
        {items.map((item, index) => (
          <ToolItemRow key={item.id || index} item={item} isAntigravity={isAntigravity} />
        ))}
      </div>
    );
  }

  const label = summary
    ? summary.charAt(0).toUpperCase() + summary.slice(1)
    : `Used ${items.length} tools`;
  return (
    <div className={`relative flex flex-col font-(family-name:--app-font) ${className}`}>
      {inTimeline && <TimelineDot y={ROW_DOT_Y} tone={anyRunning ? 'running' : 'row'} />}
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        className="group/work flex items-center gap-2 self-start py-[3px] text-[12.5px] tracking-tight leading-[18px] text-current/55 hover:text-current/85 transition-colors duration-150 cursor-pointer"
      >
        {anyRunning ? <TextShimmer duration={1.6}>{label}</TextShimmer> : <span>{label}</span>}
        <Chevron open={open} direction="down" />
      </button>

      {!open && anyRunning && runningItem && (
        <div className="pl-2 pt-0.5">
          <ToolItemRow item={runningItem} isAntigravity={isAntigravity} />
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
