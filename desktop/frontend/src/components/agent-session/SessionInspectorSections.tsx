import React, { useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { MaterialIcon } from '../common/icons';
import { OutputPanel } from './WorkRow';
import { OrbitLoader } from './OrbitLoader';
import { formatTokens, ContextUsage } from './contextUsage';
import {
  SessionCheck,
  McpServerUsage,
  RepoChangeGroup,
  FileRepoScope,
  groupChangesByRepo,
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
  onOpenFile?: (path: string) => void;
}

const TRANSITION = { duration: 0.2, ease: [0.32, 0.72, 0, 1] } as const;

const ChevronIcon: React.FC<{ open: boolean; className?: string }> = ({ open, className = '' }) => (
  <MaterialIcon
    name="chevron_right"
    size={14}
    className={`transition-transform duration-200 shrink-0 ${open ? 'rotate-90' : 'rotate-0'} ${className}`}
  />
);

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

  const [openChanges, setOpenChanges] = useState(true);
  const [openChecks, setOpenChecks] = useState(true);
  const [openContext, setOpenContext] = useState(true);
  const [openMcps, setOpenMcps] = useState(true);

  const [expandedRepos, setExpandedRepos] = useState<Record<string, boolean>>({});
  const [expandedChecks, setExpandedChecks] = useState<Record<string, boolean>>({});
  const [expandedServers, setExpandedServers] = useState<Record<string, boolean>>({});
  const [showAllChecks, setShowAllChecks] = useState(false);

  const repoGroups = useMemo(
    () => groupChangesByRepo(changes, repoScopes, workspaceCwd),
    [changes, repoScopes, workspaceCwd],
  );

  const totalAdd = useMemo(() => changes.reduce((sum, c) => sum + c.additions, 0), [changes]);
  const totalDel = useMemo(() => changes.reduce((sum, c) => sum + c.deletions, 0), [changes]);

  const failedChecks = useMemo(() => checks.filter((c) => c.status === 'failed').length, [checks]);
  const runningChecks = useMemo(() => checks.filter((c) => c.status === 'running').length, [checks]);
  const passedChecks = useMemo(() => checks.filter((c) => c.status === 'passed').length, [checks]);

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

  const visibleChecks = showAllChecks ? checks : checks.slice(0, 5);

  const sessionTokenRows = useMemo(() => {
    if (!contextUsage) return [];
    return [
      { label: 'Cache read', count: contextUsage.sessionCacheRead ?? 0, dot: 'bg-sky-400' },
      { label: 'Cache write', count: contextUsage.sessionCacheWrite ?? 0, dot: 'bg-indigo-400' },
      { label: 'Output', count: contextUsage.sessionOutput ?? 0, dot: 'bg-emerald-400' },
      { label: 'Uncached input', count: contextUsage.sessionInput ?? 0, dot: 'bg-amber-400' },
    ];
  }, [contextUsage]);

  return (
    <div className="flex min-h-0 flex-col font-(family-name:--app-font)">
      {/* Changes Section */}
      {changes.length > 0 && (
        <div className={`flex min-h-0 flex-col p-2 border-t ${tone.divider}`}>
          <div
            role="button"
            tabIndex={0}
            onClick={() => setOpenChanges((v) => !v)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') setOpenChanges((v) => !v);
            }}
            className="flex items-center gap-2 px-2 pb-1.5 pt-1.5 cursor-pointer select-none group/hdr"
          >
            <span className={`flex items-center gap-1.5 text-[12px] font-medium ${tone.muted} group-hover/hdr:${tone.title}`}>
              <ChevronIcon open={openChanges} />
              <span>Changes</span>
            </span>
            <span className={`h-px flex-1 ${tone.divider} border-t`} />
            <div className="flex items-center gap-1.5 text-[11px] tabular-nums font-(family-name:--app-font)">
              <span className={tone.faint}>
                {changes.length} {changes.length === 1 ? 'file' : 'files'}
              </span>
              {(totalAdd > 0 || totalDel > 0) && (
                <span className="flex items-center gap-1 ml-0.5">
                  {totalAdd > 0 && <span className="text-emerald-500">+{totalAdd}</span>}
                  {totalDel > 0 && <span className="text-rose-500">−{totalDel}</span>}
                </span>
              )}
            </div>
          </div>

          <AnimatePresence initial={false}>
            {openChanges && (
              <motion.div
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: 'auto', opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                transition={TRANSITION}
                className="flex flex-col gap-1 overflow-hidden pt-1 px-1"
              >
                {repoGroups.map((group) => {
                  const isExpanded = Boolean(expandedRepos[group.key]);
                  return (
                    <div
                      key={group.key}
                      className={`flex flex-col rounded-md border p-1.5 transition-colors ${tone.card}`}
                    >
                      <div
                        role="button"
                        tabIndex={0}
                        onClick={() =>
                          setExpandedRepos((prev) => ({ ...prev, [group.key]: !prev[group.key] }))
                        }
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            setExpandedRepos((prev) => ({ ...prev, [group.key]: !prev[group.key] }));
                          }
                        }}
                        className="flex items-center justify-between gap-1.5 cursor-pointer select-none"
                      >
                        <div className="flex items-center gap-1.5 min-w-0">
                          <MaterialIcon
                            name={group.isGit ? 'account_tree' : 'folder'}
                            size={14}
                            className="text-current/60 shrink-0"
                          />
                          <span className={`text-[12px] font-medium truncate max-w-[110px] ${tone.title}`}>
                            {group.name}
                          </span>
                          {group.branch && (
                            <span className="px-1.5 py-0.2 rounded text-[9.5px] bg-sky-500/10 text-sky-400 border border-sky-500/20 truncate max-w-[85px] font-mono leading-tight">
                              {group.branch}
                            </span>
                          )}
                        </div>
                        <div className="flex items-center gap-1.5 text-[10.5px] tabular-nums shrink-0">
                          {group.additions > 0 && (
                            <span className="text-emerald-500">+{group.additions}</span>
                          )}
                          {group.deletions > 0 && (
                            <span className="text-rose-500">−{group.deletions}</span>
                          )}
                          <span className={tone.faint}>
                            {group.fileCount} {group.fileCount === 1 ? 'file' : 'files'}
                          </span>
                          <ChevronIcon open={isExpanded} className={tone.faint} />
                        </div>
                      </div>

                      <AnimatePresence initial={false}>
                        {isExpanded && (
                          <motion.div
                            initial={{ height: 0, opacity: 0 }}
                            animate={{ height: 'auto', opacity: 1 }}
                            exit={{ height: 0, opacity: 0 }}
                            transition={TRANSITION}
                            className="flex flex-col overflow-hidden pt-1.5 pl-1 border-t border-current/5 mt-1.5"
                          >
                            <div className="flex flex-col gap-0.5">
                              {group.files.map((file) => {
                                const basename = file.path.split(/[/\\]/).pop() || file.path;
                                return (
                                  <div
                                    key={file.key}
                                    role="button"
                                    tabIndex={0}
                                    onClick={() => {
                                      if (onOpenFile) onOpenFile(file.path);
                                      else if (onOpenChanges) onOpenChanges(group);
                                    }}
                                    onKeyDown={(e) => {
                                      if (e.key === 'Enter' || e.key === ' ') {
                                        if (onOpenFile) onOpenFile(file.path);
                                        else if (onOpenChanges) onOpenChanges(group);
                                      }
                                    }}
                                    className={`flex items-center justify-between text-[11px] py-0.5 px-1 rounded cursor-pointer transition-colors ${tone.hover}`}
                                  >
                                    <span
                                      className={`truncate flex-1 font-mono text-[10.5px] ${tone.body}`}
                                      title={file.path}
                                    >
                                      {basename}
                                    </span>
                                    <span className="flex items-center gap-1 text-[10px] tabular-nums shrink-0 ml-2">
                                      {file.additions > 0 && (
                                        <span className="text-emerald-500">+{file.additions}</span>
                                      )}
                                      {file.deletions > 0 && (
                                        <span className="text-rose-500">−{file.deletions}</span>
                                      )}
                                    </span>
                                  </div>
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
                                className={`mt-1 flex items-center gap-1 px-2 py-1 rounded text-[10.5px] font-medium cursor-pointer transition-colors self-start ${tone.hover} ${tone.muted}`}
                              >
                                <MaterialIcon name="difference" size={13} />
                                <span>View full diff</span>
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

      {/* Checks Section */}
      {checks.length > 0 && (
        <div className={`flex min-h-0 flex-col p-2 border-t ${tone.divider}`}>
          <div
            role="button"
            tabIndex={0}
            onClick={() => setOpenChecks((v) => !v)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') setOpenChecks((v) => !v);
            }}
            className="flex items-center gap-2 px-2 pb-1.5 pt-1.5 cursor-pointer select-none group/hdr"
          >
            <span className={`flex items-center gap-1.5 text-[12px] font-medium ${tone.muted} group-hover/hdr:${tone.title}`}>
              <ChevronIcon open={openChecks} />
              <span>Checks</span>
            </span>
            <span className={`h-px flex-1 ${tone.divider} border-t`} />
            <div className="flex items-center gap-1.5 text-[11px] tabular-nums font-(family-name:--app-font)">
              {failedChecks > 0 ? (
                <span className="text-rose-500 font-medium">{failedChecks} failed</span>
              ) : runningChecks > 0 ? (
                <span className="text-amber-400 font-medium animate-pulse">{runningChecks} running</span>
              ) : (
                <span className={tone.faint}>
                  {passedChecks}/{checks.length} passed
                </span>
              )}
            </div>
          </div>

          <AnimatePresence initial={false}>
            {openChecks && (
              <motion.div
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: 'auto', opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                transition={TRANSITION}
                className="flex flex-col gap-1 overflow-hidden pt-1 px-1"
              >
                {visibleChecks.map((check) => {
                  const isExpanded = Boolean(expandedChecks[check.id]);
                  const hasOutput = Boolean(check.output && check.output.trim().length > 0);
                  return (
                    <div
                      key={check.id}
                      className={`flex flex-col rounded-md border p-1.5 transition-colors ${tone.card}`}
                    >
                      <div
                        role="button"
                        tabIndex={0}
                        onClick={() => {
                          if (hasOutput) {
                            setExpandedChecks((prev) => ({ ...prev, [check.id]: !prev[check.id] }));
                          }
                        }}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            if (hasOutput) {
                              setExpandedChecks((prev) => ({ ...prev, [check.id]: !prev[check.id] }));
                            }
                          }
                        }}
                        className={`flex items-center justify-between gap-2 select-none ${
                          hasOutput ? 'cursor-pointer' : 'cursor-default'
                        }`}
                      >
                        <div className="flex items-center gap-1.5 min-w-0 flex-1">
                          {check.status === 'running' ? (
                            <OrbitLoader size={12} />
                          ) : check.status === 'failed' ? (
                            <MaterialIcon name="cancel" size={13} className="text-rose-500 shrink-0" />
                          ) : (
                            <MaterialIcon name="check_circle" size={13} className="text-emerald-500 shrink-0" />
                          )}
                          <span
                            className={`truncate font-mono text-[11px] ${tone.body}`}
                            title={check.command}
                          >
                            {check.label}
                          </span>
                        </div>
                        <div className="flex items-center gap-1.5 shrink-0 text-[10.5px]">
                          <span
                            className={`capitalize ${
                              check.status === 'failed'
                                ? 'text-rose-500 font-medium'
                                : check.status === 'running'
                                  ? 'text-amber-400 font-medium'
                                  : 'text-emerald-500'
                            }`}
                          >
                            {check.status === 'passed'
                              ? 'Passed'
                              : check.status === 'failed'
                                ? 'Failed'
                                : 'Running'}
                          </span>
                          {hasOutput && <ChevronIcon open={isExpanded} className={tone.faint} />}
                        </div>
                      </div>

                      {hasOutput && (
                        <AnimatePresence initial={false}>
                          {isExpanded && (
                            <motion.div
                              initial={{ height: 0, opacity: 0 }}
                              animate={{ height: 'auto', opacity: 1 }}
                              exit={{ height: 0, opacity: 0 }}
                              transition={TRANSITION}
                              className="overflow-hidden pt-1.5"
                            >
                              <OutputPanel text={check.output!} />
                            </motion.div>
                          )}
                        </AnimatePresence>
                      )}
                    </div>
                  );
                })}

                {checks.length > 5 && (
                  <button
                    type="button"
                    onClick={() => setShowAllChecks((v) => !v)}
                    className={`mt-0.5 self-center text-[11px] font-medium cursor-pointer transition-colors ${tone.faint} hover:${tone.title}`}
                  >
                    {showAllChecks ? 'Show less' : `Show ${checks.length - 5} more checks`}
                  </button>
                )}
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      )}

      {/* Context & Session Usage Section */}
      {hasContextContent && contextUsage && (
        <div className={`flex min-h-0 flex-col p-2 border-t ${tone.divider}`}>
          <div
            role="button"
            tabIndex={0}
            onClick={() => setOpenContext((v) => !v)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') setOpenContext((v) => !v);
            }}
            className="flex items-center gap-2 px-2 pb-1.5 pt-1.5 cursor-pointer select-none group/hdr"
          >
            <span className={`flex items-center gap-1.5 text-[12px] font-medium ${tone.muted} group-hover/hdr:${tone.title}`}>
              <ChevronIcon open={openContext} />
              <span>Context & Usage</span>
            </span>
            <span className={`h-px flex-1 ${tone.divider} border-t`} />
            <div className="flex items-center gap-1.5 text-[11px] tabular-nums font-(family-name:--app-font)">
              {contextUsage.tokens > 0 && contextUsage.window ? (
                <span className={tone.faint}>
                  {Math.round((contextUsage.tokens / contextUsage.window) * 100)}%
                </span>
              ) : cumulativeTokens > 0 ? (
                <span className={tone.faint}>{formatTokens(cumulativeTokens)}</span>
              ) : null}
            </div>
          </div>

          <AnimatePresence initial={false}>
            {openContext && (
              <motion.div
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: 'auto', opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                transition={TRANSITION}
                className="flex flex-col overflow-hidden pt-1 px-1.5"
              >
                {/* Active Context Bar */}
                {contextUsage.tokens > 0 && (
                  <div className="flex flex-col pb-2">
                    {(() => {
                      const windowSize = contextUsage.window || 0;
                      const ratio = windowSize > 0 ? Math.min(1, contextUsage.tokens / windowSize) : 0;
                      const pct = Math.round(ratio * 100);
                      return (
                        <>
                          <div className="flex items-center justify-between text-[11px]">
                            <span className={`font-medium ${tone.body}`}>{pct}% used</span>
                            <span className={`tabular-nums ${tone.faint}`}>
                              {formatTokens(contextUsage.tokens)}
                              {windowSize > 0 ? ` / ${formatTokens(windowSize)}` : ''}
                            </span>
                          </div>
                          <div className="h-1.5 w-full rounded-full bg-current/10 overflow-hidden mt-1">
                            <div
                              className={`h-full rounded-full transition-all duration-300 ${
                                pct >= 90 ? 'bg-rose-500' : pct >= 70 ? 'bg-amber-400' : 'bg-blue-500'
                              }`}
                              style={{ width: `${Math.max(2, Math.min(100, pct))}%` }}
                            />
                          </div>
                        </>
                      );
                    })()}
                  </div>
                )}

                {/* Cumulative Session Usage Breakdown */}
                {cumulativeTokens > 0 && (
                  <div className={`flex flex-col pt-1.5 ${contextUsage.tokens > 0 ? `border-t ${tone.divider}` : ''}`}>
                    <div className="flex items-center justify-between text-[11px] pb-1">
                      <span className={`font-medium ${tone.muted}`}>Session usage</span>
                      <span className={`tabular-nums ${tone.faint}`}>{formatTokens(cumulativeTokens)} tokens</span>
                    </div>

                    <div className="flex flex-col gap-0.5">
                      {sessionTokenRows.map((row) => {
                        if (row.count <= 0 && cumulativeTokens > 0) return null;
                        const share = cumulativeTokens > 0 ? (row.count / cumulativeTokens) * 100 : 0;
                        const shareText = share > 0 && share < 0.1 ? '<0.1%' : `${share.toFixed(1)}%`;
                        return (
                          <div key={row.label} className="flex items-center justify-between text-[11px] py-0.5">
                            <div className="flex items-center gap-1.5 min-w-0">
                              <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${row.dot}`} />
                              <span className={`truncate ${tone.muted}`}>{row.label}</span>
                            </div>
                            <div className="flex items-center gap-2 tabular-nums shrink-0">
                              <span className={tone.body}>{formatTokens(row.count)}</span>
                              <span className={`text-[10px] w-10 text-right ${tone.faint}`}>{shareText}</span>
                            </div>
                          </div>
                        );
                      })}
                    </div>

                    {contextUsage.sessionCostUsd !== undefined && contextUsage.sessionCostUsd > 0 && (
                      <div className={`flex items-center justify-between text-[10.5px] pt-1.5 mt-1 border-t ${tone.divider} ${tone.faint}`}>
                        <span>Estimated cost</span>
                        <span className="tabular-nums font-mono font-medium">
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

      {/* MCPs Section */}
      {mcps.length > 0 && (
        <div className={`flex min-h-0 flex-col p-2 border-t ${tone.divider}`}>
          <div
            role="button"
            tabIndex={0}
            onClick={() => setOpenMcps((v) => !v)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') setOpenMcps((v) => !v);
            }}
            className="flex items-center gap-2 px-2 pb-1.5 pt-1.5 cursor-pointer select-none group/hdr"
          >
            <span className={`flex items-center gap-1.5 text-[12px] font-medium ${tone.muted} group-hover/hdr:${tone.title}`}>
              <ChevronIcon open={openMcps} />
              <span>MCPs</span>
            </span>
            <span className={`h-px flex-1 ${tone.divider} border-t`} />
            <span className={`text-[11px] tabular-nums ${tone.faint}`}>
              {mcps.length} used
            </span>
          </div>

          <AnimatePresence initial={false}>
            {openMcps && (
              <motion.div
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: 'auto', opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                transition={TRANSITION}
                className="flex flex-col gap-1 overflow-hidden pt-1 px-1"
              >
                {mcps.map((mcp) => {
                  const isExpanded = Boolean(expandedServers[mcp.server]);
                  return (
                    <div
                      key={mcp.server}
                      className={`flex flex-col rounded-md border p-1.5 transition-colors ${tone.card}`}
                    >
                      <div
                        role="button"
                        tabIndex={0}
                        onClick={() =>
                          setExpandedServers((prev) => ({
                            ...prev,
                            [mcp.server]: !prev[mcp.server],
                          }))
                        }
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            setExpandedServers((prev) => ({
                              ...prev,
                              [mcp.server]: !prev[mcp.server],
                            }));
                          }
                        }}
                        className="flex items-center justify-between gap-2 cursor-pointer select-none"
                      >
                        <div className="flex items-center gap-1.5 min-w-0">
                          <MaterialIcon name="hub" size={13} className="text-current/60 shrink-0" />
                          <span className={`text-[12px] font-medium truncate ${tone.title}`}>
                            {mcp.server}
                          </span>
                        </div>
                        <div className="flex items-center gap-1.5 text-[10.5px] tabular-nums shrink-0">
                          <span className={tone.faint}>
                            {mcp.calls} {mcp.calls === 1 ? 'call' : 'calls'}
                          </span>
                          <ChevronIcon open={isExpanded} className={tone.faint} />
                        </div>
                      </div>

                      <AnimatePresence initial={false}>
                        {isExpanded && (
                          <motion.div
                            initial={{ height: 0, opacity: 0 }}
                            animate={{ height: 'auto', opacity: 1 }}
                            exit={{ height: 0, opacity: 0 }}
                            transition={TRANSITION}
                            className="flex flex-col overflow-hidden pt-1.5 pl-1 border-t border-current/5 mt-1.5 gap-1"
                          >
                            {mcp.items.map((item, idx) => (
                              <div
                                key={idx}
                                className={`flex items-center gap-1.5 text-[10.5px] py-0.5 font-mono ${tone.muted}`}
                              >
                                <span className="w-1 h-1 rounded-full bg-current/40 shrink-0" />
                                <span className={`font-semibold ${tone.body}`}>{item.action}</span>
                                {item.target && (
                                  <span className="truncate flex-1 opacity-75" title={item.target}>
                                    {item.target}
                                  </span>
                                )}
                                {item.status && (
                                  <span className="text-[9.5px] uppercase opacity-60 shrink-0">
                                    {item.status}
                                  </span>
                                )}
                              </div>
                            ))}
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
    </div>
  );
};
