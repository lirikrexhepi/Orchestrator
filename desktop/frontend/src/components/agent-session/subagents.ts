import { RuntimeEvent, editBlockFields } from './eventReducer';
import { AgentStreamBlock, SubagentBlockData, SubagentEntry } from './types';
import { isEditToolName } from './threadChanges';

const SPAWN_TOOLS = new Set(['task', 'agent', 'invoke_subagent', 'subagent', 'spawn', 'sub_agent']);
const DELEGATE_TOOLS = new Set([
  'delegate_task',
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
    if (block.type === 'subagent' && (block.id === id || block.previousToolIds?.includes(toolId))) return index;
  }
  return -1;
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

function describe(input: Record<string, unknown>): { title: string; agentType: string | undefined; prompt: string | undefined } {
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
  let agentType = typeof rawType === 'string' && rawType.trim() ? rawType.trim() : undefined;

  const driverVal = lookupField(input, ['driver']);
  const modelVal = lookupField(input, ['model']);
  if (typeof driverVal === 'string' && driverVal.trim()) {
    agentType = driverVal.trim();
  } else if (!agentType && typeof modelVal === 'string' && modelVal.trim()) {
    agentType = modelVal.trim();
  }

  return { title, agentType, prompt };
}

function withDetails(agent: SubagentBlockData, input: Record<string, unknown>): SubagentBlockData {
  const { title, agentType, prompt } = describe(input);
  const nextTitle = title && title !== 'Subagent' ? title : agent.title;
  return {
    ...agent,
    title: nextTitle,
    agentType: agentType ?? agent.agentType,
    prompt: prompt ?? agent.prompt,
  };
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
        ...withDetails(agent, input),
        startedAt: earliest(agent.startedAt, event.at),
      }));
    }
    case 'tool.result': {
      if (!tool) return blocks;
      const index = findAgent(blocks, tool.id);
      if (index < 0) return blocks;
      if ((blocks[index] as SubagentBlockData).toolId !== tool.id) return blocks;
      return update(blocks, tool.id, event.at, (agent) => {
        const input = (tool.input ?? {}) as Record<string, unknown>;
        const detailed = withDetails(agent, input);

        let childThreadId = agent.childThreadId;
        let outputStatus: string | undefined;
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
            }
          } catch {}
        }

        if (tool.async) {
          return {
            ...detailed,
            isAsync: true,
            status: 'running',
            childThreadId: childThreadId ?? agent.childThreadId,
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
            status: agent.status === 'running' ? 'running' : agent.status,
            startedAt: earliest(agent.startedAt, event.at),
            toolCount: tool.toolCount && tool.toolCount > 0 ? tool.toolCount : agent.toolCount,
            tokens: tool.tokens && tool.tokens > 0 ? tool.tokens : agent.tokens,
          };
        }
        if (tool.status === 'failed' && interrupted(agent, tool.output)) {
          return { ...detailed, status: 'stopped', endedAt: agent.endedAt ?? event.at };
        }
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
      const input = (tool.input ?? {}) as Record<string, unknown>;
      const target = (tool.target && tool.target.trim()) ? tool.target.trim() : targetOf(input);
      const edit = isEditToolName(tool.name)
        ? editBlockFields(tool.name || '', input, tool.diffs)
        : null;
      const entry: SubagentEntry = {
        id: `u-${tool.id}`,
        kind: 'tool',
        name: tool.name,
        target,
        status: 'running',
        at: event.at,
      };
      if (edit) {
        entry.filePath = edit.filePath && edit.filePath !== tool.name ? edit.filePath : target;
        entry.additions = edit.additions;
        entry.deletions = edit.deletions;
        entry.diffLines = edit.diffLines;
      }
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
      return update(blocks, parent, event.at, (agent) => ({
        ...agent,
        entries: agent.entries.map((entry) =>
          entry.kind === 'tool' && entry.id === `u-${tool.id}`
            ? { ...entry, status: tool.status === 'failed' ? 'error' : 'completed' }
            : entry,
        ),
      }));
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
