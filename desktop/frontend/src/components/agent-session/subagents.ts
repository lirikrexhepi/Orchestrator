import { RuntimeEvent } from './eventReducer';
import { AgentStreamBlock, SubagentBlockData, SubagentEntry } from './types';

const SPAWN_TOOLS = new Set(['task', 'agent']);
const TARGET_KEYS = ['file_path', 'command', 'pattern', 'path', 'url', 'query', 'description', 'prompt'];

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

function describe(input: Record<string, unknown>): { title: string; agentType: string | undefined; prompt: string | undefined } {
  const rawTitle = input.description ?? input.subagent_type ?? input.title;
  const title = typeof rawTitle === 'string' ? rawTitle.trim() : '';
  const rawType = input.subagent_type ?? input.agent;
  const agentType = typeof rawType === 'string' && rawType.trim() ? rawType : undefined;
  const rawPrompt = input.prompt;
  const prompt = typeof rawPrompt === 'string' && rawPrompt ? rawPrompt : undefined;
  return { title, agentType, prompt };
}

function withDetails(agent: SubagentBlockData, input: Record<string, unknown>): SubagentBlockData {
  const { title, agentType, prompt } = describe(input);
  return {
    ...agent,
    title: title || agent.title,
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
      if (!tool || !SPAWN_TOOLS.has(tool.name.toLowerCase())) return blocks;
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
        if (tool.async) return agent;
        const input = (tool.input ?? {}) as Record<string, unknown>;
        if (!isTerminal(tool.status)) {
          return {
            ...withDetails(agent, input),
            status: agent.status === 'running' ? 'running' : agent.status,
            startedAt: earliest(agent.startedAt, event.at),
            toolCount: tool.toolCount && tool.toolCount > 0 ? tool.toolCount : agent.toolCount,
            tokens: tool.tokens && tool.tokens > 0 ? tool.tokens : agent.tokens,
          };
        }
        if (tool.status === 'failed' && interrupted(agent, tool.output)) {
          return { ...withDetails(agent, input), status: 'stopped', endedAt: agent.endedAt ?? event.at };
        }
        const startedAt = agent.startedAt ?? event.at;
        const reported = tool.durationMs && tool.durationMs > 0 ? tool.durationMs : 0;
        const observed = event.at ? Math.max(event.at, startedAt ?? 0) : startedAt;
        return {
          ...withDetails(agent, input),
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
        toolCount: Math.max(agent.toolCount ?? 0, tool.toolCount ?? 0) || agent.toolCount,
        tokens: Math.max(agent.tokens ?? 0, tool.tokens ?? 0) || agent.tokens,
      }));
    }
    case 'subagent.message': {
      const parent = event.parentToolId;
      if (!parent || !event.text) return blocks;
      return update(blocks, parent, event.at, (agent) => ({
        ...agent,
        entries: setEntry(agent.entries, { id: `t-${event.itemId || event.seq}`, kind: 'text', text: event.text ?? '', at: event.at }),
      }));
    }
    case 'subagent.tool.call': {
      const parent = event.parentToolId;
      if (!parent || !tool) return blocks;
      return update(blocks, parent, event.at, (agent) => ({
        ...agent,
        entries: setEntry(agent.entries, {
          id: `u-${tool.id}`,
          kind: 'tool',
          name: tool.name,
          target: targetOf(tool.input as Record<string, unknown> | undefined),
          status: 'running',
          at: event.at,
        }),
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
