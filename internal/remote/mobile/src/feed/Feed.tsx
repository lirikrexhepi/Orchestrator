import React, { useState } from 'react'
import {
  Check,
  ChevronDown,
  ChevronRight,
  CircleDashed,
  FileEdit,
  FileText,
  GitBranch,
  ListChecks,
  Loader2,
  Search,
  Terminal,
  Wrench,
  X,
  type IconComponent,
} from '../icons'
import Markdown from '../components/Markdown'
import { ProviderIcon } from '../components/modelPicker/ProviderIcon'
import { api } from '../api'
import { basename, dirname, duration } from '../format'
import { UserBubble } from './user/UserBubble'
import { Approval } from './ask/Approval'
import { Question } from './ask/Question'
import { StatusCard } from '../components/status/StatusCard'
import type { AgentStreamBlock, ToolGroupItem } from './types'

type Block = AgentStreamBlock

interface FeedProps {
  threadId: string
  blocks: Block[]
  turnMs: Record<string, number>
}

/** The conversation, one memoised component per block. */
export default function Feed({ threadId, blocks, turnMs }: FeedProps) {
  return (
    <div className="feed">
      {blocks.map((block) => (
        <React.Fragment key={block.id}>
          <BlockView threadId={threadId} block={block} />
          {turnMs[block.id] !== undefined && <div className="turn-end">took {duration(turnMs[block.id])}</div>}
        </React.Fragment>
      ))}
    </div>
  )
}

export const BlockView = React.memo(function BlockView({ threadId, block }: { threadId: string; block: Block }) {
  switch (block.type) {
    case 'user':
      return <UserBubble content={block.content} files={block.files} pending={block.pending} />

    case 'notice':
      return <SwitchNotice label={block.label} />

    case 'text':
      if (block.variant === 'error') {
        return <StatusCard>{block.content.replace(/^⚠\s*/, '')}</StatusCard>
      }
      return (
        <div className="say">
          <Markdown content={block.content} />
          {block.isStreaming && <span className="caret" aria-hidden="true" />}
        </div>
      )

    case 'thinking':
      return <Thought thinking={block.isThinking} text={block.thoughtText} />

    case 'tool_group':
      return (
        <div className="acts">
          {block.items.map((item, i) => (
            <Activity key={item.id || i} item={item} />
          ))}
        </div>
      )

    case 'tool_bash':
      return (
        <div className="acts">
          <Activity item={{ type: 'bash', action: 'Ran', target: block.command, details: block.output, status: block.status }} />
        </div>
      )

    case 'tool_search':
      return (
        <div className="acts">
          <Activity
            item={{
              type: 'search',
              action: 'Searched',
              target: block.query || '',
              details: block.files?.join('\n'),
              status: block.isSearching ? 'running' : 'completed',
            }}
          />
        </div>
      )

    case 'tool_edit':
      return <EditCard block={block} />

    case 'tool_todo':
      return <Todos block={block} />

    case 'tool_question':
      return <Question threadId={threadId} block={block} />

    case 'approval_request':
      return <Approval threadId={threadId} block={block} />

    case 'tool_plan':
      return (
        <div className="say">
          <Markdown content={`**${block.title}**\n\n${block.summary}`} />
        </div>
      )

    default:
      return null
  }
})

function SwitchNotice({ label }: { label: string }) {
  const parsed = parseSwitchNotice(label)
  if (!parsed) return <div className="notice">{label}</div>
  const fromShort = shortSwitchPart(parsed.fromDriver, parsed.fromModel)
  const toShort = shortSwitchPart(parsed.toDriver, parsed.toModel)
  if (fromShort.key === toShort.key) {
    return (
      <div className="notice notice-switch" aria-label={label}>
        <span className="notice-switch-item">
          <ProviderIcon driver={fromShort.iconKey} size={14} />
          <span className="notice-switch-text">{fromShort.text}</span>
        </span>
      </div>
    )
  }
  return (
    <div className="notice notice-switch" aria-label={label}>
      <span className="notice-switch-item">
        <ProviderIcon driver={fromShort.iconKey} size={14} />
        <span className="notice-switch-text">{fromShort.text}</span>
      </span>
      <span className="notice-switch-arrow" aria-hidden>
        →
      </span>
      <span className="notice-switch-item">
        <ProviderIcon driver={toShort.iconKey} size={14} />
        <span className="notice-switch-text">{toShort.text}</span>
      </span>
    </div>
  )
}

function parseSwitchNotice(label: string): { fromDriver: string; fromModel: string; toDriver: string; toModel: string } | null {
  if (!label.startsWith('Switched from ')) return null
  const parts = label.slice(14).split(' to ')
  if (parts.length !== 2) return null
  const splitPart = (p: string) => {
    const idx = p.indexOf(':')
    if (idx !== -1) return { driver: p.slice(0, idx).trim(), model: p.slice(idx + 1).trim() }
    return { driver: p.trim(), model: '' }
  }
  const from = splitPart(parts[0])
  const to = splitPart(parts[1])
  if (!from.driver || !to.driver) return null
  return { fromDriver: from.driver, fromModel: from.model, toDriver: to.driver, toModel: to.model }
}

function driverIconKey(driver: string): string {
  const key = driver.toLowerCase().replace(/[^a-z]/g, '')
  if (key.includes('claude')) return 'claude'
  if (key.includes('opencode')) return 'opencode'
  if (key.includes('antigravity') || key.includes('gemini')) return 'antigravity'
  if (key.includes('codex') || key.includes('openai') || key.includes('gpt')) return 'codex'
  return driver.toLowerCase()
}

function shortModelId(model: string): string {
  const last = model.split('/').pop()?.trim() || model.trim()
  return last
}

function shortSwitchPart(driver: string, model: string): { key: string; text: string; iconKey: string } {
  const short = shortModelId(model)
  const text = short ? `${driver}:${short}` : driver
  const key = `${driver.toLowerCase()}:${short.toLowerCase()}`
  return { key, text, iconKey: driverIconKey(driver) }
}

function Thought({ thinking, text }: { thinking: boolean; text: string }) {
  const [open, setOpen] = useState(false)
  return (
    <div>
      <button className="thought" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        {open ? <ChevronDown size={16} aria-hidden="true" /> : <ChevronRight size={16} aria-hidden="true" />}
        <span className={thinking ? 'shimmer' : undefined}>{thinking ? 'Thinking' : 'Thought'}</span>
      </button>
      {open && text && <div className="thought-body">{text}</div>}
    </div>
  )
}

const ACT_ICON: Record<ToolGroupItem['type'], IconComponent> = {
  read: FileText,
  bash: Terminal,
  search: Search,
  edit: FileEdit,
  write: FileEdit,
  git: GitBranch,
  generic: Wrench,
}

function Activity({ item }: { item: ToolGroupItem }) {
  const [open, setOpen] = useState(false)
  const Icon = ACT_ICON[item.type] ?? Wrench
  const hasOut = Boolean(item.details && item.details.trim())
  const status = item.status ?? 'completed'
  return (
    <>
      <button
        className="act"
        data-status={status}
        onClick={() => hasOut && setOpen((v) => !v)}
        aria-expanded={hasOut ? open : undefined}
        disabled={!hasOut}
        style={{ opacity: 1 }}
      >
        <Icon size={16} aria-hidden="true" />
        <span className="verb">{item.action}</span>
        {item.target && <span className="target">{item.target}</span>}
        <span className="st" aria-label={status}>
          {status === 'running' ? (
            <Loader2 size={14} className="spin" aria-hidden="true" />
          ) : status === 'error' ? (
            <X size={14} aria-hidden="true" />
          ) : hasOut ? (
            open ? <ChevronDown size={14} aria-hidden="true" /> : <ChevronRight size={14} aria-hidden="true" />
          ) : (
            <Check size={14} aria-hidden="true" />
          )}
        </span>
      </button>
      {open && hasOut && <div className="out">{item.details!.slice(0, 20000)}</div>}
    </>
  )
}

function EditCard({ block }: { block: Extract<Block, { type: 'tool_edit' }> }) {
  const lines = block.diffLines ?? []
  const [open, setOpen] = useState(lines.length > 0 && lines.length <= 30)
  const running = block.status === 'running'
  return (
    <div className="card">
      <button className="card-head" onClick={() => lines.length > 0 && setOpen((v) => !v)} aria-expanded={open}>
        {running ? <Loader2 size={18} className="spin" aria-hidden="true" /> : <FileEdit size={18} aria-hidden="true" />}
        <div className="file">
          <div className="name">{basename(block.filePath)}</div>
          {dirname(block.filePath) && <div className="dir">{dirname(block.filePath)}</div>}
        </div>
        <span className="stat" aria-label={`${block.additions ?? 0} lines added, ${block.deletions ?? 0} removed`}>
          <span className="a">+{block.additions ?? 0}</span>
          <span className="d">−{block.deletions ?? 0}</span>
        </span>
        {lines.length > 0 && (open ? <ChevronDown size={16} aria-hidden="true" /> : <ChevronRight size={16} aria-hidden="true" />)}
      </button>
      {open && lines.length > 0 && (
        <div className="diff">
          {lines.map((line, i) => (
            <div key={i} className={`diff-line ${line.type}`}>
              <span className="ln">{line.lineNum || ''}</span>
              <span className="mk">{line.type === 'add' ? '+' : line.type === 'delete' ? '−' : ''}</span>
              <span>{line.content}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function Todos({ block }: { block: Extract<Block, { type: 'tool_todo' }> }) {
  const done = block.todos.filter((t) => t.status === 'completed').length
  return (
    <div className="card todo">
      <div className="todo-h">
        <span style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <ListChecks size={16} aria-hidden="true" /> Plan
        </span>
        <span className="when">
          {done} of {block.todos.length}
        </span>
      </div>
      {block.todos.map((t) => (
        <div className="todo-item" data-status={t.status} key={t.id}>
          {t.status === 'completed' ? (
            <Check size={16} aria-hidden="true" />
          ) : t.status === 'in_progress' ? (
            <Loader2 size={16} className="spin" aria-hidden="true" />
          ) : (
            <CircleDashed size={16} aria-hidden="true" />
          )}
          <span>{t.text}</span>
        </div>
      ))}
    </div>
  )
}
