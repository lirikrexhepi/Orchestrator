import React, { useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { AnimatePresence, motion } from 'motion/react';
import { CheckCircleFilled, MaterialIcon, RunningCircleIcon, XCircleFilled } from '../common/icons';
import { formatTokens, ContextUsage } from './contextUsage';
import {
  SessionCheck,
  McpServerUsage,
  RepoChangeGroup,
  FileRepoScope,
  groupChangesByRepo,
  normalizeRepoPath,
} from './sessionInspector';
import { ThreadFileChange } from './threadChanges';
import { DARK_TONE, LIGHT_TONE, Tone } from './SubagentMenu';

export interface SessionInspectorSectionsProps {
  isLight: boolean;
  changes: ThreadFileChange[];
  repoScopes: Record<string, FileRepoScope>;
  checks: SessionCheck[];
  contextUsage?: ContextUsage;
  mcps: McpServerUsage[];
  workspaceCwd?: string;
  onOpenChanges?: (group?: RepoChangeGroup) => void;
  onOpenFile?: (path: string, group?: RepoChangeGroup) => void;
}

const TRANSITION = { duration: 0.22, ease: [0.32, 0.72, 0, 1] } as const;

const ContextRingIcon: React.FC<{ ratio: number; className?: string }> = ({ ratio, className = '' }) => {
  const clamped = Math.max(0.05, Math.min(1, ratio));
  const r = 6;
  const circ = 2 * Math.PI * r;
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true" className={`shrink-0 ${className}`}>
      <circle cx="8" cy="8" r={r} stroke="currentColor" strokeOpacity="0.22" strokeWidth="1.75" />
      <circle
        cx="8"
        cy="8"
        r={r}
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeDasharray={`${circ * clamped} ${circ}`}
        transform="rotate(-90 8 8)"
      />
    </svg>
  );
};

const SectionRow: React.FC<{
  tone: Tone;
  icon: React.ReactNode;
  label: React.ReactNode;
  trailing?: React.ReactNode;
  open: boolean;
  onToggle: () => void;
}> = ({ tone, icon, label, trailing, open, onToggle }) => (
  <button
    type="button"
    onClick={onToggle}
    aria-expanded={open}
    className={`group flex h-8 w-full items-center gap-2.5 rounded-[10px] px-2 text-left transition-colors cursor-pointer select-none ${tone.hover}`}
  >
    <span className={`flex w-4 shrink-0 items-center justify-center ${tone.muted}`}>{icon}</span>
    <span className={`min-w-0 flex-1 truncate whitespace-nowrap text-[12.5px] font-medium tracking-tight font-(family-name:--app-font) ${tone.title}`}>
      {label}
    </span>
    {trailing}
    <span className={`-mr-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-full ${tone.close}`}>
      <MaterialIcon name="expand_more" size={15} className={`transition-transform duration-200 ${open ? 'rotate-180' : ''}`} />
    </span>
  </button>
);

const ChecksCapsule: React.FC<{
  checks: SessionCheck[];
  isLight: boolean;
}> = ({ checks, isLight }) => {
  const [hovered, setHovered] = useState<{ index: number; check: SessionCheck; x: number; y: number } | null>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
  const [tooltipLeft, setTooltipLeft] = useState<number>(0);

  useLayoutEffect(() => {
    if (!hovered) return;
    const width = tooltipRef.current?.offsetWidth ?? 160;
    const margin = 12;
    const maxLeft = Math.max(margin, window.innerWidth - width - margin);
    const centered = hovered.x - width / 2;
    setTooltipLeft(Math.min(Math.max(margin, centered), maxLeft));
  }, [hovered]);

  return (
    <div
      onMouseLeave={() => setHovered(null)}
      className={`relative flex items-center gap-1.5 px-2 py-1 rounded-full border transition-colors select-none shrink-0 ${
        isLight
          ? 'bg-black/[0.05] border-black/[0.08] shadow-[inset_0_1px_1px_rgba(0,0,0,0.04)]'
          : 'bg-[#1c1c1f]/90 border-white/[0.08] shadow-[inset_0_1px_1px_rgba(255,255,255,0.05)] backdrop-blur-sm'
      }`}
    >
      {checks.slice(0, 6).map((c, i) => (
        <div
          key={c.id || i}
          className="relative flex items-center justify-center"
          onMouseEnter={(e) => {
            const rect = e.currentTarget.getBoundingClientRect();
            setHovered({ index: i, check: c, x: rect.left + rect.width / 2, y: rect.top });
          }}
        >
          <div className="relative flex items-center justify-center transition-transform duration-150 hover:scale-110">
            {c.status === 'running' ? (
              <RunningCircleIcon size={17} className={isLight ? 'text-[#F59E0B]' : 'text-[#FBBF24] drop-shadow-sm'} />
            ) : c.status === 'failed' ? (
              <XCircleFilled size={17} className="text-[#FF453A] drop-shadow-sm" />
            ) : (
              <CheckCircleFilled size={17} className={isLight ? 'text-[#22C55E]' : 'text-[#30D158] drop-shadow-sm'} />
            )}
          </div>
        </div>
      ))}

      {checks.length > 6 && (
        <span className="text-[10px] font-semibold tabular-nums opacity-60 px-0.5">
          +{checks.length - 6}
        </span>
      )}

      {typeof document !== 'undefined' &&
        createPortal(
          <AnimatePresence>
            {hovered && (
              <div
                key="checks-tooltip-anchor"
                ref={tooltipRef}
                style={{
                  position: 'fixed',
                  left: tooltipLeft,
                  top: hovered.y - 8,
                  transform: 'translateY(-100%)',
                  zIndex: 9999,
                }}
                className="pointer-events-none whitespace-nowrap"
              >
                <motion.div
                  initial={{ opacity: 0, y: 4, scale: 0.96 }}
                  animate={{ opacity: 1, y: 0, scale: 1 }}
                  exit={{ opacity: 0, y: 2, scale: 0.96 }}
                  transition={{ duration: 0.12 }}
                  className={`px-2.5 py-1 rounded-[8px] text-[11px] font-medium font-(family-name:--app-font) tracking-tight shadow-xl flex items-center border ${
                    isLight
                      ? 'bg-white/95 text-neutral-900 border-black/10 shadow-black/10'
                      : 'bg-[#222225]/95 text-white/90 border-white/10 shadow-[0_8px_24px_rgba(0,0,0,0.6)] backdrop-blur-md'
                  }`}
                >
                  <span className="font-mono text-[10.5px] opacity-90 max-w-[240px] truncate">
                    {hovered.check.label || hovered.check.command}
                  </span>
                </motion.div>
              </div>
            )}
          </AnimatePresence>,
          document.body,
        )}
    </div>
  );
};

function formatMcpStatus(status?: string): string {
  if (!status) return '';
  const lower = status.toLowerCase();
  if (lower === 'completed' || lower === 'done') return 'Completed';
  if (lower === 'running' || lower === 'in_progress' || lower === 'pending') return 'Running';
  if (lower === 'error' || lower === 'failed') return 'Failed';
  return lower.charAt(0).toUpperCase() + lower.slice(1);
}

export const SessionInspectorSections: React.FC<SessionInspectorSectionsProps> = ({
  isLight,
  changes,
  repoScopes,
  checks,
  contextUsage,
  mcps,
  workspaceCwd,
  onOpenChanges,
  onOpenFile,
}) => {
  const tone: Tone = isLight ? LIGHT_TONE : DARK_TONE;
  const checkPassIconTone = isLight ? 'text-[#22C55E]' : 'text-[#30D158]';
  const checkFailIconTone = isLight ? 'text-rose-500' : 'text-[#FF453A]';

  const [openChanges, setOpenChanges] = useState(true);
  const [openContext, setOpenContext] = useState(true);
  const [openMcps, setOpenMcps] = useState(true);

  const [expandedRepos, setExpandedRepos] = useState<Record<string, boolean>>({});
  const [expandedServers, setExpandedServers] = useState<Record<string, boolean>>({});

  const repoGroups = useMemo(
    () => groupChangesByRepo(changes, repoScopes, workspaceCwd),
    [changes, repoScopes, workspaceCwd],
  );

  // Avoid duplicating WorkspaceSection's top-level Changes row:
  // Only show the inspector Changes multi-repo breakdown when files were touched across >1 repo/branch or outside the workspace root
  const normWorkspace = workspaceCwd ? normalizeRepoPath(workspaceCwd).toLowerCase() : '';
  const shouldShowChanges =
    changes.length > 0 &&
    (repoGroups.length > 1 ||
      repoGroups.some(
        (g) => !g.isGit || (normWorkspace && normalizeRepoPath(g.root).toLowerCase() !== normWorkspace),
      ));

  const failedChecks = useMemo(() => checks.filter((c) => c.status === 'failed').length, [checks]);
  const runningChecks = useMemo(() => checks.filter((c) => c.status === 'running').length, [checks]);

  const cumulativeTokens = useMemo(() => {
    if (!contextUsage) return 0;
    return (
      (contextUsage.sessionCacheRead ?? 0) +
      (contextUsage.sessionCacheWrite ?? 0) +
      (contextUsage.sessionOutput ?? 0) +
      (contextUsage.sessionInput ?? 0)
    );
  }, [contextUsage]);

  const hasContextContent =
    Boolean(contextUsage && (contextUsage.tokens > 0 || cumulativeTokens > 0));

  const sessionTokenRows = useMemo(() => {
    if (!contextUsage) return [];
    return [
      { label: 'Cache read', count: contextUsage.sessionCacheRead ?? 0 },
      { label: 'Cache write', count: contextUsage.sessionCacheWrite ?? 0 },
      { label: 'Output', count: contextUsage.sessionOutput ?? 0 },
      { label: 'Uncached input', count: contextUsage.sessionInput ?? 0 },
    ];
  }, [contextUsage]);

  const contextRatio =
    contextUsage && contextUsage.tokens > 0 && contextUsage.window && contextUsage.window > 0
      ? Math.min(1, contextUsage.tokens / contextUsage.window)
      : 0;
  const contextPct = Math.round(contextRatio * 100);

  if (!shouldShowChanges && checks.length === 0 && !hasContextContent && mcps.length === 0) {
    return null;
  }

  return (
    <div className="flex min-h-0 flex-col gap-0.5 px-1.5 pb-1.5 font-(family-name:--app-font)">
      {/* Changes Section (only shown when multi-repo or outside workspace root) */}
      {shouldShowChanges && (
        <div className="flex min-h-0 shrink-0 flex-col">
          <div className={`mx-2 my-1 border-t ${tone.divider}`} />
          <SectionRow
            tone={tone}
            icon={<MaterialIcon name="account_tree" size={15} />}
            label="Repos"
            open={openChanges}
            onToggle={() => setOpenChanges((v) => !v)}
            trailing={
              <span className={`text-[11.5px] tabular-nums shrink-0 ${tone.muted}`}>
                {repoGroups.length}
              </span>
            }
          />

          <AnimatePresence initial={false}>
            {openChanges && (
              <motion.div
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: 'auto', opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                transition={TRANSITION}
                className="flex flex-col gap-0.5 overflow-hidden px-1"
              >
                {repoGroups.map((group) => {
                  const isExpanded = Boolean(expandedRepos[group.key]);
                  return (
                    <div key={group.key} className="flex flex-col">
                      <button
                        type="button"
                        onClick={() =>
                          setExpandedRepos((prev) => ({ ...prev, [group.key]: !prev[group.key] }))
                        }
                        className={`flex h-7 w-full items-center gap-2 px-2 rounded-[8px] ${tone.hover} transition-colors cursor-pointer select-none text-left`}
                      >
                        <MaterialIcon
                          name={group.isGit ? 'account_tree' : 'folder'}
                          size={13}
                          className={`${tone.muted} shrink-0`}
                        />
                        <span className={`min-w-0 flex-1 truncate text-[12px] font-medium ${tone.body}`}>
                          {group.name}
                        </span>
                        {group.branch && (
                          <span className={`truncate max-w-[82px] text-[10.5px] ${tone.faint}`}>
                            {group.branch}
                          </span>
                        )}
                        <span className="flex items-center gap-1.5 text-[11px] font-mono tabular-nums shrink-0">
                          {group.additions > 0 && <span className="text-emerald-500">+{group.additions}</span>}
                          {group.deletions > 0 && <span className="text-rose-500">−{group.deletions}</span>}
                        </span>
                        <MaterialIcon
                          name="expand_more"
                          size={14}
                          className={`transition-transform duration-200 ${isExpanded ? 'rotate-180' : ''} ${tone.faint}`}
                        />
                      </button>

                      <AnimatePresence initial={false}>
                        {isExpanded && (
                          <motion.div
                            initial={{ height: 0, opacity: 0 }}
                            animate={{ height: 'auto', opacity: 1 }}
                            exit={{ height: 0, opacity: 0 }}
                            transition={TRANSITION}
                            className="flex flex-col overflow-hidden pt-0.5 pl-4 pr-1 gap-0.5"
                          >
                            <div className="flex flex-col gap-0.5 max-h-[176px] overflow-y-auto overflow-x-hidden custom-scrollbar pr-0.5">
                              {group.files.map((file) => {
                                const basename = file.path.split(/[/\\]/).pop() || file.path;
                                return (
                                  <button
                                    key={file.key}
                                    type="button"
                                    onClick={() => {
                                      if (onOpenFile) onOpenFile(file.path, group);
                                      else if (onOpenChanges) onOpenChanges(group);
                                    }}
                                    className={`flex h-7 shrink-0 w-full items-center justify-between text-left text-[11.5px] px-2 rounded-[8px] ${tone.hover} cursor-pointer transition-colors`}
                                  >
                                    <span className={`truncate flex-1 ${tone.body}`} title={file.path}>
                                      {basename}
                                    </span>
                                    <span className="flex items-center gap-1.5 text-[10.5px] font-mono tabular-nums shrink-0 ml-2">
                                      {file.additions > 0 && <span className="text-emerald-500">+{file.additions}</span>}
                                      {file.deletions > 0 && <span className="text-rose-500">−{file.deletions}</span>}
                                    </span>
                                  </button>
                                );
                              })}
                            </div>
                            {onOpenChanges && (
                              <button
                                type="button"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  onOpenChanges(group);
                                }}
                                className={`flex h-7 shrink-0 items-center gap-1.5 px-2 rounded-[8px] text-[11px] font-medium cursor-pointer transition-colors self-start ${tone.hover} ${tone.muted}`}
                              >
                                <MaterialIcon name="difference" size={13} />
                                <span>View diff</span>
                              </button>
                            )}
                          </motion.div>
                        )}
                      </AnimatePresence>
                    </div>
                  );
                })}
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      )}

      {/* Checks Section (non-expanding, latest batch only) */}
      {checks.length > 0 && (
        <div className="flex shrink-0 flex-col">
          <div className={`mx-2 my-1 border-t ${tone.divider}`} />
          <div className="flex h-8 w-full items-center gap-2.5 rounded-[10px] px-2 select-none">
            <span className={`flex w-4 shrink-0 items-center justify-center ${tone.muted}`}>
              {failedChecks > 0 ? (
                <XCircleFilled size={17} className={checkFailIconTone} />
              ) : runningChecks > 0 ? (
                <RunningCircleIcon size={17} className={isLight ? 'text-[#F59E0B]' : 'text-[#FBBF24]'} />
              ) : (
                <CheckCircleFilled size={17} className={checkPassIconTone} />
              )}
            </span>
            <span className={`min-w-0 flex-1 truncate whitespace-nowrap text-[12.5px] font-medium tracking-tight font-(family-name:--app-font) ${tone.title}`}>
              Checks
            </span>
            <ChecksCapsule checks={checks} isLight={isLight} />
          </div>
        </div>
      )}

      {/* MCPs Section */}
      {mcps.length > 0 && (
        <div className="flex shrink-0 flex-col">
          <div className={`mx-2 my-1 border-t ${tone.divider}`} />
          <SectionRow
            tone={tone}
            icon={<MaterialIcon name="hub" size={15} />}
            label="MCPs"
            open={openMcps}
            onToggle={() => setOpenMcps((v) => !v)}
            trailing={<span className={`text-[11.5px] tabular-nums ${tone.muted}`}>{mcps.length} used</span>}
          />

          <AnimatePresence initial={false}>
            {openMcps && (
              <motion.div
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: 'auto', opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                transition={TRANSITION}
                className="flex flex-col gap-0.5 overflow-hidden"
              >
                {mcps.map((mcp) => {
                  const isExpanded = Boolean(expandedServers[mcp.server]);
                  return (
                    <div key={mcp.server} className="flex flex-col">
                      <button
                        type="button"
                        onClick={() =>
                          setExpandedServers((prev) => ({
                            ...prev,
                            [mcp.server]: !prev[mcp.server],
                          }))
                        }
                        className={`flex h-7 w-full items-center gap-2 rounded-[8px] px-2 text-left ${tone.hover} transition-colors cursor-pointer select-none`}
                      >
                        <MaterialIcon name="hub" size={14} className={`${tone.muted} shrink-0`} />
                        <span className={`min-w-0 flex-1 truncate text-[12px] font-medium ${tone.body}`}>
                          {mcp.server}
                        </span>
                        <span className={`text-[11px] tabular-nums shrink-0 ${tone.faint}`}>
                          {mcp.calls} {mcp.calls === 1 ? 'call' : 'calls'}
                        </span>
                        <MaterialIcon
                          name="expand_more"
                          size={14}
                          className={`-mr-0.5 transition-transform duration-200 shrink-0 ${isExpanded ? 'rotate-180' : ''} ${tone.faint}`}
                        />
                      </button>

                      <AnimatePresence initial={false}>
                        {isExpanded && (
                          <motion.div
                            initial={{ height: 0, opacity: 0 }}
                            animate={{ height: 'auto', opacity: 1 }}
                            exit={{ height: 0, opacity: 0 }}
                            transition={TRANSITION}
                            className="overflow-hidden"
                          >
                            <div className={`mx-2 my-0.5 flex flex-col gap-0.5 border-l ${tone.divider} pl-2.5`}>
                              {mcp.items.map((item, idx) => {
                                const statusLabel = formatMcpStatus(item.status);
                                return (
                                  <div
                                    key={idx}
                                    className={`flex h-6 items-center justify-between gap-2 text-[11.5px] font-(family-name:--app-font) ${tone.muted}`}
                                  >
                                    <span className="flex min-w-0 flex-1 items-center gap-1.5">
                                      <span className={`truncate font-medium ${tone.body}`}>{item.action}</span>
                                      {item.target && (
                                        <span className={`truncate ${tone.faint}`} title={item.target}>
                                          {item.target}
                                        </span>
                                      )}
                                    </span>
                                    {statusLabel && (
                                      <span
                                        className={`text-[11px] shrink-0 ${
                                          statusLabel === 'Failed'
                                            ? tone.danger
                                            : statusLabel === 'Running'
                                              ? 'text-amber-400'
                                              : tone.faint
                                        }`}
                                      >
                                        {statusLabel}
                                      </span>
                                    )}
                                  </div>
                                );
                              })}
                            </div>
                          </motion.div>
                        )}
                      </AnimatePresence>
                    </div>
                  );
                })}
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      )}

      {/* Context & Session Usage Section */}
      {hasContextContent && contextUsage && (
        <div className="flex min-h-0 flex-col">
          <div className={`mx-2 my-1 border-t ${tone.divider}`} />
          <SectionRow
            tone={tone}
            icon={<ContextRingIcon ratio={contextUsage.window && contextUsage.window > 0 ? contextRatio : 0.35} />}
            label="Context"
            open={openContext}
            onToggle={() => setOpenContext((v) => !v)}
            trailing={
              contextUsage.tokens > 0 && contextUsage.window && contextUsage.window > 0 ? (
                <span className={`text-[11.5px] tabular-nums ${tone.muted}`}>{contextPct}%</span>
              ) : contextUsage.tokens > 0 ? (
                <span className={`text-[11.5px] tabular-nums ${tone.muted}`}>{formatTokens(contextUsage.tokens)}</span>
              ) : cumulativeTokens > 0 ? (
                <span className={`text-[11.5px] tabular-nums ${tone.muted}`}>{formatTokens(cumulativeTokens)}</span>
              ) : undefined
            }
          />

          <AnimatePresence initial={false}>
            {openContext && (
              <motion.div
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: 'auto', opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                transition={TRANSITION}
                className="flex flex-col gap-2 overflow-hidden px-2 pt-0.5 pb-1.5"
              >
                {contextUsage.tokens > 0 && (
                  <div className="flex flex-col gap-1.5">
                    {(() => {
                      const windowSize = contextUsage.window || 0;
                      if (windowSize <= 0) {
                        return (
                          <div className="flex items-center justify-between text-[11.5px]">
                            <span className={tone.muted}>Window</span>
                            <span className={`tabular-nums ${tone.body}`}>{formatTokens(contextUsage.tokens)} tokens</span>
                          </div>
                        );
                      }
                      const barColor =
                        contextPct >= 90
                          ? 'bg-rose-400/85'
                          : contextPct >= 80
                            ? 'bg-amber-400/85'
                            : isLight
                              ? 'bg-black/70'
                              : 'bg-white/75';
                      const trackBg = isLight ? 'bg-black/[0.08]' : 'bg-white/[0.08]';

                      return (
                        <>
                          <div className="flex items-center justify-between text-[11.5px] tabular-nums">
                            <span className={tone.body}>Window</span>
                            <span className={tone.faint}>
                              {formatTokens(contextUsage.tokens)} / {formatTokens(windowSize)}
                            </span>
                          </div>
                          <div className={`h-[3px] w-full rounded-full ${trackBg} overflow-hidden`}>
                            <div
                              className={`h-full rounded-full transition-all duration-300 ${barColor}`}
                              style={{ width: `${Math.max(2, Math.min(100, contextPct))}%` }}
                            />
                          </div>
                        </>
                      );
                    })()}
                  </div>
                )}

                {cumulativeTokens > 0 && (
                  <div className="flex flex-col gap-0.5">
                    <div className="flex items-center justify-between text-[11px] pb-0.5">
                      <span className={tone.muted}>Session</span>
                      <span className={`tabular-nums ${tone.faint}`}>{formatTokens(cumulativeTokens)} tokens</span>
                    </div>

                    {sessionTokenRows.map((row) => {
                      if (row.count <= 0) return null;
                      const share = cumulativeTokens > 0 ? (row.count / cumulativeTokens) * 100 : 0;
                      const shareText = share > 0 && share < 0.1 ? '<0.1%' : `${share.toFixed(1)}%`;
                      return (
                        <div
                          key={row.label}
                          className="flex h-6 items-center justify-between text-[11.5px]"
                        >
                          <span className={`truncate ${tone.muted}`}>{row.label}</span>
                          <div className="flex items-center gap-2 tabular-nums shrink-0">
                            <span className={tone.body}>{formatTokens(row.count)}</span>
                            <span className={`text-[10.5px] w-10 text-right ${tone.faint}`}>{shareText}</span>
                          </div>
                        </div>
                      );
                    })}

                    {contextUsage.sessionCostUsd !== undefined && contextUsage.sessionCostUsd > 0 && (
                      <div className="flex h-6 items-center justify-between text-[11px]">
                        <span className={tone.muted}>Estimated cost</span>
                        <span className={`tabular-nums font-medium ${tone.body}`}>
                          {contextUsage.sessionCostUsd < 0.01
                            ? '<$0.01'
                            : `$${contextUsage.sessionCostUsd.toFixed(2)}`}
                        </span>
                      </div>
                    )}
                  </div>
                )}
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      )}
    </div>
  );
};
