import type { RuntimeEvent } from './eventReducer';

export interface ContextUsage {
  tokens: number;
  window?: number;
  sessionInput?: number;
  sessionOutput?: number;
  sessionCacheRead?: number;
  sessionCacheWrite?: number;
  sessionCostUsd?: number;
  /** Internal turn-level usage map to prevent multi-counting across intermediate frames */
  _turns?: Record<string, {
    input: number;
    output: number;
    cacheRead: number;
    cacheWrite: number;
    costUsd: number;
  }>;
}

export function nextContext(prev: ContextUsage | undefined, event: RuntimeEvent): ContextUsage | undefined {
  const usage = event.usage;
  if (!usage) return prev;
  const tokens = usage.contextTokens && usage.contextTokens > 0 ? usage.contextTokens : (prev?.tokens ?? 0);
  const window = usage.contextWindow && usage.contextWindow > 0 ? usage.contextWindow : prev?.window;

  const key = event.turnId || event.itemId || '_default';
  const turns = prev?._turns ? { ...prev._turns } : {};

  turns[key] = {
    input: Math.max(0, usage.inputTokens ?? 0),
    output: Math.max(0, usage.outputTokens ?? 0),
    cacheRead: Math.max(0, usage.cacheReadTokens ?? 0),
    cacheWrite: Math.max(0, usage.cacheWriteTokens ?? 0),
    costUsd: Math.max(0, usage.costUsd ?? 0),
  };

  let sessionInput = 0;
  let sessionOutput = 0;
  let sessionCacheRead = 0;
  let sessionCacheWrite = 0;
  let sessionCostUsd = 0;

  for (const t of Object.values(turns)) {
    sessionInput += t.input;
    sessionOutput += t.output;
    sessionCacheRead += t.cacheRead;
    sessionCacheWrite += t.cacheWrite;
    sessionCostUsd += t.costUsd;
  }

  const cumulativeTokens = sessionInput + sessionOutput + sessionCacheRead + sessionCacheWrite;
  if (tokens <= 0 && cumulativeTokens <= 0) return prev;

  return {
    tokens,
    window,
    sessionInput,
    sessionOutput,
    sessionCacheRead,
    sessionCacheWrite,
    sessionCostUsd,
    _turns: turns,
  };
}

export function latestContext(events: RuntimeEvent[] | undefined): ContextUsage | undefined {
  let context: ContextUsage | undefined;
  for (const event of events ?? []) {
    if (event.kind === 'usage') context = nextContext(context, event);
  }
  return context;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1).replace(/\.0$/, '')}M`;
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return String(n);
}
