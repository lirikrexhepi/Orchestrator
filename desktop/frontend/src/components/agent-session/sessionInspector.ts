import { main } from '../../../wailsjs/go/models';
import type { AgentStreamBlock, BashToolBlockData, SubagentBlockData, ToolGroupBlockData } from './types';
import type { ThreadFileChange } from './threadChanges';

export type FileRepoScope = main.FileRepoScope;

export interface SessionCheck {
  id: string;
  label: string;
  command: string;
  status: 'running' | 'passed' | 'failed';
  exitCode?: number;
  output?: string;
}

export interface McpCallItem {
  action: string;
  target?: string;
  status?: string;
}

export interface McpServerUsage {
  server: string;
  calls: number;
  items: McpCallItem[];
}

export interface RepoChangeGroup {
  key: string;
  root: string;
  name: string;
  branch?: string;
  isGit: boolean;
  additions: number;
  deletions: number;
  fileCount: number;
  files: ThreadFileChange[];
}

const CHECK_COMMAND_PATTERN =
  /(?:^|[;&|]\s*)(?:npx\s+)?(?:go\s+(?:test|build|vet)|(?:npm|pnpm|bun)\s+(?:run\s+)?(?:test|build|lint|typecheck|check)|yarn\s+(?:(?:run\s+)?(?:build|lint|typecheck|check)|test)|vitest|jest|pytest|cargo\s+(?:test|check|build|clippy)|php\s+artisan\s+test|phpunit|playwright\s+test|dotnet\s+test|mvn\s+test|gradle\s+test|tsc|vite\s+build|wails\s+build|eslint|biome\s+check|ruff|mypy|golangci-lint|phpstan)\b/i;

export function isCheckCommand(cmd: string): boolean {
  if (!cmd) return false;
  return CHECK_COMMAND_PATTERN.test(cmd);
}

export function deriveCheckLabel(cmd: string): string {
  if (!cmd) return 'Check';
  // Strip leading cd ... && or cd ... ; prefixes
  let cleaned = cmd.replace(/^(?:cd\s+[^&;]+(?:\s*&&\s*|\s*;\s*))+/i, '').trim();
  if (!cleaned) cleaned = cmd.trim();
  if (cleaned.length <= 46) return cleaned;
  return cleaned.slice(0, 43).trimEnd() + '…';
}

function mapCheckStatus(status?: string, exitCode?: number): 'running' | 'passed' | 'failed' {
  if (status === 'error' || (exitCode !== undefined && exitCode !== 0)) {
    return 'failed';
  }
  if (status === 'running') {
    return 'running';
  }
  return 'passed';
}

export function extractSessionChecks(blocks: AgentStreamBlock[]): SessionCheck[] {
  const checks: SessionCheck[] = [];

  for (const block of blocks) {
    if (block.type === 'tool_bash') {
      const bash = block as BashToolBlockData;
      if (bash.command && isCheckCommand(bash.command)) {
        checks.push({
          id: bash.id,
          label: deriveCheckLabel(bash.command),
          command: bash.command,
          status: mapCheckStatus(bash.status, bash.exitCode),
          exitCode: bash.exitCode,
          output: bash.output,
        });
      }
    } else if (block.type === 'tool_group') {
      const group = block as ToolGroupBlockData;
      group.items.forEach((item, idx) => {
        if (item.type === 'bash') {
          const cmd = item.target || item.details || '';
          if (cmd && isCheckCommand(cmd)) {
            checks.push({
              id: item.id || `${group.id}-item-${idx}`,
              label: deriveCheckLabel(cmd),
              command: cmd,
              status: mapCheckStatus(item.status),
              output: item.details,
            });
          }
        }
      });
    } else if (block.type === 'subagent') {
      const agent = block as SubagentBlockData;
      agent.entries.forEach((entry, idx) => {
        if (entry.kind === 'tool') {
          const cmd = entry.target || '';
          if (cmd && isCheckCommand(cmd)) {
            checks.push({
              id: entry.id || `${agent.id}-entry-${idx}`,
              label: deriveCheckLabel(cmd),
              command: cmd,
              status: mapCheckStatus(entry.status),
              output: entry.output,
            });
          }
        }
      });
    }
  }

  // Return newest-first so the latest verification sits at the top
  return checks.reverse();
}

const ORCHESTRATOR_ACTIONS =
  /^(Checked Orchestrator|Delegated|Listed threads|Read thread|Messaged thread|Stopped thread|delegate_task)$/i;

export function inferMcpServer(actionOrName: string, target?: string): string {
  const trimmed = actionOrName.trim();
  const combined = `${trimmed} ${target || ''}`.trim();

  if (ORCHESTRATOR_ACTIONS.test(trimmed) || /(?:Checked Orchestrator|Delegated|Listed threads|Read thread|Messaged thread|Stopped thread|delegate_task)/i.test(combined)) {
    return 'orchestrator';
  }

  // mcp__<server>__<tool>
  const doubleUnderMatch = trimmed.match(/^mcp__([^_]+)__/i);
  if (doubleUnderMatch) return doubleUnderMatch[1];

  // <server>:<tool>
  const colonMatch = trimmed.match(/^([^:]+):/);
  if (colonMatch) return colonMatch[1];

  // mcp_<server>_<tool>
  const singleUnderMatch = trimmed.match(/^mcp_([a-zA-Z0-9_-]+)_[a-zA-Z0-9_-]+$/i);
  if (singleUnderMatch) return singleUnderMatch[1];

  return 'mcp';
}

export function extractMcpUsage(blocks: AgentStreamBlock[]): McpServerUsage[] {
  const serverMap = new Map<string, McpCallItem[]>();
  const delegatedChildIds = new Set<string>();

  const record = (server: string, item: McpCallItem) => {
    let list = serverMap.get(server);
    if (!list) {
      list = [];
      serverMap.set(server, list);
    }
    list.push(item);
  };

  for (const block of blocks) {
    if (block.type === 'tool_group') {
      const group = block as ToolGroupBlockData;
      for (const item of group.items) {
        if (item.type === 'mcp') {
          const server = inferMcpServer(item.action, item.target);
          if (server === 'orchestrator' && item.target) {
            delegatedChildIds.add(item.target);
          }
          record(server, {
            action: item.action,
            target: item.target,
            status: item.status,
          });
        }
      }
    } else if (block.type === 'mcp_app') {
      const server = inferMcpServer(block.toolName, block.resourceUri);
      record(server, {
        action: block.toolName,
        target: block.resourceUri,
        status: block.status,
      });
    } else if (block.type === 'subagent') {
      const sub = block as SubagentBlockData;
      if (sub.childThreadId && !delegatedChildIds.has(sub.childThreadId) && !delegatedChildIds.has(sub.id)) {
        delegatedChildIds.add(sub.childThreadId);
        record('orchestrator', {
          action: 'Delegated',
          target: sub.title || sub.childThreadId,
          status: sub.status,
        });
      }
    }
  }

  const result: McpServerUsage[] = [];
  for (const [server, items] of serverMap.entries()) {
    result.push({
      server,
      calls: items.length,
      items,
    });
  }

  result.sort((a, b) => b.calls - a.calls);
  return result;
}

export function groupChangesByRepo(
  changes: ThreadFileChange[],
  scopes: Record<string, FileRepoScope>,
  fallbackCwd?: string,
): RepoChangeGroup[] {
  const groupsByKey = new Map<string, RepoChangeGroup>();
  const order: string[] = [];

  const normKey = (p: string) => p.replace(/\\/g, '/').toLowerCase();

  const scopeLookup = new Map<string, FileRepoScope>();
  for (const [p, scope] of Object.entries(scopes || {})) {
    scopeLookup.set(normKey(p), scope);
    const trimmed = p.replace(/^\/[A-Za-z]:\//, (m) => m.slice(1));
    scopeLookup.set(normKey(trimmed), scope);
  }

  for (const change of changes) {
    const key = normKey(change.path);
    const scope = scopeLookup.get(key);

    let root: string;
    let name: string;
    let branch: string | undefined;
    let isGit: boolean;

    if (scope) {
      root = scope.root;
      name = scope.name || (root.replace(/\\/g, '/').split('/').filter(Boolean).pop() ?? 'Repository');
      branch = scope.branch;
      isGit = scope.isGit;
    } else {
      root = fallbackCwd || 'Workspace';
      name = fallbackCwd
        ? (fallbackCwd.replace(/\\/g, '/').split('/').filter(Boolean).pop() ?? fallbackCwd)
        : 'Workspace';
      branch = undefined;
      isGit = false;
    }

    const groupKey = normKey(root);
    let group = groupsByKey.get(groupKey);
    if (!group) {
      group = {
        key: groupKey,
        root,
        name,
        branch,
        isGit,
        additions: 0,
        deletions: 0,
        fileCount: 0,
        files: [],
      };
      groupsByKey.set(groupKey, group);
      order.push(groupKey);
    }

    if (branch && !group.branch) group.branch = branch;
    if (isGit && !group.isGit) group.isGit = isGit;

    group.additions += change.additions;
    group.deletions += change.deletions;
    group.fileCount += 1;
    group.files.push(change);
  }

  return order.map((k) => groupsByKey.get(k)!);
}
