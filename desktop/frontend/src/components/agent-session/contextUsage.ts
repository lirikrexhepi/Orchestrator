import type { RuntimeEvent } from './eventReducer';

export interface ContextUsage {
  tokens: number;
  window?: number;
  sessionInput?: number;
  sessionOutput?: number;
  sessionCacheRead?: number;
  sessionCacheWrite?: number;
  sessionCostUsd?: number;
}

export function nextContext(prev: ContextUsage | undefined, event: RuntimeEvent): ContextUsage | undefined {
  const usage = event.usage;
  if (!usage) return prev;
  const tokens = usage.contextTokens && usage.contextTokens > 0 ? usage.contextTokens : (prev?.tokens ?? 0);
  const window = usage.contextWindow && usage.contextWindow > 0 ? usage.contextWindow : prev?.window;

  const sessionInput = (prev?.sessionInput ?? 0) + Math.max(0, usage.inputTokens ?? 0);
  const sessionOutput = (prev?.sessionOutput ?? 0) + Math.max(0, usage.outputTokens ?? 0);
  const sessionCacheRead = (prev?.sessionCacheRead ?? 0) + Math.max(0, usage.cacheReadTokens ?? 0);
  const sessionCacheWrite = (prev?.sessionCacheWrite ?? 0) + Math.max(0, usage.cacheWriteTokens ?? 0);
  const sessionCostUsd = (prev?.sessionCostUsd ?? 0) + Math.max(0, usage.costUsd ?? 0);

  const cumulativeTokens = sessionInput + sessionOutput + sessionCacheRead + sessionCacheWrite;
  if (tokens <= 0 && cumulativeTokens <= 0) return prev;

  if (
    prev &&
    prev.tokens === tokens &&
    prev.window === window &&
    prev.sessionInput === sessionInput &&
    prev.sessionOutput === sessionOutput &&
    prev.sessionCacheRead === sessionCacheRead &&
    prev.sessionCacheWrite === sessionCacheWrite &&
    prev.sessionCostUsd === sessionCostUsd
  ) {
    return prev;
  }

  return {
    tokens,
    window,
    sessionInput,
    sessionOutput,
    sessionCacheRead,
    sessionCacheWrite,
    sessionCostUsd,
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
