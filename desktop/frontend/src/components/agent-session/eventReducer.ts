import { domain } from '../../../wailsjs/go/models';
import { AgentStreamBlock, UserMessageFile } from './types';
import { ToolGroupItem } from './ToolGroup';
import { DiffLine } from './EditTool';
import { isSubagentEvent, trackSubagents } from './subagents';
import { chatRefTitle, isChatRefPath } from '../common/chatDrag';

export type RuntimeEvent = domain.RuntimeEvent;

const TEXT_TOOLS = new Set([
  'Read', 'Glob', 'Grep', 'WebFetch', 'WebSearch', 'view_file', 'list_dir', 'grep_search', 'find_by_name', 'read_url_content',
  'read', 'glob', 'grep', 'webfetch', 'websearch', 'cat', 'readfile', 'read_file',
]);
const BASH_TOOLS = new Set([
  'Bash', 'PowerShell', 'run_command', 'commandExecution',
  'bash', 'powershell', 'terminal', 'cmd', 'command', 'sh',
]);
const EDIT_TOOLS = new Set([
  'Edit', 'Write', 'NotebookEdit', 'replace_file_content', 'write_to_file', 'multi_replace_file_content', 'fileChange',
  'edit', 'write', 'notebookedit', 'patch', 'writefile', 'editfile',
]);
const SEARCH_TOOLS = new Set([
  'Glob', 'Grep', 'grep_search', 'find_by_name',
  'glob', 'grep', 'search', 'find',
]);
const TODO_TOOLS = new Set(['TodoWrite', 'manage_task', 'todowrite', 'todo', 'task']);
const QUESTION_TOOLS = new Set([
  'ask_question', 'AskUserQuestion', 'askquestion', 'question',
]);

const COMMAND_KEYS = ['command', 'CommandLine', 'cmd', 'script', 'input', 'args'];
const PATH_KEYS = [
  'filePath',
  'file_path',
  'path',
  'AbsolutePath',
  'DirectoryPath',
  'TargetFile',
  'FilePath',
  'NotebookPath',
  'file',
  'filename',
  'fileName',
  'uri',
  'target',
  'dest',
  'src',
  'url',
  'Url',
];
const QUERY_KEYS = ['pattern', 'query', 'Query', 'Pattern', 'search_term', 'searchTerm', 'regex', 'glob'];
const ROOT_KEYS = ['SearchPath', 'SearchDirectory', 'search_path', 'directory', 'dir', 'cwd', 'Cwd'];
const ACTION_KEYS = ['Action', 'action'];

function field(input: Record<string, unknown>, keys: string[]): string {
  for (const key of keys) {
    const value = input[key];
    if (typeof value === 'string' && value) return value;
  }
  return '';
}

function findLastIndex<T>(items: readonly T[], predicate: (item: T) => boolean | undefined): number {
  for (let index = items.length - 1; index >= 0; index--) {
    if (predicate(items[index])) return index;
  }
  return -1;
}

function extractTarget(name: string, input: Record<string, unknown>): string {
  const filePath = field(input, PATH_KEYS);
  if (filePath) {
    const start = input.StartLine ?? input.start_line ?? input.startLine ?? input.line;
    const end = input.EndLine ?? input.end_line ?? input.endLine;
    if (start !== undefined && start !== null && String(start).trim() !== '') {
      const s = String(start).trim();
      const e = end !== undefined && end !== null ? String(end).trim() : '';
      if (e && e !== s) {
        return `${filePath}:${s}-${e}`;
      }
      return `${filePath}:${s}`;
    }
    return filePath;
  }

  const direct =
    field(input, COMMAND_KEYS) ||
    field(input, QUERY_KEYS) ||
    field(input, ROOT_KEYS) ||
    field(input, ACTION_KEYS);
  if (direct) return direct;

  const title = typeof input.title === 'string' ? input.title.trim() : '';
  if (title) {
    const stripped = title
      .replace(new RegExp(`^(?:ran\\s+|run\\s+|reading\\s+|read(?:ing)?\\s+(?:file\\s+)?|glob\\s+|grep\\s+|bash\\s*:?\\s*|view_file\\s*)`, 'i'), '')
      .trim();
    if (stripped) return stripped;
    return title;
  }

  const summary = typeof input.toolSummary === 'string' ? input.toolSummary.trim() : '';
  if (summary) return summary;
  const action = typeof input.toolAction === 'string' ? input.toolAction.trim() : '';
  if (action) return action;

  return '';
}

function determineAction(name: string, cmd?: string): { type: ToolGroupItem['type']; action: string } {
  const lower = name.toLowerCase();
  if (isGitCommand(name, cmd)) {
    return { type: 'git', action: 'Git' };
  }
  if (lower === 'read' || lower === 'view_file' || lower === 'read_file' || lower === 'readfile' || lower === 'cat') {
    return { type: 'read', action: 'Read' };
  }
  if (lower === 'glob' || lower === 'list_dir' || lower === 'find_by_name' || lower === 'listfiles') {
    return { type: 'search', action: 'Glob' };
  }
  if (lower === 'grep' || lower === 'grep_search' || lower === 'search') {
    return { type: 'search', action: 'Grep' };
  }
  if (lower === 'bash' || lower === 'powershell' || lower === 'run_command' || lower === 'commandexecution' || lower === 'terminal' || lower === 'cmd' || lower === 'sh') {
    return { type: 'bash', action: isInspectionCommand(cmd) ? 'Inspect' : 'Ran' };
  }
  if (lower === 'edit' || lower === 'replace_file_content' || lower === 'multi_replace_file_content' || lower === 'filechange' || lower === 'patch') {
    return { type: 'edit', action: 'Edit' };
  }
  if (lower === 'write' || lower === 'write_to_file' || lower === 'writefile') {
    return { type: 'write', action: 'Write' };
  }
  if (lower === 'webfetch' || lower === 'websearch' || lower === 'read_url_content') {
    return { type: 'search', action: 'Fetch' };
  }
  if (lower.includes('todo') || lower.includes('task')) {
    return { type: 'generic', action: 'Task' };
  }
  return { type: 'generic', action: name.charAt(0).toUpperCase() + name.slice(1) };
}

/**
 * Folds one runtime event into the block list.
 *
 * Streaming text arrives as many small deltas, so consecutive text/thinking
 * events are merged into the trailing block rather than appended, which keeps
 * the list short and lets React reconcile a single node per message.
 */
export function reduceEvent(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const tracked = trackSubagents(blocks, event);
  return isSubagentEvent(event) ? tracked : reduceMain(tracked, event);
}

function reduceMain(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  switch (event.kind) {
    case 'user.message':
      return appendUserMessage(blocks, event);
    case 'agent.message':
      return appendText(blocks, event, 'text');
    case 'agent.thought':
      return appendText(blocks, event, 'thinking');
    case 'tool.call':
    case 'tool.result':
      return upsertTool(closeStreaming(blocks), event);
    case 'diagnostic':
      return appendDiagnostic(blocks, event);
    case 'plan':
      return upsertPlan(blocks, event);
    case 'turn.started':
      // No placeholder block: the surrounding panel renders the waiting
      // indicator, so emitting one here would show two spinners at once.
      return blocks;
    case 'approval.request':
      return upsertApproval(closeStreaming(blocks), event);
    case 'approval.resolved':
      return resolveApproval(blocks, event);
    case 'question.asked':
      return upsertQuestion(closeStreaming(blocks), event);
    case 'question.answered':
      return resolveQuestion(blocks, event);
    case 'turn.completed':
    case 'turn.failed':
      return settle(blocks, event);
    case 'notice':
      return appendNotice(blocks, event);
    default:
      return blocks;
  }
}

function appendNotice(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const label = (event.text ?? '').trim();
  if (!label) return blocks;
  const icon = (event as unknown as { icon?: string }).icon || undefined;
  const last = blocks[blocks.length - 1];
  if (last?.type === 'notice' && last.label === label) {
    if (icon && !last.icon) {
      const updated = [...blocks];
      updated[updated.length - 1] = { ...last, icon };
      return updated;
    }
    return blocks;
  }
  return [...blocks, { type: 'notice', id: `notice-${event.seq || Date.now()}`, label, icon }];
}

function upsertApproval(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const req = event.approval;
  if (!req) return blocks;
  blocks = closeThinking(blocks);

  const reqID = (req as any).requestId || (req as any).requestID || '';
  const id = `approval-${reqID || event.seq}`;
  const block: AgentStreamBlock = {
    type: 'approval_request',
    id,
    requestID: reqID,
    title: req.title || 'Permission Request',
    detail: req.detail,
    options: (req.options ?? []).map((o) => ({
      id: o.id,
      name: o.name,
      kind: o.kind,
    })),
    status: 'pending',
  };

  const existingIdx = findLastIndex(blocks,
    (b) => b.type === 'approval_request' && (b.id === id || b.requestID === reqID),
  );
  if (existingIdx >= 0) {
    const next = [...blocks];
    next[existingIdx] = { ...(next[existingIdx] as any), ...block };
    return next;
  }
  return [...blocks, block];
}

function resolveApproval(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const req = event.approval;
  const reqID = (req as any)?.requestId || (req as any)?.requestID;
  const decision = event.text;

  return blocks.map((b) => {
    if (b.type === 'approval_request' && (!reqID || b.requestID === reqID)) {
      const isDenied = decision === 'reject' || decision === 'deny' || decision === 'cancel';
      return {
        ...b,
        status: isDenied ? 'denied' : 'resolved',
        decision: decision || b.decision,
      };
    }
    return b;
  });
}

function questionOptionLabel(opt: any): string {
  if (typeof opt === 'string') return opt;
  if (opt && typeof opt === 'object') return String(opt.label || opt.value || '');
  return String(opt);
}

function questionOptionDescription(opt: any): string | undefined {
  if (opt && typeof opt === 'object' && typeof opt.description === 'string' && opt.description.trim()) {
    return opt.description.trim();
  }
  return undefined;
}

function toQuestionItems(raw: any[]): Array<{ question: string; options: any[] }> {
  return (raw ?? [])
    .map((entry: any) => {
      const header = typeof entry?.header === 'string' ? entry.header.trim() : '';
      const body = typeof entry?.question === 'string' ? entry.question.trim() : '';
      const text = body && header && body !== header ? `${header}: ${body}` : body || header;
      return { question: text, options: Array.isArray(entry?.options) ? entry.options : [] };
    })
    // A tool declaration with choices but no actual prompt is not a question.
    // Rendering one produces the misleading “The agent has a question” card.
    .filter((item) => Boolean(item.question));
}

function buildQuestionOptions(
  rawOptions: any[],
): Array<{ key: string; label: string; description?: string; isCustomInput?: boolean }> {
  const options = (rawOptions ?? []).map((opt: any, idx: number) => ({
    key: String.fromCharCode(65 + idx),
    label: questionOptionLabel(opt),
    description: questionOptionDescription(opt),
  }));
  options.push({ key: String.fromCharCode(65 + options.length), label: 'Type your answer', isCustomInput: true } as any);
  return options;
}

function upsertQuestion(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const q = (event as any).question;
  if (!q) return blocks;

  const requestID: string = q.requestId || q.requestID || '';
  const id = `question-${requestID || event.seq}`;

  const rawItems = toQuestionItems(Array.isArray(q.questions) ? q.questions : []);
  if (rawItems.length === 0) return blocks;

  const items = rawItems.map((it) => ({ question: it.question, options: buildQuestionOptions(it.options) }));
  const first = items[0];

  const block: AgentStreamBlock = {
    type: 'tool_question',
    id,
    questionNumber: 1,
    totalQuestions: items.length || 1,
    question: first.question,
    options: first.options,
    items,
  };

  const existingIdx = findLastIndex(blocks,
    (b) => b.id === id || (b.type === 'tool_question' && Boolean(requestID) && b.id.includes(requestID)),
  );
  if (existingIdx >= 0) {
    const next = [...blocks];
    next[existingIdx] = { ...(next[existingIdx] as any), ...block };
    return next;
  }
  return [...blocks, block];
}

function resolveQuestion(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const answer = event.text;
  const reqID = (event as any).question?.requestId || (event as any).question?.requestID || '';
  const isSkip = !answer || answer.trim() === '';

  // With a request id only that card resolves; without one, only the most
  // recent open question does — never every question in the feed.
  let target = -1;
  for (let i = blocks.length - 1; i >= 0; i--) {
    const b = blocks[i];
    if (b.type !== 'tool_question') continue;
    if (reqID ? b.id === `question-${reqID}` || b.id.includes(reqID) : !b.answered) {
      target = i;
      break;
    }
  }
  if (target < 0) return blocks;
  const next = [...blocks];
  const b = next[target] as Extract<AgentStreamBlock, { type: 'tool_question' }>;
  next[target] = {
    ...b,
    answered: true,
    selectedAnswer: isSkip ? 'Skipped' : answer || b.selectedAnswer || 'Answered',
  };
  return next;
}

export function userBlock(
  text: string,
  id: string,
  files: (string | UserMessageFile | domain.FileRef)[] = [],
  timestamp?: number,
): AgentStreamBlock {
  const normalizedFiles: UserMessageFile[] = (files || []).map((f) => {
    if (typeof f === 'string') {
      if (isChatRefPath(f)) return { path: f, name: chatRefTitle(f), mime: 'application/x-orchestrator-chat' };
      const parts = f.split(/[\\/]/);
      return { path: f, name: parts[parts.length - 1] || f };
    }
    const path = f.path || '';
    const name = isChatRefPath(path) ? chatRefTitle(path) : (f as any).name || path.split(/[\\/]/).pop() || path;
    return { path, name, mime: f.mime };
  });

  return {
    type: 'user',
    id,
    content: text,
    files: normalizedFiles.length > 0 ? normalizedFiles : undefined,
    timestamp: timestamp || Date.now(),
    pending: true,
  };
}

/**
 * Creates a user block from a backend user.message event. Deduplicates against
 * optimistic client-side user blocks that were already added by the send()
 * callback, so the same message does not appear twice.
 */
function appendUserMessage(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const text = event.text ?? '';
  const atTime = event.at || Date.now();
  const rawFiles = ((event as any).files || []) as (domain.FileRef | UserMessageFile)[];
  const eventFiles: UserMessageFile[] = rawFiles.map((f) => {
    const path = f.path || '';
    const name = isChatRefPath(path) ? chatRefTitle(path) : (f as any).name || path.split(/[\\/]/).pop() || path;
    return { path, name, mime: f.mime };
  });

  // Helper to normalize content for matching by stripping legacy attachment notes
  const normalize = (s: string) => s.replace(/(?:\r?\n)*📎[^\r\n]*/g, '').trim();
  const cleanEventText = normalize(text);

  if (!cleanEventText && eventFiles.length === 0 && !text) {
    return blocks;
  }

  // The client adds a user block optimistically on send (marked pending).
  // Only such a pending block may absorb the backend echo; a replayed
  // transcript has none, so repeated messages ("yes", "continue") all stay.
  for (let i = blocks.length - 1; i >= Math.max(0, blocks.length - 16); i--) {
    const b = blocks[i];
    if (b.type !== 'user' || !b.pending) continue;
    const isSameText = normalize(b.content) === cleanEventText;
    const isSameTurn = Boolean(event.turnId && b.turnId === event.turnId);
    if (isSameText || isSameTurn) {
      const updated = [...blocks];
      updated[i] = {
        ...b,
        pending: false,
        turnId: event.turnId || b.turnId,
        files: b.files && b.files.length > 0 ? b.files : eventFiles.length > 0 ? eventFiles : undefined,
        timestamp: b.timestamp || atTime,
      };
      return updated;
    }
  }

  const id = `user-${event.turnId || event.seq}`;
  return [
    ...blocks,
    {
      type: 'user',
      id,
      content: text,
      files: eventFiles.length > 0 ? eventFiles : undefined,
      timestamp: atTime,
      turnId: event.turnId,
    },
  ];
}

function itemOf(event: RuntimeEvent): string {
  return (event as { itemId?: string }).itemId || '';
}

/**
 * Streams text into the block for the event's content item. With an item id
 * (all current adapters send one) a delta only ever extends that item's block
 * and a new item always starts a new block, so consecutive replies, parallel
 * blocks and diagnostics never overwrite one another. Events without an id
 * fall back to extending the streaming block of the same turn.
 */
function appendText(
  blocks: AgentStreamBlock[],
  event: RuntimeEvent,
  kind: 'text' | 'thinking',
): AgentStreamBlock[] {
  const text = event.text ?? '';
  if (!text) return blocks;
  const itemId = itemOf(event);
  const turnId = event.turnId || '';
  const atTime = event.at || Date.now();

  // Find the block this event continues.
  let index = -1;
  if (itemId) {
    for (let i = blocks.length - 1; i >= Math.max(0, blocks.length - 12); i--) {
      const b = blocks[i];
      if ((b.type === 'text' || b.type === 'thinking') && b.type === kind && b.itemId === itemId) {
        index = i;
        break;
      }
      if (b.type === 'user') break;
    }
  } else {
    const last = blocks[blocks.length - 1];
    if (
      last &&
      last.type === kind &&
      !last.itemId &&
      (last.turnId || '') === turnId &&
      (kind === 'text' ? (last as { isStreaming?: boolean }).isStreaming && !(last as { variant?: string }).variant : (last as { isThinking?: boolean }).isThinking)
    ) {
      index = blocks.length - 1;
    }
  }

  if (index >= 0) {
    const existing = blocks[index];
    const next = [...blocks];
    if (existing.type === 'text') {
      next[index] = {
        ...existing,
        content: event.delta ? existing.content + text : text,
        isStreaming: true,
        timestamp: existing.timestamp || atTime,
        driver: existing.driver || event.driver,
        model: existing.model || (event as any).model,
      };
    } else if (existing.type === 'thinking') {
      next[index] = { ...existing, thoughtText: event.delta ? existing.thoughtText + text : text };
    }
    return next;
  }

  const settled = closeThinking(closeStreaming(blocks));
  const id = `${kind}-${itemId || event.seq}`;
  return [
    ...settled,
    kind === 'text'
      ? { type: 'text', id, content: text, isStreaming: true, timestamp: atTime, itemId: itemId || undefined, turnId, driver: event.driver, model: (event as any).model }
      : { type: 'thinking', id, isThinking: true, thoughtText: text, itemId: itemId || undefined, turnId },
  ];
}

/** Marks open thinking blocks finished once the agent moves on. */
function closeThinking(blocks: AgentStreamBlock[]): AgentStreamBlock[] {
  let next: AgentStreamBlock[] | null = null;
  for (let index = 0; index < blocks.length; index++) {
    const b = blocks[index];
    if (b.type === 'thinking' && b.isThinking) {
      next ??= blocks.slice();
      next[index] = { ...b, isThinking: false };
    }
  }
  return next ?? blocks;
}

/** Tools that always render as their own card, never folded into a group. */
function isStandaloneTool(name: string, tool: domain.ToolCall): boolean {
  return (
    EDIT_TOOLS.has(name) ||
    tool.kind === 'fileChange' ||
    TODO_TOOLS.has(name) ||
    isQuestionTool(name) ||
    Boolean(tool.diffs?.length)
  );
}

function upsertTool(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const tool = event.tool;
  if (!tool) return blocks;
  blocks = closeThinking(blocks);

  const id = `tool-${tool.id}`;
  const index = findLastIndex(blocks,
    (candidate) =>
      candidate.id === id ||
      (candidate.type === 'tool_group' && candidate.memberIds?.includes(tool.id)),
  );

  // A result event carries only id/status/output — the tool name and its input
  // arrived with the earlier call — so it is merged into the existing block
  // rather than rebuilt, which would drop the command being displayed.
  if (index >= 0) {
    const merged = [...blocks];
    merged[index] = mergeToolBlock(blocks[index], tool, Boolean(event.delta));
    return merged;
  }

  const name = tool.name || tool.kind || '';
  // A result whose call never rendered (dropped, or shown through another
  // surface such as a question card) must not become a nameless ghost row.
  if (event.kind === 'tool.result' && !tool.name) return blocks;

  const input = asRecord(tool.input);

  // Todo lists: one live list, moved to where the agent currently is.
  if (TODO_TOOLS.has(name)) {
    const todos = toTodos(input);
    if (todos.length > 0) {
      return [
        ...blocks.filter((b) => b.type !== 'tool_todo'),
        { type: 'tool_todo', id: 'session-tasklist', title: 'Tasklist', todos },
      ];
    }
  }

  // Question tools are interactive prompts that must never be collapsed into a tool group
  if (isQuestionTool(name)) {
    const qBlock = toQuestionBlock(tool);
    if (!qBlock) return blocks;
    const existingQ = findLastIndex(blocks,
      (b) => b.type === 'tool_question' && (b.id === qBlock.id || b.id.includes(tool.id)),
    );
    if (existingQ >= 0) {
      const next = [...blocks];
      next[existingQ] = { ...(next[existingQ] as any), ...qBlock };
      return next;
    }
    return [...blocks, qBlock];
  }

  if (isStandaloneTool(name, tool)) {
    const block = toToolBlock(tool);
    return block ? [...blocks, block] : blocks;
  }

  // Consecutive read/bash/search activity collapses into one group so
  // repetitive commands don't bury the feed. Every member keeps its id so a
  // result that arrives later still finds its row.
  const newItem = toGroupItem(name || 'tool', tool, input);
  const last = blocks[blocks.length - 1];
  if (last?.type === 'tool_group') {
    const items = [...last.items, newItem];
    const next: AgentStreamBlock = {
      ...last,
      title: groupTitle(items),
      items,
      summary: groupSummary(items),
      memberIds: [...(last.memberIds ?? []), tool.id],
    };
    return [...blocks.slice(0, -1), next];
  }

  if (last?.type === 'tool_bash' || last?.type === 'tool_search') {
    const prevId = last.id.replace(/^tool-/, '');
    const prevItem: ToolGroupItem =
      last.type === 'tool_bash'
        ? {
            id: prevId,
            type: isGitCommand('bash', last.command) ? 'git' : 'bash',
            action: isGitCommand('bash', last.command) ? 'Git' : isInspectionCommand(last.command) ? 'Inspect' : 'Ran',
            target: last.command && !/^(bash|powershell|cmd)$/i.test(last.command) ? last.command : '',
            details: last.output,
            status: last.status,
          }
        : {
            id: prevId,
            type: 'search',
            action: last.query ? 'Search' : 'Glob',
            target: last.query || (last.files?.length ? `${last.files.length} files` : ''),
            details: last.files?.join('\n'),
            status: last.isSearching ? 'running' : 'completed',
          };
    const items = [prevItem, newItem];
    const group: AgentStreamBlock = {
      type: 'tool_group',
      id: `group-${prevId}`,
      title: groupTitle(items),
      summary: groupSummary(items),
      items,
      memberIds: [prevId, tool.id],
    };
    return [...blocks.slice(0, -1), group];
  }

  const block = toToolBlock(tool);
  return block ? [...blocks, block] : blocks;
}

function isGitCommand(name: string, cmd?: string): boolean {
  if (name.toLowerCase() === 'git') return true;
  if (!cmd) return false;
  return /^\s*git(\.exe)?(\s+|$)/i.test(cmd);
}

function isInspectionCommand(cmd?: string): boolean {
  if (!cmd) return false;
  return /^\s*(get|cat|type|head|tail|curl|fetch|dir|ls|find|grep|rg)(\.exe)?(\s+|$)/i.test(cmd);
}

// Names the group after the tool used when it is homogeneous, and generically
// once it mixes, so the header stays informative either way.
function groupTitle(items: ToolGroupItem[]): string {
  if (items.length === 0) return 'Tools';
  if (items.every((item) => item.type === 'git')) return 'Git';
  if (items.every((item) => item.type === 'read')) return 'Read';
  if (items.every((item) => item.type === 'search')) return 'Search';
  if (items.every((item) => item.type === 'bash')) return 'Commands';
  return 'Worked';
}

function groupSummary(items: ToolGroupItem[]): string {
  if (items.length === 0) return '';
  if (items.length === 1) return items[0].target || '1 action';

  const readCount = items.filter((i) => i.type === 'read').length;
  const searchCount = items.filter((i) => i.type === 'search').length;
  const cmdCount = items.filter((i) => i.type === 'bash' || i.type === 'git').length;
  const editCount = items.filter((i) => i.type === 'edit' || i.type === 'write').length;

  const parts: string[] = [];
  if (readCount > 0) parts.push(`${readCount} ${readCount === 1 ? 'read' : 'reads'}`);
  if (searchCount > 0) parts.push(`${searchCount} ${searchCount === 1 ? 'search' : 'searches'}`);
  if (cmdCount > 0) parts.push(`${cmdCount} ${cmdCount === 1 ? 'command' : 'commands'}`);
  if (editCount > 0) parts.push(`${editCount} ${editCount === 1 ? 'edit' : 'edits'}`);

  return parts.length > 0 ? parts.join(', ') : `${items.length} actions`;
}

function mergeToolBlock(existing: AgentStreamBlock, tool: domain.ToolCall, delta = false): AgentStreamBlock {
  const status = toStatus(tool.status);
  const output = (previous?: string) =>
    delta && tool.output ? (previous || '') + tool.output : tool.output || previous;

  switch (existing.type) {
    case 'tool_bash': {
      const nextInput = asRecord(tool.input);
      const cmd = field(nextInput, COMMAND_KEYS) || (typeof nextInput.title === 'string' ? nextInput.title : '');
      return {
        ...existing,
        command: cmd && (existing.command === 'bash' || existing.command === 'powershell' || existing.command === existing.id) ? cmd : existing.command,
        status,
        output: output(existing.output),
        exitCode: existing.exitCode,
      };
    }
    case 'tool_search': {
      const nextInput = asRecord(tool.input);
      const query = extractTarget(tool.name || 'search', nextInput) || existing.query;
      return {
        ...existing,
        query,
        files: tool.output ? toFileList(tool.output) : existing.files,
        isSearching: status === 'running',
      };
    }
    case 'tool_edit': {
      const next = { ...existing, status };
      if (tool.diffs?.length || tool.input) {
        const computed = editBlockFields(tool.name || existing.toolName || '', asRecord(tool.input), tool.diffs);
        if (computed.diffLines.length > 0) Object.assign(next, computed);
      }
      return next;
    }
    case 'tool_group': {
      const items = existing.items.map((item) => {
        if (item.id !== tool.id) return item;
        const nextInput = asRecord(tool.input);
        const extracted = extractTarget(tool.name || item.action, nextInput);
        const nextTarget =
          extracted ||
          (item.target &&
          item.target.toLowerCase() !== item.action.toLowerCase() &&
          item.target.toLowerCase() !== (tool.name || '').toLowerCase()
            ? item.target
            : '');
        return {
          ...item,
          status,
          target: nextTarget,
          details: output(item.details),
        };
      });
      const summary = groupSummary(items);
      return {
        ...existing,
        items,
        title: groupTitle(items),
        summary: items.length > 1 ? summary : (items[0]?.target || summary),
      };
    }
    default:
      return existing;
  }
}

function toToolBlock(tool: domain.ToolCall): AgentStreamBlock | null {
  const id = `tool-${tool.id}`;
  const name = tool.name || tool.kind || 'tool';
  const input = (tool.input ?? {}) as Record<string, unknown>;
  const cmd = field(input, COMMAND_KEYS);

  // Group standalone git commands immediately into a clean Git tool group
  if (isGitCommand(name, cmd)) {
    const item = toGroupItem(name, tool, input);
    return {
      type: 'tool_group',
      id,
      title: 'Git',
      summary: cmd || 'git',
      items: [item],
      memberIds: [tool.id],
    };
  }

  if (BASH_TOOLS.has(name) || tool.kind === 'commandExecution') {
    return {
      type: 'tool_bash',
      id,
      command: cmd || name,
      output: tool.output,
      status: toStatus(tool.status),
    };
  }

  if (EDIT_TOOLS.has(name) || tool.kind === 'fileChange' || tool.diffs?.length) {
    const computed = editBlockFields(name, input, tool.diffs);
    return {
      type: 'tool_edit',
      id,
      filePath: computed.filePath || extractTarget(name, input) || name,
      additions: computed.additions,
      deletions: computed.deletions,
      diffLines: computed.diffLines,
      status: toStatus(tool.status),
      toolName: name,
    };
  }

  if (SEARCH_TOOLS.has(name)) {
    return {
      type: 'tool_search',
      id,
      files: toFileList(tool.output),
      query: extractTarget(name, input),
      isSearching: tool.status === 'in_progress' || tool.status === 'pending',
    };
  }

  if (TODO_TOOLS.has(name)) {
    const todos = toTodos(input);
    if (todos.length > 0) return { type: 'tool_todo', id, todos };
  }

  if (isQuestionTool(name)) {
    return toQuestionBlock(tool);
  }

  const item = toGroupItem(name, tool, input);
  return {
    type: 'tool_group',
    id,
    title: item.action,
    summary: item.target || summarize(tool),
    items: [item],
    memberIds: [tool.id],
  };
}

function isQuestionTool(name?: string): boolean {
  if (!name) return false;
  const lower = name.toLowerCase();
  return (
    QUESTION_TOOLS.has(name) ||
    QUESTION_TOOLS.has(lower) ||
    lower === 'question' ||
    lower.includes('ask_question') ||
    lower.includes('askuserquestion')
  );
}

function toQuestionBlock(tool: domain.ToolCall): AgentStreamBlock | null {
  const id = `question-${tool.id}`;
  const input = asRecord(tool.input);

  let items: Array<{ question?: string; header?: string; options?: any[]; isMultiSelect?: boolean }> = [];
  if (Array.isArray(input.questions)) {
    items = input.questions as any;
  } else if (typeof input.question === 'string') {
    const opts = Array.isArray(input.options) ? (input.options as any[]) : [];
    items = [{ question: input.question, options: opts, isMultiSelect: !!input.is_multi_select || !!input.multiple }];
  } else if (typeof input.prompt === 'string') {
    items = [{ question: input.prompt, options: Array.isArray(input.options) ? (input.options as any[]) : [] }];
  } else if (typeof input.header === 'string') {
    items = [{ question: input.header, options: Array.isArray(input.options) ? (input.options as any[]) : [] }];
  }

  const rawItems = toQuestionItems(items.length > 0 ? items : [{ question: input.question || input.prompt || input.header || input.title, options: input.options }]);
  if (rawItems.length === 0) return null;
  const built = rawItems.map((it) => ({ question: it.question, options: buildQuestionOptions(it.options) }));
  const first = built[0];

  return {
    type: 'tool_question',
    id,
    questionNumber: 1,
    totalQuestions: built.length || 1,
    question: first.question,
    options: first.options,
    items: built,
  };
}

function asRecord(value: unknown): Record<string, unknown> {
  return (value ?? {}) as Record<string, unknown>;
}

function toGroupItem(
  name: string,
  tool: domain.ToolCall,
  input: Record<string, unknown>,
): ToolGroupItem {
  const cmd = field(input, COMMAND_KEYS);
  const target = extractTarget(name, input);
  const { type, action } = determineAction(name, cmd);

  const cleanTarget =
    target &&
    target.toLowerCase() !== name.toLowerCase() &&
    target.toLowerCase() !== action.toLowerCase()
      ? target
      : '';

  return {
    id: tool.id,
    type,
    action,
    target: cleanTarget,
    status: toStatus(tool.status),
    details: tool.output || undefined,
  };
}

function upsertPlan(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const entries = event.plan ?? [];
  if (entries.length === 0) return blocks;

  const id = 'session-tasklist';
  const block: AgentStreamBlock = {
    type: 'tool_todo',
    id,
    title: 'Tasklist',
    todos: entries.map((entry, index) => ({
      id: `task-${index}`,
      text: entry.content,
      status: toTodoStatus(entry.status),
    })),
  };

  return [...blocks.filter((candidate) => candidate.type !== 'tool_todo'), block];
}

function friendlyError(raw?: string): string | null {
  if (!raw) return null;
  if (/MessageAbortedError|aborted/i.test(raw)) return 'Question skipped — send a new message to continue';
  if (/cancelled|canceled/i.test(raw)) return 'Interrupted';
  return null;
}

function appendDiagnostic(
  blocks: AgentStreamBlock[],
  event: RuntimeEvent,
): AgentStreamBlock[] {
  const friendly = friendlyError(event.error);
  // Show question-skip notices instead of silently swallowing them
  const headline = friendly || event.error || event.text;
  if (!headline) return blocks;
  const lines = [`⚠ ${headline}`];
  if (event.error && event.text && !friendly) lines.push('', '```', event.text, '```');
  return [
    ...closeStreaming(blocks),
    { type: 'text', id: `diagnostic-${event.seq}`, content: lines.join('\n'), timestamp: event.at || Date.now(), variant: 'error' },
  ];
}

function closeStreaming(blocks: AgentStreamBlock[]): AgentStreamBlock[] {
  const last = blocks[blocks.length - 1];
  if (last && last.type === 'text' && last.isStreaming) {
    return [...blocks.slice(0, -1), { ...last, isStreaming: false, timestamp: last.timestamp || Date.now() }];
  }
  return blocks;
}

// Closes out any streaming indicators so the feed does not keep pulsing after
// the turn ends.
function settle(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const cleaned = closeThinking(closeStreaming(blocks));

  if (event.kind === 'turn.failed' && event.error) {
    if (friendlyError(event.error)) return cleaned;
    return [...cleaned, { type: 'text', id: `error-${event.seq}`, content: `⚠ ${event.error}`, timestamp: event.at || Date.now(), variant: 'error' }];
  }
  return cleaned;
}

function toStatus(status?: string): 'running' | 'completed' | 'error' {
  if (status === 'completed' || status === 'done' || status === 'success') return 'completed';
  if (status === 'error' || status === 'failed') return 'error';
  return 'running';
}

function toTodoStatus(status: string): 'pending' | 'in_progress' | 'completed' {
  const s = (status || '').toLowerCase();
  if (s === 'completed' || s === 'done' || s === 'complete' || s === 'finished') return 'completed';
  if (s === 'in_progress' || s === 'active' || s === 'running' || s === 'started' || s === 'progress') return 'in_progress';
  return 'pending';
}

function toTodos(input: Record<string, unknown>) {
  const raw = Array.isArray(input.todos) ? input.todos : [];
  return raw.map((entry, index) => {
    const item = (entry ?? {}) as Record<string, unknown>;
    return {
      id: `todo-${index}`,
      text: str(item.content) || str(item.text),
      status: toTodoStatus(str(item.status)),
    };
  });
}

function toFileList(output?: string): string[] {
  if (!output) return [];
  return output
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .slice(0, 12);
}

function summarize(tool: domain.ToolCall): string | undefined {
  if (!tool.output) return undefined;
  const firstLine = tool.output.split('\n')[0]?.trim();
  return firstLine && firstLine.length <= 120 ? firstLine : undefined;
}

function countLines(text?: string): number | undefined {
  if (!text) return undefined;
  return text.split('\n').length;
}

function str(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

// ---------------------------------------------------------------------------
// Edit diffs
// ---------------------------------------------------------------------------

interface EditFields {
  filePath: string;
  additions: number;
  deletions: number;
  diffLines: DiffLine[];
}

function str2(input: Record<string, unknown>, keys: string[]): string | undefined {
  for (const key of keys) {
    const value = input[key];
    if (typeof value === 'string') return value;
  }
  return undefined;
}

/**
 * Builds the diff for an edit tool from whatever the provider sends: Claude
 * (Edit old_string/new_string, MultiEdit edits[], Write content), OpenCode
 * (oldString/newString, content), Antigravity (TargetContent /
 * ReplacementContent, ReplacementChunks[], CodeContent) or explicit diffs.
 */
export function editBlockFields(
  name: string,
  input: Record<string, unknown>,
  diffs?: domain.FileDiff[],
): EditFields {
  const pairs: Array<{ oldText: string; newText: string }> = [];
  let filePath = field(input, PATH_KEYS);

  if (diffs?.length) {
    for (const d of diffs) pairs.push({ oldText: d.oldText || '', newText: d.newText || '' });
    filePath = diffs[0].path || filePath;
  } else if (Array.isArray(input.edits)) {
    for (const raw of input.edits as Array<Record<string, unknown>>) {
      const e = asRecord(raw);
      pairs.push({
        oldText: str2(e, ['old_string', 'oldString', 'TargetContent']) || '',
        newText: str2(e, ['new_string', 'newString', 'ReplacementContent']) || '',
      });
    }
  } else if (Array.isArray(input.ReplacementChunks)) {
    for (const raw of input.ReplacementChunks as Array<Record<string, unknown>>) {
      const e = asRecord(raw);
      pairs.push({ oldText: str2(e, ['TargetContent']) || '', newText: str2(e, ['ReplacementContent']) || '' });
    }
  } else {
    const oldText = str2(input, ['old_string', 'oldString', 'TargetContent', 'old_str']);
    const newText = str2(input, ['new_string', 'newString', 'ReplacementContent', 'new_str']);
    const content = str2(input, ['content', 'CodeContent', 'new_source', 'file_text', 'text']);
    if (oldText !== undefined || newText !== undefined) {
      pairs.push({ oldText: oldText || '', newText: newText || '' });
    } else if (content !== undefined) {
      pairs.push({ oldText: '', newText: content });
    }
  }

  const diffLines: DiffLine[] = [];
  let additions = 0;
  let deletions = 0;
  pairs.forEach((pair, i) => {
    if (i > 0) diffLines.push({ type: 'context', lineNum: 0, content: '⋯' });
    for (const line of lineDiff(pair.oldText, pair.newText)) {
      if (line.type === 'add') additions++;
      if (line.type === 'delete') deletions++;
      diffLines.push(line);
    }
  });
  return { filePath: filePath || name, additions, deletions, diffLines };
}

const MAX_LCS_CELLS = 250_000;

/** Line diff via LCS; very large inputs degrade to delete-all/add-all. */
function lineDiff(oldText: string, newText: string): DiffLine[] {
  const a = oldText ? oldText.replace(/\r\n/g, '\n').split('\n') : [];
  const b = newText ? newText.replace(/\r\n/g, '\n').split('\n') : [];
  const out: DiffLine[] = [];
  if (a.length * b.length > MAX_LCS_CELLS) {
    a.forEach((content, i) => out.push({ type: 'delete', lineNum: i + 1, content }));
    b.forEach((content, i) => out.push({ type: 'add', lineNum: i + 1, content }));
    return out;
  }
  const n = a.length;
  const m = b.length;
  const dp: Uint32Array[] = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  let i = 0;
  let j = 0;
  while (i < n || j < m) {
    if (i < n && j < m && a[i] === b[j]) {
      out.push({ type: 'context', lineNum: j + 1, content: a[i] });
      i++;
      j++;
    } else if (i < n && (j >= m || dp[i + 1][j] >= dp[i][j + 1])) {
      out.push({ type: 'delete', lineNum: i + 1, content: a[i] });
      i++;
    } else {
      out.push({ type: 'add', lineNum: j + 1, content: b[j] });
      j++;
    }
  }
  return out;
}
