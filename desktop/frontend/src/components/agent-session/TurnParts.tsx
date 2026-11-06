import React, { useEffect, useMemo, useState } from 'react';
import { fileIconForPath } from '../common/fileIcon';
import { Chevron, Collapse, formatElapsed } from './WorkRow';
import { DiffStat, DiffView } from './EditTool';
import { BreakdownHover } from './ChangeBreakdown';
import { EditToolBlockData } from './types';

export const WorkingHeader = React.memo(function WorkingHeader({ startedAt }: { startedAt: number }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);
  return (
    <div className="flex flex-col gap-2 font-(family-name:--app-font)">
      <span className="text-[12.5px] tracking-tight leading-[18px] text-current/45 tabular-nums">
        Working for {formatElapsed((now - startedAt) / 1000)}
      </span>
    </div>
  );
});

export function WorkedFor({ seconds, children }: { seconds: number | null; children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const label = seconds !== null && seconds >= 1 ? `Worked for ${formatElapsed(seconds)}` : 'Worked';
  return (
    <div className="flex flex-col gap-2 font-(family-name:--app-font)">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        className="group/work flex items-center gap-1 self-start text-[12.5px] tracking-tight leading-[18px] text-current/45 hover:text-current/80 transition-colors duration-150 cursor-pointer"
      >
        <span>{label}</span>
        <Chevron open={open} />
      </button>
      <Collapse open={open}>
        <div className="flex flex-col gap-2 pb-1">{children}</div>
      </Collapse>
    </div>
  );
}

function splitPath(path: string): { dir: string; base: string } {
  const parts = path.split(/[\\/]/).filter(Boolean);
  const base = parts.pop() || path;
  return { dir: parts.length ? `${parts.slice(-2).join('/')}/` : '', base };
}

const ChangedFile = React.memo(function ChangedFile({ edit }: { edit: EditToolBlockData }) {
  const [open, setOpen] = useState(false);
  const lines = edit.diffLines ?? [];
  const { dir, base } = splitPath(edit.filePath);
  return (
    <div className="flex flex-col">
      <button
        type="button"
        disabled={lines.length === 0}
        onClick={() => setOpen((value) => !value)}
        title={edit.filePath}
        className="group/work flex items-center gap-2 min-w-0 px-3 py-[7px] text-left text-[12.5px] tracking-tight leading-[18px] enabled:hover:bg-current/[0.035] transition-colors duration-150 enabled:cursor-pointer"
      >
        <img src={fileIconForPath(edit.filePath)} alt="" draggable={false} className="w-[14px] h-[14px] shrink-0" />
        <span className="truncate min-w-0 flex-1">
          {dir && <span className="text-current/35">{dir}</span>}
          <span className="text-current/85">{base}</span>
        </span>
        <DiffStat additions={edit.additions} deletions={edit.deletions} />
        {lines.length > 0 && <Chevron open={open} />}
      </button>
      {lines.length > 0 && (
        <Collapse open={open}>
          <div className="px-2 pb-2">
            <DiffView lines={lines} />
          </div>
        </Collapse>
      )}
    </div>
  );
});

export const ChangesCard = React.memo(function ChangesCard({ edits }: { edits: EditToolBlockData[] }) {
  const additions = edits.reduce((sum, edit) => sum + (edit.additions ?? 0), 0);
  const deletions = edits.reduce((sum, edit) => sum + (edit.deletions ?? 0), 0);
  const statFiles = useMemo(
    () => edits.map((edit) => ({ path: edit.filePath, additions: edit.additions ?? 0, deletions: edit.deletions ?? 0 })),
    [edits],
  );
  return (
    <div className="rounded-[14px] border border-current/[0.08] overflow-hidden font-(family-name:--app-font)">
      <div className="flex items-center gap-2 px-3 py-2 text-[12px] tracking-tight text-current/50">
        <span>
          {edits.length} {edits.length === 1 ? 'file' : 'files'} changed
        </span>
        <BreakdownHover files={statFiles}>
          <DiffStat additions={additions} deletions={deletions} />
        </BreakdownHover>
      </div>
      <div className="flex flex-col border-t border-current/[0.06] divide-y divide-current/[0.06]">
        {edits.map((edit) => (
          <ChangedFile key={edit.filePath} edit={edit} />
        ))}
      </div>
    </div>
  );
});
