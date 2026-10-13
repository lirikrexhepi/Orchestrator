export type TaskState = 'pending' | 'running' | 'complete' | 'failed' | 'closed'

export interface FileRef {
  path: string
  mime?: string
}

export interface FileDiff {
  path: string
  oldText?: string
  newText?: string
}

export interface ToolCall {
  id: string
  name: string
  kind?: string
  status: string
  input?: Record<string, unknown>
  output?: string
  diffs?: FileDiff[]
  durationMs?: number
  toolCount?: number
  tokens?: number
  async?: boolean
}

export interface PlanEntry {
  content: string
  status: string
  priority?: string
}

export interface ApprovalOption {
  id: string
  name: string
  kind: string
}

export interface ApprovalRequest {
  requestId: string
  title: string
  detail?: string
  options?: ApprovalOption[]
}

export interface QuestionRequest {
  requestId: string
  questions: Array<{
    question: string
    options?: string[]
    isMultiSelect?: boolean
  }>
}

export interface RuntimeEvent {
  kind: string
  threadId: string
  turnId?: string
  instanceId?: string
  driver?: string
  seq: number
  itemId?: string
  at: number
  text?: string
  delta?: boolean
  icon?: string
  files?: FileRef[]
  tool?: ToolCall
  parentToolId?: string
  plan?: PlanEntry[]
  approval?: ApprovalRequest
  question?: QuestionRequest
  stopReason?: string
  error?: string
  rateLimits?: Array<{ window: string; status?: string; usedPercent?: number; resetsAt?: number }>
  usage?: { contextTokens?: number; contextWindow?: number }
}

export interface ServerMessage {
  type: 'event' | 'events' | 'agents' | 'status' | 'pong' | 'error'
  event?: RuntimeEvent
  events?: RuntimeEvent[]
  error?: string
}

export type ModelOptions = Record<string, unknown>

export interface ThreadSummary {
  source?: string
  threadId: string
  title: string
  kind: 'coordinator' | 'agent'
  driver: string
  account?: string
  drivers?: string[]
  model: string
  options?: ModelOptions
  state?: TaskState
  cwd?: string
  projectCwd?: string
  projectName?: string
  branch?: string
  live: boolean
  busy: boolean
  runningSubagents?: number
  totalSubagents?: number
  turnStartedAt?: number
  lastTurnMs?: number
  lastActivity?: number
  preview?: string
  attention?: 'approval' | 'question' | ''
}

export interface OptionChoice {
  id: string
  label: string
  default?: boolean
}

export interface OptionDescriptor {
  id: string
  label: string
  type: 'select' | 'boolean'
  choices?: OptionChoice[]
  default?: unknown
}

export interface ModelInfo {
  id: string
  name: string
  default?: boolean
  options?: OptionDescriptor[]
}

export interface AccountInfo {
  id: string
  name: string
}

export interface ProviderInfo {
  driver: string
  name: string
  models: ModelInfo[]
  accounts?: AccountInfo[]
}

export interface ModelChoice {
  driver: string
  account?: string
  model: string
  options?: ModelOptions
}

export interface Project {
  id?: string
  path: string
  name: string
  totalAgents: number
  runningAgents: number
  lastActivity: number
}

export type PreviewState = 'starting' | 'live' | 'failed'

export interface PreviewInfo {
  port: number
  url?: string
  state: PreviewState
  error?: string
  startedAt?: number
}

export interface MonitorStatus {
  supported: boolean
  count: number
  off: boolean
  autoOff: boolean
}

export interface PcDiagnostics {
  headless: boolean
  port: number
  publicUrl?: string
  connecting: boolean
  tunnelError?: string
  clients: number
  tailscale: boolean
  cloudflared: boolean
  log: string[]
}

export interface ManagedDevServer {
  id: string
  name: string
  command: string
  cwd: string
  port?: number
  status: 'running' | 'exited' | 'failed'
  log?: string[]
}

export interface DevServer {
  pid: number
  port: number
  name: string
  kind: string
  ownerThreadId?: string
  cwd?: string
  preview?: PreviewInfo
}

export interface ServerGroup {
  threadId?: string
  title: string
  servers: DevServer[]
}
