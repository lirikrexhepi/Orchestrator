import { AgentStreamBlock, EditToolBlockData } from './types';

export type FeedSegment =
  | { kind: 'block'; key: string; block: AgentStreamBlock }
  | { kind: 'run'; key: string; blocks: AgentStreamBlock[] }
  | { kind: 'worked'; key: string; blocks: AgentStreamBlock[]; seconds: number | null }
  | { kind: 'working'; key: string; startedAt: number }
  | { kind: 'changes'; key: string; edits: EditToolBlockData[] };

const WORK_TYPES = new Set<AgentStreamBlock['type']>(['thinking', 'tool_group', 'tool_bash', 'tool_search', 'tool_edit']);

export function isWorkBlock(block: AgentStreamBlock): boolean {
  return WORK_TYPES.has(block.type);
}

function isActive(block: AgentStreamBlock): boolean {
  switch (block.type) {
    case 'text':
      return Boolean(block.isStreaming);
    case 'thinking':
      return block.isThinking;
    case 'tool_bash':
    case 'tool_edit':
      return block.status === 'running';
    case 'tool_search':
      return Boolean(block.isSearching);
    case 'tool_group':
      return block.items.some((item) => item.status === 'running');
    default:
      return false;
  }
}

function isPinned(block: AgentStreamBlock): boolean {
  switch (block.type) {
    case 'tool_question':
      return !block.answered;
    case 'approval_request':
      return block.status === 'pending' || !block.status;
    case 'tool_todo':
    case 'tool_plan':
    case 'notice':
    case 'mcp_app':
      return true;
    case 'text':
      return Boolean(block.variant);
    default:
      return false;
  }
}

function pushRuns(segments: FeedSegment[], blocks: AgentStreamBlock[]) {
  let run: AgentStreamBlock[] = [];
  const flush = () => {
    if (run.length === 0) return;
    segments.push({ kind: 'run', key: `run-${run[0].id}`, blocks: run });
    run = [];
  };
  for (const block of blocks) {
    if (isWorkBlock(block)) {
      run.push(block);
      continue;
    }
    flush();
    segments.push({ kind: 'block', key: block.id, block });
  }
  flush();
}

export function normalizePathKey(filePath: string): string {
  let normalized = filePath.trim().replace(/\\/g, '/');
  if (/^\/[A-Za-z]:\//.test(normalized)) normalized = normalized.slice(1);
  return /^[A-Za-z]:\//.test(normalized) ? normalized.toLowerCase() : normalized;
}

export function groupEdits(edits: EditToolBlockData[]): EditToolBlockData[] {
  const byPath = new Map<string, EditToolBlockData>();
  for (const edit of edits) {
    if (edit.status === 'error') continue;
    const key = normalizePathKey(edit.filePath);
    const existing = byPath.get(key);
    if (!existing) {
      byPath.set(key, { ...edit, diffLines: edit.diffLines ? [...edit.diffLines] : [] });
      continue;
    }
    byPath.set(key, {
      ...existing,
      additions: (existing.additions ?? 0) + (edit.additions ?? 0),
      deletions: (existing.deletions ?? 0) + (edit.deletions ?? 0),
      diffLines: [...(existing.diffLines ?? []), ...(edit.diffLines ?? [])],
    });
  }
  return [...byPath.values()];
}

export function buildFeedSegments(
  blocks: AgentStreamBlock[],
  isWorking?: boolean,
): FeedSegment[] {
  const turns: { user?: AgentStreamBlock; body: AgentStreamBlock[] }[] = [];
  for (const block of blocks) {
    if (block.type === 'subagent') continue;
    if (block.type === 'user') {
      turns.push({ user: block, body: [] });
    } else if (turns.length === 0) {
      turns.push({ body: [block] });
    } else {
      turns[turns.length - 1].body.push(block);
    }
  }

  const segments: FeedSegment[] = [];
  turns.forEach((turn, index) => {
    const user = turn.user;
    if (user) segments.push({ kind: 'block', key: user.id, block: user });
    const body = turn.body;
    const last = index === turns.length - 1;
    const live = last && (isWorking ?? body.some(isActive));

    if (live) {
      if (isWorking && user?.type === 'user') {
        segments.push({ kind: 'working', key: `working-${user.id}`, startedAt: user.timestamp ?? Date.now() });
      }
      pushRuns(segments, body);
      return;
    }

    let finalIndex = -1;
    for (let i = body.length - 1; i >= 0; i--) {
      const block = body[i];
      if (block.type === 'text' && !block.variant) {
        finalIndex = i;
        break;
      }
    }
    if (finalIndex < 0) {
      pushRuns(segments, body);
      return;
    }

    const folded: AgentStreamBlock[] = [];
    const pinned: AgentStreamBlock[] = [];
    const edits: EditToolBlockData[] = [];
    for (const block of body.slice(0, finalIndex)) {
      if (isPinned(block)) {
        pinned.push(block);
        continue;
      }
      folded.push(block);
      if (block.type === 'tool_edit') edits.push(block);
    }

    if (folded.length > 0) {
      const final = body[finalIndex];
      const start = user?.type === 'user' ? user.timestamp : undefined;
      const end = final.type === 'text' ? final.timestamp : undefined;
      const seconds = start && end && end >= start ? (end - start) / 1000 : null;
      segments.push({ kind: 'worked', key: `worked-${body[0].id}`, blocks: folded, seconds });
    }
    pinned.forEach((block) => segments.push({ kind: 'block', key: block.id, block }));
    const final = body[finalIndex];
    segments.push({ kind: 'block', key: final.id, block: final });
    const changed = groupEdits(edits);
    if (changed.length > 0) segments.push({ kind: 'changes', key: `changes-${final.id}`, edits: changed });
    pushRuns(segments, body.slice(finalIndex + 1));
  });
  return segments;
}
