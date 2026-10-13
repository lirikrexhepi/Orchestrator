import { useEffect, useMemo, useState } from 'react'
import { Sheet, SheetList, SheetRow } from '../sheet'
import { ChevronLeft } from '../../icons'
import { elapsed } from '../../format'
import Markdown from '../Markdown'
import type { AgentStreamBlock } from '../../feed/types'
import type { SubagentData, SubagentStatus } from '../../feed/subagents'

const STATUS_WORD: Record<SubagentStatus, string> = {
  running: 'Running',
  completed: 'Done',
  error: 'Failed',
  stopped: 'Stopped',
}

function plural(count: number, word: string): string {
  return `${count} ${word}${count === 1 ? '' : 's'}`
}

function runningShells(blocks: AgentStreamBlock[]): number {
  let count = 0
  for (const block of blocks) {
    if (block.type === 'tool_bash' && block.status === 'running') count++
    if (block.type === 'tool_group') {
      for (const item of block.items) if (item.type === 'bash' && item.status === 'running') count++
    }
  }
  return count
}

function formatTokens(tokens?: number): string {
  if (!tokens || tokens <= 0) return ''
  if (tokens < 1000) return `${tokens} tok`
  return `${(tokens / 1000).toFixed(tokens < 10_000 ? 1 : 0).replace(/\.0$/, '')}k tok`
}

function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [active])
  return now
}

function detailOf(agent: SubagentData, now: number): string {
  const end = agent.status === 'running' ? now : agent.endedAt ?? now
  const time = agent.startedAt ? elapsed(end - agent.startedAt) : ''
  return [STATUS_WORD[agent.status], time, formatTokens(agent.tokens)].filter(Boolean).join(' · ')
}

function Glyph({ live }: { live: boolean }) {
  return <span className={live ? 'subagent-glyph live' : 'subagent-glyph'} aria-hidden />
}

function AgentDetail({ agent, now, onBack }: { agent: SubagentData; now: number; onBack: () => void }) {
  return (
    <div className="subagent-detail">
      <button type="button" className="subagent-back" onClick={onBack}>
        <ChevronLeft size={18} strokeWidth={2} aria-hidden />
        <span className="subagent-back-title">{agent.title}</span>
      </button>
      <div className="subagent-meta">{[agent.agentType, detailOf(agent, now)].filter(Boolean).join(' · ')}</div>
      <div className="subagent-entries">
        {agent.prompt ? <div className="subagent-brief">{agent.prompt}</div> : null}
        {agent.entries.map((entry) =>
          entry.kind === 'text' ? (
            <div key={entry.id} className="subagent-text">
              <Markdown content={entry.text ?? ''} />
            </div>
          ) : (
            <div key={entry.id} className="subagent-tool" data-status={entry.status}>
              <span className="subagent-tool-name">{entry.name}</span>
              <span className="subagent-tool-target">{entry.target}</span>
            </div>
          ),
        )}
        {agent.entries.length === 0 && agent.status === 'running' ? <div className="subagent-wait">Waiting for the first step…</div> : null}
        {agent.result && agent.status !== 'running' ? (
          <div className="subagent-result">
            <Markdown content={agent.result} />
          </div>
        ) : null}
      </div>
    </div>
  )
}

export function SubagentPill({ subagents, blocks, busy }: { subagents: SubagentData[]; blocks: AgentStreamBlock[]; busy: boolean }) {
  const [open, setOpen] = useState(false)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const running = subagents.filter((agent) => agent.status === 'running').length
  const shells = useMemo(() => (busy ? runningShells(blocks) : 0), [busy, blocks])
  const totalAgents = (busy ? 1 : 0) + running
  const live = totalAgents > 0 || shells > 0
  const now = useNow(open && running > 0)
  const selected = subagents.find((agent) => agent.id === selectedId) ?? null

  if (!live && subagents.length === 0) return null

  const parts = live
    ? [`${plural(totalAgents, 'agent')} running`, shells > 0 ? plural(shells, 'shell') : ''].filter(Boolean)
    : [plural(subagents.length, 'subagent')]
  const canOpen = subagents.length > 0

  return (
    <>
      <button
        type="button"
        className={live ? 'subagent-pill live' : 'subagent-pill'}
        onClick={() => canOpen && setOpen(true)}
        aria-label={canOpen ? `${parts.join(', ')}. Show subagents` : parts.join(', ')}
        disabled={!canOpen}
      >
        <Glyph live={live} />
        <span>{parts.join(' · ')}</span>
      </button>
      {open ? (
        <Sheet
          title="Subagents"
          onClose={() => {
            setOpen(false)
            setSelectedId(null)
          }}
        >
          {selected ? (
            <AgentDetail agent={selected} now={now} onBack={() => setSelectedId(null)} />
          ) : (
            <SheetList scroll scrollKey="subagents">
              {subagents.map((agent) => (
                <SheetRow
                  key={agent.id}
                  iconNode={<span className="subagent-dot" data-status={agent.status} aria-hidden />}
                  label={agent.title}
                  detail={detailOf(agent, now)}
                  chevron
                  onClick={() => setSelectedId(agent.id)}
                />
              ))}
            </SheetList>
          )}
        </Sheet>
      ) : null}
    </>
  )
}
