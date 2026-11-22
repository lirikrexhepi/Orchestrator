import { RuntimeEvent, editBlockFields } from './eventReducer';
import { AgentStreamBlock, SubagentBlockData, SubagentEntry } from './types';
import { isEditToolName } from './threadChanges';

const KNOWN_DRIVERS = new Set(['claude', 'codex', 'antigravity', 'opencode']);

const SPAWN_TOOLS = new Set(['task', 'agent', 'invoke_subagent', 'subagent', 'spawn', 'sub_agent']);
const DELEGATE_TOOLS = new Set([
  'delegate_task',
  'mcp_orchestrator_delegate_task',
  'mcp__orchestrator__delegate_task',
  'orchestrator:delegate_task',
  'orchestrator__delegate_task',
]);

export function isSpawnTool(name: string): boolean {
  const norm = (name || '').toLowerCase().trim();
  return SPAWN_TOOLS.has(norm) || DELEGATE_TOOLS.has(norm);
}

export function isDelegateTool(name: string): boolean {
  const norm = (name || '').toLowerCase().trim();
  return DELEGATE_TOOLS.has(norm);
}

const TARGET_KEYS = ['file_path', 'command', 'pattern', 'path', 'url', 'query', 'description', 'prompt', 'Prompt', 'Role', 'TypeName'];

export function isSubagentEvent(event: RuntimeEvent): boolean {
  return event.kind.startsWith('subagent.');
}

export function isSubagentBlock(block: AgentStreamBlock): block is SubagentBlockData {
  return block.type === 'subagent';
}

function targetOf(input: Record<string, unknown> | undefined): string {
  if (!input) return '';
  for (const key of TARGET_KEYS) {
    const value = input[key];
    if (typeof value === 'string' && value.trim()) return value.trim();
  }
  return '';
}

function blankAgent(toolId: string, at?: number): SubagentBlockData {
  return { type: 'subagent', id: `subagent-${toolId}`, toolId, title: 'Subagent', status: 'running', startedAt: at, entries: [] };
}

function replaceAgent(blocks: AgentStreamBlock[], index: number, next: SubagentBlockData): AgentStreamBlock[] {
  const out = blocks.slice();
  out[index] = next;
  return out;
}

function insertAgent(blocks: AgentStreamBlock[], agent: SubagentBlockData): AgentStreamBlock[] {
  let at = 0;
  while (at < blocks.length && blocks[at].type === 'subagent') at++;
  return [...blocks.slice(0, at), agent, ...blocks.slice(at)];
}

function findAgent(blocks: AgentStreamBlock[], toolId: string): number {
  const id = `subagent-${toolId}`;
  for (let index = blocks.length - 1; index >= 0; index--) {
    const block = blocks[index];
    if (
      block.type === 'subagent' &&
      (block.id === id ||
        block.toolId === toolId ||
        block.childThreadId === toolId ||
        block.previousToolIds?.includes(toolId))
    )
      return index;
  }
  return -1;
}

export function isTrackedSubagentTool(blocks: AgentStreamBlock[], toolId: string): boolean {
  return Boolean(toolId) && findAgent(blocks, toolId) >= 0;
}

function update(
  blocks: AgentStreamBlock[],
  toolId: string,
  at: number | undefined,
  change: (agent: SubagentBlockData) => SubagentBlockData,
): AgentStreamBlock[] {
  const index = findAgent(blocks, toolId);
  if (index < 0) return insertAgent(blocks, change(blankAgent(toolId, at)));
  return replaceAgent(blocks, index, change(blocks[index] as SubagentBlockData));
}

function relaunched(blocks: AgentStreamBlock[], toolId: string, title: string, agentType: string | undefined, at: number | undefined): AgentStreamBlock[] {
  if (findAgent(blocks, toolId) >= 0 || !title) return blocks;
  const index = blocks.findIndex(
    (block) => block.type === 'subagent' && block.status === 'stopped' && block.title === title && block.agentType === agentType,
  );
  if (index < 0) return blocks;
  const previous = blocks[index] as SubagentBlockData;
  return replaceAgent(blocks, index, {
    ...previous,
    id: `subagent-${toolId}`,
    toolId,
    previousToolIds: [...(previous.previousToolIds ?? []), previous.toolId],
    status: 'running',
    startedAt: at ?? previous.startedAt,
    endedAt: undefined,
    result: undefined,
  });
}

function interrupted(agent: SubagentBlockData, output: string | undefined): boolean {
  if (agent.status === 'stopped') return true;
  const text = (output ?? '').trim();
  return !text || text === agent.title || /interrupt|killed|aborted|cancell?ed/i.test(text.slice(0, 200));
}

function setEntry(entries: SubagentEntry[], entry: SubagentEntry): SubagentEntry[] {
  const index = entries.findIndex((existing) => existing.id === entry.id);
  if (index < 0) return [...entries, entry];
  const out = entries.slice();
  out[index] = entry;
  return out;
}

function lookupField(input: Record<string, unknown>, keys: string[]): unknown {
  const entries = Object.entries(input);
  for (const targetKey of keys) {
    const lowerTarget = targetKey.toLowerCase();
    for (const [k, v] of entries) {
      if (k.toLowerCase() === lowerTarget && v !== undefined && v !== null) {
        return v;
      }
    }
  }
  return undefined;
}

function normalizeKnownDriver(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined;
  const trimmed = value.trim().toLowerCase();
  return trimmed && KNOWN_DRIVERS.has(trimmed) ? trimmed : undefined;
}

function normalizeModel(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined;
  const trimmed = value.trim();
  if (!trimmed || trimmed.toLowerCase() === 'inherit') return undefined;
  return trimmed;
}

function normalizeText(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined;
  const trimmed = value.trim();
  return trimmed || undefined;
}

function extractConversationId(output?: string): string | undefined {
  if (!output) return undefined;
  const trimmed = output.trim();
  if (trimmed.startsWith('{') && trimmed.endsWith('}')) {
    try {
      const parsed = JSON.parse(trimmed);
      if (parsed && typeof parsed === 'object') {
        const cid = parsed.conversationId ?? parsed.conversation_id;
        if (typeof cid === 'string' && cid.trim()) return cid.trim();
      }
    } catch {}
  }
  const match = output.match(/conversationId["']?\s*[:=]\s*["']?([A-Za-z0-9_-]+)/i);
  return match?.[1]?.trim() || undefined;
}

function describe(input: Record<string, unknown>): {
  title: string;
  agentType: string | undefined;
  driver: string | undefined;
  model: string | undefined;
  cwd: string | undefined;
  prompt: string | undefined;
} {
  const explicitTitleVal = lookupField(input, ['description', 'title', 'role', 'subagent_type', 'typename', 'name']);
  let title = typeof explicitTitleVal === 'string' ? explicitTitleVal.trim() : '';

  const rawPromptVal = lookupField(input, ['prompt', 'task', 'instruction', 'instructions']);
  const prompt = typeof rawPromptVal === 'string' && rawPromptVal.trim() ? rawPromptVal.trim() : undefined;

  if (!title && prompt) {
    const lines = prompt.split(/\r?\n/);
    for (const rawLine of lines) {
      const cleaned = rawLine.replace(/^[\s#*>-]+/, '').trim();
      if (cleaned) {
        title = cleaned.length > 56 ? cleaned.slice(0, 55).trimEnd() + '…' : cleaned;
        break;
      }
    }
  }

  const rawType = lookupField(input, ['typename', 'subagent_type', 'agent', 'role']);
  const agentType = typeof rawType === 'string' && rawType.trim() ? rawType.trim() : undefined;
  const driver = normalizeKnownDriver(lookupField(input, ['driver']));
  const model = normalizeModel(lookupField(input, ['model']));
  const cwd = normalizeText(lookupField(input, ['cwd']));

  return { title, agentType, driver, model, cwd, prompt };
}

function withDetails(
  agent: SubagentBlockData,
  input: Record<string, unknown>,
  eventDriver?: string,
  eventModel?: string,
): SubagentBlockData {
  const { title, agentType, driver, model, cwd, prompt } = describe(input);
  const nextTitle = title && title !== 'Subagent' ? title : agent.title;
  return {
    ...agent,
    title: nextTitle,
    agentType: agentType ?? agent.agentType,
    driver: driver ?? agent.driver ?? normalizeKnownDriver(eventDriver),
    model: model ?? agent.model ?? normalizeModel(eventModel),
    cwd: cwd ?? agent.cwd,
    prompt: prompt ?? agent.prompt,
  };
}

function mergeSubagentToolEntry(
  tool: NonNullable<RuntimeEvent['tool']>,
  at: number | undefined,
  isResult: boolean,
  existing?: Extract<SubagentEntry, { kind: 'tool' }>,
): Extract<SubagentEntry, { kind: 'tool' }> {
  const input = (tool.input ?? {}) as Record<string, unknown>;
  const name = tool.name || existing?.name || 'tool';
  const target = (tool.target && tool.target.trim()) ? tool.target.trim() : (targetOf(input) || existing?.target || '');
  const status: 'running' | 'completed' | 'error' = isResult
    ? (tool.status === 'failed' || tool.status === 'error' ? 'error' : 'completed')
    : 'running';

  const entry: Extract<SubagentEntry, { kind: 'tool' }> = {
    id: `u-${tool.id}`,
    kind: 'tool',
    name,
    target,
    status,
    output: tool.output || existing?.output,
    summary: tool.summary || existing?.summary,
    at: existing?.at ?? at,
    filePath: existing?.filePath,
    additions: existing?.additions,
    deletions: existing?.deletions,
    diffLines: existing?.diffLines,
  };

  if (isEditToolName(name) || Boolean(tool.diffs?.length)) {
    const hasDiffs = Boolean(tool.diffs?.length);
    const needsDiffs = !existing?.diffLines?.length;
    if (hasDiffs || (needsDiffs && (tool.input || tool.diffs))) {
      const edit = editBlockFields(name, input, tool.diffs);
      entry.filePath = edit.filePath && edit.filePath !== name ? edit.filePath : (target || existing?.filePath);
      if (edit.diffLines.length > 0 || hasDiffs) {
        entry.additions = edit.additions;
        entry.deletions = edit.deletions;
        entry.diffLines = edit.diffLines;
      }
    }
  }

  return entry;
}

function isTerminal(status: string | undefined): boolean {
  return status === 'completed' || status === 'failed';
}

function earliest(current: number | undefined, next: number | undefined): number | undefined {
  if (!next) return current;
  if (!current) return next;
  return Math.min(current, next);
}

export function trackSubagents(blocks: AgentStreamBlock[], event: RuntimeEvent): AgentStreamBlock[] {
  const tool = event.tool;
  switch (event.kind) {
    case 'tool.call': {
      if (!tool || !isSpawnTool(tool.name)) return blocks;
      const input = (tool.input ?? {}) as Record<string, unknown>;
      const { title, agentType } = describe(input);
      return update(relaunched(blocks, tool.id, title, agentType, event.at), tool.id, event.at, (agent) => ({
        ...withDetails(agent, input, event.driver, event.model),
        startedAt: earliest(agent.startedAt, event.at),
      }));
    }
    case 'tool.result': {
      if (!tool) return blocks;
      const index = findAgent(blocks, tool.id);
      if (index < 0) return blocks;
      const targetAgent = blocks[index] as SubagentBlockData;
      if (targetAgent.toolId !== tool.id && targetAgent.childThreadId !== tool.id) return blocks;
      return update(blocks, tool.id, event.at, (agent) => {
        const input = (tool.input ?? {}) as Record<string, unknown>;
        const detailed = withDetails(agent, input, event.driver, event.model);

        if (agent.childThreadId && tool.id === agent.childThreadId) {
          const startedAt = agent.startedAt ?? event.at;
          const reported = tool.durationMs && tool.durationMs > 0 ? tool.durationMs : 0;
          const observed = event.at ? Math.max(event.at, startedAt ?? 0) : startedAt;
          return {
            ...detailed,
            status: tool.status === 'failed' ? 'error' : 'completed',
            startedAt,
            endedAt: reported && startedAt ? startedAt + reported : observed,
            toolCount: tool.toolCount && tool.toolCount > 0 ? tool.toolCount : agent.toolCount,
            tokens: tool.tokens && tool.tokens > 0 ? tool.tokens : agent.tokens,
            result: tool.output || agent.result,
          };
        }

        let childThreadId = agent.childThreadId;
        let outputStatus: string | undefined;
        let parsedCwd = detailed.cwd;
        if (isDelegateTool(tool.name) && tool.output) {
          try {
            const parsed = typeof tool.output === 'string' ? JSON.parse(tool.output.trim()) : tool.output;
            if (parsed && typeof parsed === 'object') {
              if (typeof parsed.threadId === 'string' && parsed.threadId.trim()) {
                childThreadId = parsed.threadId.trim();
              }
              if (typeof parsed.status === 'string' && parsed.status.trim()) {
                outputStatus = parsed.status.trim().toLowerCase();
              }
              if (typeof parsed.cwd === 'string' && parsed.cwd.trim()) {
                parsedCwd = parsed.cwd.trim();
              }
            }
          } catch {}
        }

        const isInvokeSubagent = (tool.name || '').toLowerCase() === 'invoke_subagent';
        const outputText = typeof tool.output === 'string' ? tool.output : '';
        const isInvokeAsyncAck =
          isInvokeSubagent &&
          outputText.includes('conversationId') &&
          outputText.toLowerCase().includes('will send you a message') &&
          agent.endedAt === undefined &&
          !agent.isAsync;

        if (tool.async || isInvokeAsyncAck) {
          const conversationId = extractConversationId(outputText);
          return {
            ...detailed,
            cwd: parsedCwd,
            isAsync: true,
            status: 'running',
            childThreadId: childThreadId ?? conversationId ?? agent.childThreadId,
            startedAt: earliest(agent.startedAt, event.at),
          };
        }

        if (isDelegateTool(tool.name) && childThreadId) {
          const isStillRunning = outputStatus === 'running';
          const isCompleted = outputStatus === 'completed' || outputStatus === 'done' || outputStatus === 'success';
          const nextStatus = isStillRunning ? 'running' : (isCompleted ? 'completed' : (tool.status === 'failed' ? 'error' : 'completed'));
          const startedAt = agent.startedAt ?? event.at;
          return {
            ...detailed,
            cwd: parsedCwd,
            isAsync: isStillRunning || agent.isAsync,
            childThreadId,
            status: nextStatus,
            startedAt,
            endedAt: nextStatus === 'running' ? undefined : (agent.endedAt ?? event.at),
            result: tool.output || agent.result,
          };
        }

        if (!isTerminal(tool.status)) {
          return {
            ...detailed,
            cwd: parsedCwd,
            status: agent.status === 'running' ? 'running' : agent.status,
            startedAt: earliest(agent.startedAt, event.at),
            toolCount: tool.toolCount && tool.toolCount > 0 ? tool.toolCount : agent.toolCount,
            tokens: tool.tokens && tool.tokens > 0 ? tool.tokens : agent.tokens,
          };
        }
        if (tool.status === 'failed' && interrupted(agent, tool.output)) {
          return { ...detailed, cwd: parsedCwd, status: 'stopped', endedAt: agent.endedAt ?? event.at };
        }
        const startedAt = agent.startedAt ?? event.at;
        const reported = tool.durationMs && tool.durationMs > 0 ? tool.durationMs : 0;
        const observed = event.at ? Math.max(event.at, startedAt ?? 0) : startedAt;
        return {
          ...detailed,
          cwd: parsedCwd,
          status: tool.status === 'failed' ? 'error' : 'completed',
          startedAt,
          endedAt: reported && startedAt ? startedAt + reported : observed,
          toolCount: tool.toolCount && tool.toolCount > 0 ? tool.toolCount : agent.toolCount,
          tokens: tool.tokens && tool.tokens > 0 ? tool.tokens : agent.tokens,
          result: tool.output || agent.result,
        };
      });
    }
    case 'subagent.progress': {
      const parent = event.parentToolId;
      if (!parent || !tool) return blocks;
      return update(blocks, parent, event.at, (agent) => ({
        ...agent,
        status: agent.status === 'completed' ? 'running' : agent.status,
        endedAt: agent.status === 'completed' ? undefined : agent.endedAt,
        toolCount: Math.max(agent.toolCount ?? 0, tool.toolCount ?? 0) || agent.toolCount,
        tokens: Math.max(agent.tokens ?? 0, tool.tokens ?? 0) || agent.tokens,
      }));
    }
    case 'subagent.message': {
      const parent = event.parentToolId;
      if (!parent || !event.text) return blocks;
      return update(blocks, parent, event.at, (agent) => ({
        ...agent,
        status: agent.status === 'completed' ? 'running' : agent.status,
        endedAt: agent.status === 'completed' ? undefined : agent.endedAt,
        entries: setEntry(agent.entries, { id: `t-${event.itemId || event.seq}`, kind: 'text', text: event.text ?? '', at: event.at }),
      }));
    }
    case 'subagent.tool.call': {
      const parent = event.parentToolId;
      if (!parent || !tool) return blocks;
      const entry = mergeSubagentToolEntry(tool, event.at, false);
      return update(blocks, parent, event.at, (agent) => ({
        ...agent,
        status: agent.status === 'completed' ? 'running' : agent.status,
        endedAt: agent.status === 'completed' ? undefined : agent.endedAt,
        entries: setEntry(agent.entries, entry),
      }));
    }
    case 'subagent.tool.result': {
      const parent = event.parentToolId;
      if (!parent || !tool) return blocks;
      return update(blocks, parent, event.at, (agent) => {
        const entryId = `u-${tool.id}`;
        const existingIndex = agent.entries.findIndex((entry) => entry.kind === 'tool' && entry.id === entryId);
        if (existingIndex < 0) {
          if (!tool.name) return agent;
          return {
            ...agent,
            entries: setEntry(agent.entries, mergeSubagentToolEntry(tool, event.at, true)),
          };
        }
        const existing = agent.entries[existingIndex] as Extract<SubagentEntry, { kind: 'tool' }>;
        return {
          ...agent,
          entries: setEntry(agent.entries, mergeSubagentToolEntry(tool, event.at, true, existing)),
        };
      });
    }
    case 'session.stopped':
    case 'turn.failed':
    case 'turn.completed': {
      const stopped = event.kind !== 'turn.completed' || event.stopReason === 'cancelled';
      if (!stopped || !blocks.some((block) => block.type === 'subagent' && block.status === 'running')) return blocks;
      return blocks.map((block) =>
        block.type === 'subagent' && block.status === 'running'
          ? { ...block, status: 'stopped', endedAt: event.at }
          : block,
      );
    }
    default:
      return blocks;
  }
}
