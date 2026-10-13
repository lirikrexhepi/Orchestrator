import { useSyncExternalStore } from 'react'
import { logConnection } from './connectionLog'
import { keepAwakeEnabled, onKeepAwakeChange } from './keepAwake'
import {
  flushPhoneLogs,
  logPhoneError,
  logPhoneInfo,
  logPhoneWarn,
  setAgentRunningState,
  setSocketSender,
} from './phoneLogger'
import { api, getBase, getToken } from './api'
import { reduceEvent, userBlock } from './feed/reducer'
import { trackSubagents, type SubagentData } from './feed/subagents'
import { readLocal, readThread, writeLocal, writeThread } from './cache'
import { setBadge } from './push'
import type { AgentStreamBlock } from './feed/types'
import type {
  FileRef,
  ModelChoice,
  ProviderInfo,
  RuntimeEvent,
  ServerMessage,
  ThreadSummary,
} from './types'

/**
 * One store for the whole app. Threads the user opens keep a folded block
 * list (the same reducer the desktop uses); every thread keeps a summary for
 * the inbox. Live events arrive in batches over one socket and are applied
 * after the snapshot a thread was loaded from, so nothing shows twice and
 * nothing that happened while loading is lost.
 */

export interface QueuedMessage {
  id: string
  text: string
  files: FileRef[]
  choice?: ModelChoice
}

export interface ThreadState {
  blocks: AgentStreamBlock[]
  lastSeq: number
  loaded: boolean
  loading: boolean
  error?: string
  busy: boolean
  turnStartedAt?: number
  /** Duration of each finished turn, keyed by the last block id of that turn. */
  turnMs: Record<string, number>
  queued: QueuedMessage[]
  sending: boolean
  context?: { tokens: number; window?: number }
  subagents: SubagentData[]
}

export type Connection = 'connecting' | 'live' | 'offline'

interface State {
  summaries: ThreadSummary[]
  summariesLoaded: boolean
  threads: Record<string, ThreadState>
  providers: ProviderInfo[]
  providersLoaded: boolean
  providersLoading: boolean
  connection: Connection
  pcDown: boolean
  /** Model picked on the phone for a thread's next sends; overrides its summary. */
  choices: Record<string, ModelChoice>
}

const PROVIDERS_CACHE_KEY = 'orchestrator_providers_cache'

function loadCachedProviders(): ProviderInfo[] {
  try {
    const raw = localStorage.getItem(PROVIDERS_CACHE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed) && parsed.length > 0) return parsed
    }
  } catch {
    /* ignore */
  }
  return []
}

const initialProviders = loadCachedProviders()

function withDriver(list: string[], driver: string): string[] {
  if (list[list.length - 1] === driver) return list
  return [...list.filter((d) => d !== driver), driver]
}

const SUMMARIES_CACHE_KEY = 'orchestrator_summaries_cache'
const initialSummaries = readLocal<ThreadSummary[]>(SUMMARIES_CACHE_KEY, []).map((s) => ({ ...s, busy: false }))

let state: State = {
  summaries: initialSummaries,
  summariesLoaded: initialSummaries.length > 0,
  threads: {},
  providers: initialProviders,
  providersLoaded: initialProviders.length > 0,
  providersLoading: false,
  connection: 'connecting',
  pcDown: false,
  choices: loadChoices(),
}

const listeners = new Set<() => void>()

function set(next: Partial<State>) {
  state = { ...state, ...next }
  for (const l of listeners) l()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function useStore<T>(select: (s: State) => T): T {
  return useSyncExternalStore(subscribe, () => select(state))
}

export function getState(): State {
  return state
}

// ---------------------------------------------------------------------------
// Threads
// ---------------------------------------------------------------------------

function emptyThread(): ThreadState {
  return { blocks: [], lastSeq: 0, loaded: false, loading: false, busy: false, turnMs: {}, queued: [], sending: false, subagents: [] }
}

function patchThread(threadId: string, patch: (t: ThreadState) => ThreadState) {
  const current = state.threads[threadId] ?? emptyThread()
  const next = patch(current)
  if (next === current) return
  set({ threads: { ...state.threads, [threadId]: next } })
}

/** Folds events into a thread, tracking busy state and turn durations. */
function fold(t: ThreadState, events: RuntimeEvent[]): ThreadState {
  let { blocks, busy, turnStartedAt, turnMs, context } = t
  let subagents = t.subagents ?? []
  let lastSeq = t.lastSeq
  let durations: Record<string, number> | null = null
  for (const event of events) {
    if (event.kind === 'usage') {
      const tokens = event.usage?.contextTokens || context?.tokens
      const window = event.usage?.contextWindow || context?.window
      if (tokens) context = { tokens, window }
      if (event.seq > lastSeq) lastSeq = event.seq
      continue
    }
    blocks = reduceEvent(blocks, event)
    subagents = trackSubagents(subagents, event)
    if (event.seq > lastSeq) lastSeq = event.seq
    if (event.kind === 'turn.started') {
      busy = true
      turnStartedAt = event.at || Date.now()
    } else if (event.kind === 'turn.completed' || event.kind === 'turn.failed' || event.kind === 'session.stopped') {
      if (busy && turnStartedAt && blocks.length > 0 && event.kind !== 'session.stopped') {
        durations = durations ?? { ...turnMs }
        durations[blocks[blocks.length - 1].id] = Math.max(0, (event.at || Date.now()) - turnStartedAt)
      }
      busy = false
      turnStartedAt = undefined
    }
  }
  return { ...t, blocks, busy, turnStartedAt, lastSeq, turnMs: durations ?? turnMs, context, subagents }
}

const saveTimers = new Map<string, number>()

function scheduleSave(threadId: string) {
  window.clearTimeout(saveTimers.get(threadId))
  saveTimers.set(
    threadId,
    window.setTimeout(() => {
      saveTimers.delete(threadId)
      const t = state.threads[threadId]
      if (t?.loaded) void writeThread({ id: threadId, blocks: t.blocks, lastSeq: t.lastSeq, turnMs: t.turnMs })
    }, 1500),
  )
}

async function hydrateThread(threadId: string) {
  const cached = await readThread(threadId)
  if (!cached) return
  patchThread(threadId, (t) =>
    t.loaded || t.blocks.length > 0
      ? t
      : { ...t, blocks: cached.blocks, lastSeq: cached.lastSeq, turnMs: cached.turnMs, loaded: true, busy: false },
  )
}

export async function loadThread(threadId: string, force = false) {
  const current = state.threads[threadId]
  if (current?.loading || (current?.loaded && !force)) return
  patchThread(threadId, (t) => ({ ...t, loading: true, error: undefined }))
  if (!current?.loaded) await hydrateThread(threadId)
  try {
    const snapshot = await api.thread(threadId)
    patchThread(threadId, (t) => {
      const base: ThreadState = { ...emptyThread(), queued: t.queued, loaded: true }
      const folded = fold(base, snapshot.events ?? [])
      // Keep optimistic bubbles the snapshot does not contain yet.
      const pending = t.blocks.filter(
        (b) => b.type === 'user' && b.pending && !folded.blocks.some((f) => f.type === 'user' && f.content === b.content),
      )
      const liveSeq = typeof snapshot.lastSeq === 'number' ? snapshot.lastSeq : folded.lastSeq
      const stale = liveSeq === 0
      return {
        ...folded,
        blocks: [...folded.blocks, ...pending],
        lastSeq: liveSeq,
        busy: stale ? false : folded.busy,
        turnStartedAt: stale ? undefined : folded.turnStartedAt,
        loading: false,
        sending: t.sending,
      }
    })
    scheduleSave(threadId)
    void drainQueue(threadId)
  } catch (e) {
    const error = failure(e)
    patchThread(threadId, (t) => ({ ...t, loading: false, error }))
  }
}

// ---------------------------------------------------------------------------
// Live events
// ---------------------------------------------------------------------------

let refreshTimer: number | undefined

function scheduleSummaryRefresh(delay = 1200) {
  window.clearTimeout(refreshTimer)
  refreshTimer = window.setTimeout(() => void refreshSummaries(), delay)
}

const SPAWN_TOOL_NAMES = new Set(['task', 'agent'])
const pendingSubagentIds = new Map<string, Set<string>>()

function trackSummarySubagents(threadId: string, summary: { runningSubagents?: number; totalSubagents?: number }, e: RuntimeEvent) {
  if (e.kind === 'tool.call' && e.tool && SPAWN_TOOL_NAMES.has(e.tool.name.toLowerCase()) && e.tool.id) {
    let pending = pendingSubagentIds.get(threadId)
    if (!pending) {
      pending = new Set()
      pendingSubagentIds.set(threadId, pending)
    }
    if (!pending.has(e.tool.id)) {
      pending.add(e.tool.id)
      summary.runningSubagents = (summary.runningSubagents ?? 0) + 1
      summary.totalSubagents = (summary.totalSubagents ?? 0) + 1
    }
    return
  }
  if (e.kind === 'tool.result' && e.tool?.id) {
    const pending = pendingSubagentIds.get(threadId)
    if (pending?.has(e.tool.id) && !e.tool.async) {
      pending.delete(e.tool.id)
      summary.runningSubagents = Math.max(0, (summary.runningSubagents ?? 1) - 1)
    }
    return
  }
  if (e.kind === 'turn.failed' || e.kind === 'session.stopped') {
    pendingSubagentIds.delete(threadId)
    summary.runningSubagents = 0
    return
  }
  if (e.kind === 'turn.completed' && e.stopReason === 'cancelled') {
    pendingSubagentIds.delete(threadId)
    summary.runningSubagents = 0
  }
}

function applyEvents(events: RuntimeEvent[]) {
  const byThread = new Map<string, RuntimeEvent[]>()
  for (const event of events) {
    if (!event?.threadId) continue
    const list = byThread.get(event.threadId)
    if (list) list.push(event)
    else byThread.set(event.threadId, [event])
  }
  if (byThread.size === 0) return

  let threads = state.threads
  let summaries = state.summaries
  let refresh = false
  const finished: string[] = []

  for (const [threadId, list] of byThread) {
    const t = threads[threadId]
    if (t?.loaded) {
      const fresh = t.lastSeq > 0 ? list.filter((e) => !(e.seq > 0 && e.seq <= t.lastSeq)) : list
      if (fresh.length > 0) {
        threads = { ...threads, [threadId]: fold(t, fresh) }
        scheduleSave(threadId)
        if (fresh.some((e) => e.kind === 'turn.completed' || e.kind === 'turn.failed')) finished.push(threadId)
      }
    }

    const index = summaries.findIndex((s) => s.threadId === threadId)
    if (index < 0) {
      refresh = true
    } else {
      const summary = { ...summaries[index] }
      for (const e of list) {
        summary.lastActivity = Math.max(summary.lastActivity ?? 0, e.at || 0)
        if (e.driver) summary.drivers = withDriver(summary.drivers ?? (summary.driver ? [summary.driver] : []), e.driver.toLowerCase())
        trackSummarySubagents(threadId, summary, e)
        switch (e.kind) {
          case 'turn.started':
            summary.busy = true
            summary.turnStartedAt = e.at
            logPhoneInfo(`Agent turn started: ${threadId}`)
            break
          case 'turn.completed':
          case 'turn.failed':
            if (summary.busy && summary.turnStartedAt) summary.lastTurnMs = (e.at || Date.now()) - summary.turnStartedAt
            summary.busy = false
            summary.turnStartedAt = undefined
            summary.attention = ''
            if (!finished.includes(threadId)) finished.push(threadId)
            refresh = true
            if (e.kind === 'turn.failed') {
              logPhoneError(`Agent turn failed: ${threadId}${e.error ? ` (${e.error})` : ''}`)
            } else {
              logPhoneInfo(`Agent turn completed: ${threadId}`)
            }
            break
          case 'agent.message':
            summary.preview = e.delta ? ((summary.preview ?? '') + (e.text ?? '')).slice(-240) : e.text
            break
          case 'approval.request':
            summary.attention = 'approval'
            break
          case 'question.asked':
            summary.attention = 'question'
            break
          case 'approval.resolved':
          case 'question.answered':
            summary.attention = ''
            refresh = true
            break
          case 'session.started':
          case 'session.stopped':
            refresh = true
            break
        }
      }
      summaries = [...summaries]
      summaries[index] = summary
    }
  }

  set({ threads, summaries })
  const anyBusy = Object.values(threads).some((t) => t.busy) || summaries.some((s) => s.busy)
  setAgentRunningState(anyBusy)
  setBadge(summaries.filter((t) => t.attention).length)
  if (refresh) scheduleSummaryRefresh()
  for (const threadId of finished) void drainQueue(threadId)
}

// ---------------------------------------------------------------------------
// Summaries, models
// ---------------------------------------------------------------------------

export async function refreshSummaries() {
  try {
    const summaries = await api.threads()
    const list = Array.isArray(summaries) ? summaries : []
    const seen = new Set(list.map((s) => s.threadId))
    for (const id of pendingSubagentIds.keys()) {
      if (!seen.has(id)) pendingSubagentIds.delete(id)
    }
    for (const s of list) {
      const pending = pendingSubagentIds.get(s.threadId)
      if (!pending || pending.size === 0) {
        if ((s.runningSubagents ?? 0) === 0) pendingSubagentIds.delete(s.threadId)
        continue
      }
      if ((s.runningSubagents ?? 0) === 0) pendingSubagentIds.delete(s.threadId)
      else s.runningSubagents = Math.max(s.runningSubagents ?? 0, pending.size)
    }
    writeLocal(SUMMARIES_CACHE_KEY, list)
    set({ summaries: list, summariesLoaded: true, pcDown: false })
    setBadge(list.filter((t) => t.attention).length)
  } catch {
    set({ summariesLoaded: true })
  }
}

export async function loadProviders(refresh = false) {
  if (state.providersLoading) return
  if (!refresh && state.providersLoaded && state.providers.length > 0) return
  set({ providersLoading: true })
  try {
    const providers = await api.models(refresh)
    if (Array.isArray(providers)) {
      if (providers.length > 0) {
        try {
          localStorage.setItem(PROVIDERS_CACHE_KEY, JSON.stringify(providers))
        } catch {
          /* private mode */
        }
      }
      set({ providers, providersLoaded: true, providersLoading: false })
      return
    }
    set({ providersLoaded: true, providersLoading: false })
  } catch {
    set({ providersLoaded: true, providersLoading: false })
  }
}

// ---------------------------------------------------------------------------
// Model choice
// ---------------------------------------------------------------------------

const CHOICES_KEY = 'orchestrator_model_choices'

function loadChoices(): Record<string, ModelChoice> {
  try {
    return JSON.parse(localStorage.getItem(CHOICES_KEY) || '{}') || {}
  } catch {
    return {}
  }
}

export function setChoice(threadId: string, choice: ModelChoice) {
  const choices = { ...state.choices, [threadId]: choice }
  try {
    localStorage.setItem(CHOICES_KEY, JSON.stringify(choices))
  } catch {
    /* private mode */
  }
  set({ choices })
}

/** The model a thread will use next: the phone's pick, else what it runs. */
export function effectiveChoice(threadId: string): ModelChoice | undefined {
  const picked = state.choices[threadId]
  if (picked) return picked
  const summary = state.summaries.find((s) => s.threadId === threadId)
  if (summary?.driver) return { driver: summary.driver, account: summary.account, model: summary.model, options: summary.options }
  return undefined
}

/** A choice worth sending: only when it differs from what the thread runs. */
function choiceToSend(threadId: string): ModelChoice | undefined {
  const picked = state.choices[threadId]
  if (!picked) return undefined
  const summary = state.summaries.find((s) => s.threadId === threadId)
  if (
    summary &&
    summary.driver === picked.driver &&
    (!picked.account || picked.account === summary.account) &&
    summary.model === picked.model &&
    JSON.stringify(summary.options ?? {}) === JSON.stringify(picked.options ?? {})
  ) {
    return undefined
  }
  return picked
}

// ---------------------------------------------------------------------------
// Sending
// ---------------------------------------------------------------------------

export async function send(threadId: string, text: string, files: FileRef[] = []) {
  const trimmed = text.trim()
  if (!trimmed && files.length === 0) return
  const t = state.threads[threadId]
  const choice = choiceToSend(threadId)

  // A busy agent gets the message when its turn ends, like the desktop queue.
  if (t?.busy || t?.sending) {
    patchThread(threadId, (th) => ({
      ...th,
      queued: [...th.queued, { id: `q-${Date.now()}`, text: trimmed, files, choice }],
    }))
    return
  }
  await dispatch(threadId, trimmed, files, choice)
}

async function dispatch(threadId: string, text: string, files: FileRef[], choice?: ModelChoice) {
  const stamp = Date.now()
  logPhoneInfo(`Prompt dispatched to agent: ${threadId}`, { textLength: text.length, filesCount: files.length })
  patchThread(threadId, (th) => ({
    ...th,
    sending: true,
    blocks: [...th.blocks, userBlock(text, `user-local-${stamp}`, files.map((f) => ({ path: f.path, mime: f.mime })), stamp)],
  }))
  try {
    await api.send(threadId, text, files, choice)
    patchThread(threadId, (th) => ({ ...th, sending: false }))
  } catch (e) {
    logPhoneError(`Failed to send prompt to agent: ${threadId}`, { error: message(e) })
    patchThread(threadId, (th) => ({
      ...th,
      sending: false,
      blocks: [
        ...th.blocks,
        { type: 'text', id: `send-error-${stamp}`, content: `Not sent: ${message(e)}`, variant: 'error', timestamp: Date.now() },
      ],
    }))
  }
}

async function drainQueue(threadId: string) {
  const t = state.threads[threadId]
  if (!t || t.queued.length === 0 || t.busy || t.sending) return
  const [next, ...rest] = t.queued
  patchThread(threadId, (th) => ({ ...th, queued: rest }))
  await dispatch(threadId, next.text, next.files, next.choice)
}

export function removeQueued(threadId: string, id: string) {
  patchThread(threadId, (th) => ({ ...th, queued: th.queued.filter((q) => q.id !== id) }))
}

export async function interrupt(threadId: string) {
  try {
    await api.interrupt(threadId)
  } catch {
    /* the turn may already be over */
  }
}

// ---------------------------------------------------------------------------
// Socket
// ---------------------------------------------------------------------------

let socket: WebSocket | null = null
let backoff = 1000
let reconnectTimer: number | undefined
let started = false

function wsUrl(): string {
  const base = getBase()
  const token = getToken()
  const suffix = token ? `/api/ws?token=${encodeURIComponent(token)}` : '/api/ws'
  if (base && /^https?:\/\//.test(base)) {
    const u = new URL(base)
    return `${u.protocol === 'https:' ? 'wss:' : 'ws:'}//${u.host}${suffix}`
  }
  return `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}${suffix}`
}

function connect() {
  window.clearTimeout(reconnectTimer)
  if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) return
  set({ connection: 'connecting' })
  try {
    socket = new WebSocket(wsUrl())
  } catch {
    retry()
    return
  }
  let opened = false
  socket.onopen = () => {
    opened = true
    backoff = 1000
    logConnection('Connected to the PC')
    set({ connection: 'live', pcDown: false })
    setSocketSender((logs) => {
      if (socket && socket.readyState === WebSocket.OPEN) {
        try {
          socket.send(JSON.stringify({ action: 'phone_logs', logs }))
          return true
        } catch {
          return false
        }
      }
      return false
    })
    sendPresence()
    void flushPhoneLogs()
    // Anything missed while disconnected comes back through fresh snapshots.
    void refreshSummaries()
    for (const [threadId, t] of Object.entries(state.threads)) {
      if (t.loaded || t.error) void loadThread(threadId, true)
    }
  }
  socket.onmessage = (event) => {
    let msg: ServerMessage
    try {
      msg = JSON.parse(event.data)
    } catch {
      return
    }
    if (msg.type === 'events' && msg.events) applyEvents(msg.events)
    else if (msg.type === 'event' && msg.event) applyEvents([msg.event])
  }
  socket.onclose = (event) => {
    socket = null
    setSocketSender(null)
    const text = opened
      ? `Lost the live connection to the PC (code ${event.code})`
      : `Couldn't connect to the PC (code ${event.code}). It may be off or asleep, or Tailscale on it isn't running or is signed out`
    logConnection(text)
    logPhoneWarn(text, { code: event.code, reason: event.reason, wasClean: event.wasClean, openedBefore: opened })
    set({ connection: 'offline', pcDown: true })
    retry()
  }
  socket.onerror = (e) => {
    logPhoneError('PC WebSocket connection error', { error: String(e) })
    socket?.close()
  }
}

function retry() {
  window.clearTimeout(reconnectTimer)
  reconnectTimer = window.setTimeout(connect, backoff)
  backoff = Math.min(backoff * 2, 10000)
}

let presenceThread = ''

function sendPresence() {
  if (!socket || socket.readyState !== WebSocket.OPEN) return
  try {
    socket.send(
      JSON.stringify({ action: 'presence', threadId: presenceThread, visible: document.visibilityState === 'visible', keepAwake: keepAwakeEnabled() }),
    )
  } catch {
    return
  }
}

export function setPresence(threadId: string | null) {
  presenceThread = threadId ?? ''
  sendPresence()
}

export function failure(e: unknown): string {
  const text = message(e)
  logConnection(e instanceof Error && e.message !== text ? `${text} (${e.message})` : text)
  if (isUnreachable(text)) set({ pcDown: true })
  return text
}

export function retryNow() {
  backoff = 1000
  if (!socket || socket.readyState !== WebSocket.OPEN) {
    connect()
  } else {
    void refreshSummaries()
    for (const [threadId, t] of Object.entries(state.threads)) {
      if (t.error) void loadThread(threadId, true)
    }
  }
  if (state.providers.length === 0) void loadProviders()
}

/** Starts the socket and the background refresh; safe to call repeatedly. */
export function startSync() {
  if (started) return
  started = true
  connect()
  void refreshSummaries()
  void loadProviders()
  window.setInterval(() => {
    if (document.visibilityState === 'visible') void refreshSummaries()
  }, 15000)
  window.setInterval(() => {
    if (document.visibilityState === 'visible') sendPresence()
  }, 10000)
  document.addEventListener('visibilitychange', sendPresence)
  onKeepAwakeChange(sendPresence)
  window.addEventListener('pagehide', sendPresence)
  // iOS suspends sockets in the background; reconnect the moment we return.
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState !== 'visible') return
    backoff = 1000
    if (!socket || socket.readyState !== WebSocket.OPEN) connect()
    else void refreshSummaries()
    if (state.providers.length === 0) void loadProviders()
  })
  window.addEventListener('online', () => {
    backoff = 1000
    connect()
    if (state.providers.length === 0) void loadProviders()
  })
}

const UNREACHABLE = /load failed|failed to fetch|networkerror|network request failed|network connection was lost/i
const UNREACHABLE_TEXT = "Can't reach your PC"

export const isUnreachable = (text: string | null | undefined) => text === UNREACHABLE_TEXT

export function message(e: unknown): string {
  const text = e instanceof Error ? e.message : String(e)
  if (UNREACHABLE.test(text)) return UNREACHABLE_TEXT
  if (/unexpected token|not valid json/i.test(text)) return 'The PC answered with a page instead of data, it may run an older app version'
  if (!/^\d{3}$/.test(text)) return text
  const status = Number(text)
  if (status === 401 || status === 403) return 'This phone needs pairing again'
  if (status === 404) return 'Not found on your PC'
  if (status >= 502) return UNREACHABLE_TEXT
  return 'Your PC ran into an error'
}
