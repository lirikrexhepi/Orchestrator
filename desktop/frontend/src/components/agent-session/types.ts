import { DiffLine } from './EditTool';
import { TodoItem } from './TodoTool';
import { QuestionOption } from './QuestionTool';
import { ToolGroupItem } from './ToolGroup';

/**
 * Standard Agent Streaming Event & Message Types
 * Compatible with Claude Code CLI, Antigravity Agent loops, AI SDK, and MCP tools.
 */

export interface UserMessageFile {
  path: string;
  name?: string;
  mime?: string;
}

export interface UserMessageBlock {
  type: 'user';
  id: string;
  content: string;
  timestamp?: number;
  files?: UserMessageFile[];
  /** Added optimistically on send; cleared when the backend echo arrives. */
  pending?: boolean;
  turnId?: string;
}

/** Inline rule marking a provider/model switch mid-conversation. */
export interface NoticeBlock {
  type: 'notice';
  id: string;
  label: string;
  icon?: string;
}

export interface AssistantTextBlock {
  type: 'text';
  id: string;
  content: string;
  isStreaming?: boolean;
  timestamp?: number;
  /** Backend content item this block renders; deltas merge only into it. */
  itemId?: string;
  turnId?: string;
  /** Errors and diagnostics are never merged into by agent text. */
  variant?: 'error';
  model?: string;
  driver?: string;
}

export interface ThinkingBlockData {
  type: 'thinking';
  id: string;
  isThinking: boolean;
  thoughtText: string;
  durationSeconds?: number;
  itemId?: string;
  turnId?: string;
}

export interface ToolGroupBlockData {
  type: 'tool_group';
  id: string;
  title: string;
  summary?: string;
  items: ToolGroupItem[];
  /** Tool call ids folded into this group, so results can find their row. */
  memberIds?: string[];
}

export interface BashToolBlockData {
  type: 'tool_bash';
  id: string;
  command: string;
  output?: string;
  summary?: string;
  status?: 'running' | 'completed' | 'error';
  exitCode?: number;
}

export interface SearchToolBlockData {
  type: 'tool_search';
  id: string;
  files: string[];
  query?: string;
  summary?: string;
  isSearching?: boolean;
}

export interface EditToolBlockData {
  type: 'tool_edit';
  id: string;
  filePath: string;
  additions?: number;
  deletions?: number;
  diffLines?: DiffLine[];
  status?: 'running' | 'completed' | 'error';
  /** Tool name (Edit, Write, MultiEdit…), shown as the card's verb. */
  toolName?: string;
}

export interface TodoToolBlockData {
  type: 'tool_todo';
  id: string;
  title?: string;
  todos: TodoItem[];
}

export interface PlanToolBlockData {
  type: 'tool_plan';
  id: string;
  planFile?: string;
  title: string;
  summary: string;
  approved?: boolean;
}

export interface QuestionItemData {
  question: string;
  options: QuestionOption[];
}

export interface QuestionToolBlockData {
  type: 'tool_question';
  id: string;
  questionNumber?: number;
  totalQuestions?: number;
  question: string;
  options: QuestionOption[];
  items?: QuestionItemData[];
  answered?: boolean;
  selectedAnswer?: string;
}

export interface ApprovalOptionItem {
  id: string;
  name: string;
  kind?: string;
}

export interface ApprovalBlockData {
  type: 'approval_request';
  id: string;
  requestID: string;
  title: string;
  detail?: string;
  options: ApprovalOptionItem[];
  status?: 'pending' | 'resolved' | 'denied';
  decision?: string;
}

export type SubagentEntry =
  | { id: string; kind: 'text'; text: string; at?: number }
  | {
      id: string;
      kind: 'tool';
      name: string;
      target: string;
      status: 'running' | 'completed' | 'error';
      output?: string;
      at?: number;
    };

export interface SubagentBlockData {
  type: 'subagent';
  id: string;
  toolId: string;
  title: string;
  agentType?: string;
  prompt?: string;
  status: 'running' | 'completed' | 'error' | 'stopped';
  startedAt?: number;
  endedAt?: number;
  toolCount?: number;
  tokens?: number;
  result?: string;
  entries: SubagentEntry[];
  previousToolIds?: string[];
}

export type AgentStreamBlock =  | UserMessageBlock
  | NoticeBlock
  | AssistantTextBlock
  | ThinkingBlockData
  | ToolGroupBlockData
  | BashToolBlockData
  | SearchToolBlockData
  | EditToolBlockData
  | TodoToolBlockData
  | PlanToolBlockData
  | QuestionToolBlockData
  | ApprovalBlockData
  | SubagentBlockData;

