import { MaterialIcon } from '../common/icons';
import React, { useState } from 'react';
import { fileIconForPath } from '../common/fileIcon';
import { WorkRow } from './WorkRow';

export interface DiffLine {
  type: 'add' | 'delete' | 'context';
  lineNum: number;
  content: string;
}

export interface EditToolProps {
  filePath: string;
  additions?: number;
  deletions?: number;
  diffLines?: DiffLine[];
  className?: string;
  defaultExpanded?: boolean;
  toolName?: string;
  status?: 'running' | 'completed' | 'error';
}

export function DiffStat({ additions = 0, deletions = 0 }: { additions?: number; deletions?: number }) {
  if (!additions && !deletions) return null;
  return (
    <span className="flex items-center gap-1.5 text-[11.5px] tabular-nums shrink-0">
      {additions > 0 && <span className="text-emerald-400/90">+{additions}</span>}
      {deletions > 0 && <span className="text-rose-400/90">−{deletions}</span>}
    </span>
  );
}

export const DiffView = React.memo(function DiffView({ lines }: { lines: DiffLine[] }) {
  const [copied, setCopied] = useState(false);
  const copy = (event: React.MouseEvent) => {
    event.stopPropagation();
    navigator.clipboard.writeText(
      lines.map((l) => `${l.type === 'add' ? '+' : l.type === 'delete' ? '-' : ' '}${l.content}`).join('\n'),
    );
    setCopied(true);
    setTimeout(() => setCopied(false), 1400);
  };
  return (
    <div className="relative group/out rounded-[10px] bg-current/[0.04] overflow-hidden">
      <button
        type="button"
        onClick={copy}
        title="Copy diff"
        className="absolute top-1.5 right-1.5 z-[1] w-[22px] h-[22px] rounded-[6px] flex items-center justify-center text-current/45 hover:text-current hover:bg-current/[0.08] opacity-0 group-hover/out:opacity-100 transition-all duration-150 cursor-pointer"
      >
        <MaterialIcon name={copied ? 'check' : 'content_copy'} className="text-[13px]"/>
      </button>
      <div className="max-h-[300px] overflow-auto custom-scrollbar py-1.5 select-text">
        {lines.map((line, index) => {
          const add = line.type === 'add';
          const del = line.type === 'delete';
          return (
            <div
              key={index}
              className={`flex min-w-full w-max font-['Geist_Mono',monospace] text-[11px] leading-[18px] ${
                add ? 'bg-emerald-500/[0.11]' : del ? 'bg-rose-500/[0.11]' : ''
              }`}
            >
              <span className="w-9 pr-2.5 text-right text-current/25 select-none shrink-0 tabular-nums">{line.lineNum || ''}</span>
              <span
                className={`w-3 select-none shrink-0 ${add ? 'text-emerald-400/80' : del ? 'text-rose-400/80' : 'text-transparent'}`}
              >
                {add ? '+' : del ? '−' : ' '}
              </span>
              <span
                className={`whitespace-pre pr-4 ${add || del ? 'text-current/85' : 'text-current/55'}`}
              >
                {line.content}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
});

export function editVerb(toolName?: string, status?: string): string {
  const write = /^(write|write_file|write_to_file|writefile|create|create_file)$/i.test(toolName || '');
  if (status === 'running') return write ? 'Writing' : 'Editing';
  if (status === 'error') return write ? 'Failed to write' : 'Failed to edit';
  return write ? 'Wrote' : 'Edited';
}

export function baseName(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).pop() || path;
}

const EditToolImpl: React.FC<EditToolProps> = ({
  filePath,
  additions = 0,
  deletions = 0,
  diffLines = [],
  defaultExpanded = false,
  toolName,
  status,
}) => (
  <WorkRow
    iconNode={<img src={fileIconForPath(filePath)} alt="" draggable={false} className="w-[14px] h-[14px]" />}
    label={editVerb(toolName, status)}
    target={baseName(filePath)}
    running={status === 'running'}
    error={status === 'error'}
    meta={<DiffStat additions={additions} deletions={deletions} />}
    defaultOpen={defaultExpanded}
  >
    {diffLines.length > 0 ? <DiffView lines={diffLines} /> : undefined}
  </WorkRow>
);

export const EditTool = React.memo(EditToolImpl);
EditTool.displayName = 'EditTool';

export default EditTool;
