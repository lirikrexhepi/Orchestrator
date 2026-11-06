import { domain } from '../../../wailsjs/go/models';
import { AgentStreamBlock, EditToolBlockData, SubagentBlockData } from './types';
import { DiffLine } from './EditTool';

export interface ThreadEditGroup {
  toolName?: string;
  status?: EditToolBlockData['status'];
  additions: number;
  deletions: number;
  lines: DiffLine[];
}

export interface ThreadFileChange {
  key: string;
  path: string;
  additions: number;
  deletions: number;
  edits: number;
  groups: ThreadEditGroup[];
}

const EDIT_TOOL_NAME = /^(edit|multiedit|write|notebookedit|patch|apply_patch|writefile|editfile|replace_file_content|write_to_file|multi_replace_file_content|filechange)$/i;

export function isEditToolName(name?: string): boolean {
  return Boolean(name) && EDIT_TOOL_NAME.test(name!);
}

function normalizePath(path: string): string {
  return path.replace(/\\/g, '/').replace(/:\d+(?:-\d+)?$/, '');
}

export function collectThreadChanges(blocks: AgentStreamBlock[]): ThreadFileChange[] {
  const byKey = new Map<string, ThreadFileChange>();
  const order: string[] = [];

  const add = (rawPath: string, group: ThreadEditGroup) => {
    const path = normalizePath(rawPath.trim());
    if (!path || /[/\\]$/.test(path)) return;
    const key = path.toLowerCase();
    let entry = byKey.get(key);
    if (!entry) {
      entry = { key, path, additions: 0, deletions: 0, edits: 0, groups: [] };
      byKey.set(key, entry);
      order.push(key);
    }
    entry.path = path;
    entry.additions += Math.max(0, group.additions);
    entry.deletions += Math.max(0, group.deletions);
    entry.edits += 1;
    if (group.lines.length > 0 || entry.groups.every((existing) => existing.lines.length === 0)) {
      entry.groups.push(group);
    } else {
      entry.groups.unshift(group);
    }
  };

  for (const block of blocks) {
    if (block.type === 'tool_edit') {
      if (block.status === 'error') continue;
      add(block.filePath, {
        toolName: block.toolName,
        status: block.status,
        additions: block.additions ?? 0,
        deletions: block.deletions ?? 0,
        lines: block.diffLines ?? [],
      });
    } else if (block.type === 'subagent') {
      const agent = block as SubagentBlockData;
      for (const entry of agent.entries) {
        if (entry.kind !== 'tool' || !isEditToolName(entry.name)) continue;
        if (entry.status === 'error') continue;
        add(entry.filePath || entry.target, {
          status: entry.status,
          additions: entry.additions ?? 0,
          deletions: entry.deletions ?? 0,
          lines: entry.diffLines ?? [],
        });
      }
    }
  }

  return order.map((key) => byKey.get(key)!);
}

export interface SubagentEditTotals {
  additions: number;
  deletions: number;
  files: number;
}

export function subagentTotals(agent: SubagentBlockData): SubagentEditTotals {
  let additions = 0;
  let deletions = 0;
  const files = new Set<string>();
  for (const entry of agent.entries) {
    if (entry.kind !== 'tool' || !isEditToolName(entry.name)) continue;
    if (entry.status === 'error') continue;
    additions += entry.additions ?? 0;
    deletions += entry.deletions ?? 0;
    const path = (entry.filePath || entry.target || '').trim();
    if (path) files.add(path.replace(/\\/g, '/').toLowerCase());
  }
  return { additions, deletions, files: files.size };
}

export interface ThreadTotals {
  count: number;
  additions: number;
  deletions: number;
}

export function threadTotals(changes: ThreadFileChange[]): ThreadTotals {
  let additions = 0;
  let deletions = 0;
  for (const change of changes) {
    additions += change.additions;
    deletions += change.deletions;
  }
  return { count: changes.length, additions, deletions };
}

export function changeDiffFile(change: ThreadFileChange): domain.DiffFile | null {
  const hunks = change.groups
    .filter((group) => group.lines.length > 0)
    .map((group) =>
      new domain.DiffHunk({
        header: `${group.toolName || 'Edit'} · +${group.additions} −${group.deletions}`,
        lines: group.lines.map((line) =>
          new domain.DiffLine({
            kind: line.type === 'add' ? 'added' : line.type === 'delete' ? 'removed' : 'context',
            old: line.type === 'delete' ? line.lineNum : undefined,
            new: line.type === 'add' ? line.lineNum : undefined,
            content: line.content,
          }),
        ),
      }),
    );
  if (hunks.length === 0) return null;
  return new domain.DiffFile({
    path: change.path,
    hunks,
    insertions: change.additions,
    deletions: change.deletions,
    binary: false,
    truncated: false,
  });
}

export function changeDiffText(change: ThreadFileChange): string {
  const blocks = change.groups
    .filter((group) => group.lines.length > 0)
    .map((group) =>
      group.lines
        .map((line) => `${line.type === 'add' ? '+' : line.type === 'delete' ? '-' : ' '}${line.content}`)
        .join('\n'),
    );
  return blocks.join('\n\n');
}
