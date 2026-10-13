import type { RuntimeEvent } from '../types'

export type SubagentStatus = 'running' | 'completed' | 'error' | 'stopped'

export interface SubagentEntry {
  id: string
  kind: 'text' | 'tool'
  text?: string
  name?: string
  target?: string
  status?: 'running' | 'completed' | 'error'
}

export interface SubagentData {
  id: string
  toolId: string
  previousToolIds?: string[]
  title: string
  agentType?: string
  prompt?: string
  status: SubagentStatus
  startedAt?: number
  endedAt?: number
  toolCount?: number
  tokens?: number
  result?: string
  entries: SubagentEntry[]
}

const SPAWN_TOOLS = new Set(['task', 'agent'])
const TARGET_KEYS = ['file_path', 'command', 'pattern', 'path', 'url', 'query', 'description', 'prompt']
const MAX_ENTRIES = 200

function targetOf(input: Record<string, unknown> | undefined): string {
  if (!input) return ''
  for (const key of TARGET_KEYS) {
    const value = input[key]
    if (typeof value === 'string' && value.trim()) return value.trim().split('\n')[0]
  }
  return ''
}

function find(list: SubagentData[], toolId: string): number {
  return list.findIndex((agent) => agent.toolId === toolId || agent.previousToolIds?.includes(toolId))
}

function update(list: SubagentData[], toolId: string, at: number | undefined, change: (agent: SubagentData) => SubagentData): SubagentData[] {
  const index = find(list, toolId)
  if (index < 0) {
    const blank: SubagentData = { id: `subagent-${toolId}`, toolId, title: 'Subagent', status: 'running', startedAt: at, entries: [] }
    return [...list, change(blank)]
  }
  const out = list.slice()
  out[index] = change(list[index])
  return out
}

function setEntry(entries: SubagentEntry[], entry: SubagentEntry): SubagentEntry[] {
  const index = entries.findIndex((existing) => existing.id === entry.id)
  if (index >= 0) {
    const out = entries.slice()
    out[index] = entry
    return out
  }
  const out = [...entries, entry]
  return out.length > MAX_ENTRIES ? out.slice(out.length - MAX_ENTRIES) : out
}

function relaunched(list: SubagentData[], toolId: string, title: string, agentType: string | undefined, at: number | undefined): SubagentData[] {
  if (!title || find(list, toolId) >= 0) return list
  const index = list.findIndex((agent) => agent.status === 'stopped' && agent.title === title && agent.agentType === agentType)
  if (index < 0) return list
  const previous = list[index]
  const out = list.slice()
  out[index] = {
    ...previous,
    toolId,
    previousToolIds: [...(previous.previousToolIds ?? []), previous.toolId],
    status: 'running',
    startedAt: at ?? previous.startedAt,
    endedAt: undefined,
    result: undefined,
  }
  return out
}

function interrupted(agent: SubagentData, output: string | undefined): boolean {
  if (agent.status === 'stopped') return true
  const text = (output ?? '').trim()
  return !text || text === agent.title || /interrupt|killed|aborted|cancell?ed/i.test(text.slice(0, 200))
}

export function trackSubagents(list: SubagentData[], event: RuntimeEvent): SubagentData[] {
  const tool = event.tool
  switch (event.kind) {
    case 'tool.call': {
      if (!tool || !SPAWN_TOOLS.has(tool.name.toLowerCase())) return list
      const input = (tool.input ?? {}) as Record<string, unknown>
      const title = String(input.description || input.subagent_type || '')
      const agentType = typeof input.subagent_type === 'string' ? input.subagent_type : undefined
      return update(relaunched(list, tool.id, title, agentType, event.at), tool.id, event.at, (agent) => ({
        ...agent,
        title: title || agent.title,
        agentType: agentType ?? agent.agentType,
        prompt: typeof input.prompt === 'string' ? input.prompt : agent.prompt,
        startedAt: agent.startedAt ?? event.at,
      }))
    }
    case 'tool.result': {
      if (!tool) return list
      const index = find(list, tool.id)
      if (index < 0 || list[index].toolId !== tool.id) return list
      return update(list, tool.id, event.at, (agent) => {
        if (tool.async) return agent
        if (tool.status === 'failed' && interrupted(agent, tool.output)) {
          return { ...agent, status: 'stopped', endedAt: agent.endedAt ?? event.at }
        }
        const startedAt = agent.startedAt ?? event.at
        const reported = tool.durationMs && tool.durationMs > 0 ? tool.durationMs : 0
        return {
          ...agent,
          status: tool.status === 'failed' ? 'error' : 'completed',
          startedAt,
          endedAt: reported && startedAt ? startedAt + reported : event.at,
          toolCount: tool.toolCount && tool.toolCount > 0 ? tool.toolCount : agent.toolCount,
          tokens: tool.tokens && tool.tokens > 0 ? tool.tokens : agent.tokens,
          result: tool.output || agent.result,
        }
      })
    }
    case 'subagent.progress': {
      if (!event.parentToolId || !tool) return list
      return update(list, event.parentToolId, event.at, (agent) => ({
        ...agent,
        toolCount: Math.max(agent.toolCount ?? 0, tool.toolCount ?? 0) || agent.toolCount,
        tokens: Math.max(agent.tokens ?? 0, tool.tokens ?? 0) || agent.tokens,
      }))
    }
    case 'subagent.message': {
      if (!event.parentToolId || !event.text) return list
      return update(list, event.parentToolId, event.at, (agent) => ({
        ...agent,
        entries: setEntry(agent.entries, { id: `t-${event.itemId || event.seq}`, kind: 'text', text: event.text }),
      }))
    }
    case 'subagent.tool.call': {
      if (!event.parentToolId || !tool) return list
      return update(list, event.parentToolId, event.at, (agent) => ({
        ...agent,
        entries: setEntry(agent.entries, {
          id: `u-${tool.id}`,
          kind: 'tool',
          name: tool.name,
          target: targetOf(tool.input),
          status: 'running',
        }),
      }))
    }
    case 'subagent.tool.result': {
      if (!event.parentToolId || !tool) return list
      return update(list, event.parentToolId, event.at, (agent) => ({
        ...agent,
        entries: agent.entries.map((entry) =>
          entry.kind === 'tool' && entry.id === `u-${tool.id}` ? { ...entry, status: tool.status === 'failed' ? 'error' : 'completed' } : entry,
        ),
      }))
    }
    case 'session.stopped':
    case 'turn.failed':
    case 'turn.completed': {
      const stopped = event.kind !== 'turn.completed' || event.stopReason === 'cancelled'
      if (!stopped || !list.some((agent) => agent.status === 'running')) return list
      return list.map((agent) => (agent.status === 'running' ? { ...agent, status: 'stopped', endedAt: event.at } : agent))
    }
    default:
      return list
  }
}
