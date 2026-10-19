import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { ActiveProject, InstructionSources, ListProjects, OpenPath, RevealPath } from '../../../wailsjs/go/main/App';
import { instructions, projects } from '../../../wailsjs/go/models';
import { CleanDropdown } from '../common/CleanDropdown';
import { ChevronRight, CircleAlert, FolderOpen, RefreshCw, SquareArrowOutUpRight } from '../common/icons';
import { providerIcon } from '../orchestrator/providerIcons';
import { SettingsGroup } from './SettingsParts';

const GLOBAL_ONLY = '__global__';
const GROUP_ORDER = ['Global', 'This project', 'Auto memory', 'Added by this app'];

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(bytes < 10240 ? 1 : 0)} KB`;
}

function formatAge(modified?: number): string {
  if (!modified) return '';
  const minutes = Math.round((Date.now() - modified) / 60000);
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return `${hours}h ago`;
  return new Date(modified).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

function activeCount(source: instructions.Source): number {
  return source.files.filter((file) => file.exists).length;
}

function FileRow({ file, isLight, open, onToggle }: { file: instructions.File; isLight: boolean; open: boolean; onToggle: () => void }) {
  const [error, setError] = useState<string | null>(null);
  const muted = isLight ? 'text-black/45' : 'text-white/40';
  const strong = isLight ? 'text-black/90' : 'text-white/90';
  const chip = isLight ? 'bg-black/[0.06] text-black/60' : 'bg-white/[0.08] text-white/60';
  const button = `h-[26px] px-2.5 rounded-[8px] flex items-center gap-1.5 text-[11.5px] font-medium tracking-tight cursor-pointer transition-colors duration-150 active:scale-95 ${
    isLight ? 'bg-black/[0.05] hover:bg-black/[0.09] text-black/70' : 'bg-white/[0.07] hover:bg-white/[0.12] text-white/75'
  }`;
  const expandable = file.exists && Boolean(file.content);

  const run = async (action: (path: string) => Promise<void>) => {
    if (!file.path) return;
    setError(null);
    try {
      await action(file.path);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  };

  return (
    <div className="flex flex-col">
      <button
        type="button"
        onClick={expandable ? onToggle : undefined}
        className={`flex items-start gap-2.5 px-3.5 py-2.5 text-left ${expandable ? 'cursor-pointer' : 'cursor-default'} ${
          expandable ? (isLight ? 'hover:bg-black/[0.025]' : 'hover:bg-white/[0.025]') : ''
        }`}
      >
        <span className={`mt-[3px] shrink-0 transition-transform duration-150 ${open ? 'rotate-90' : ''} ${expandable ? muted : 'opacity-0'}`}>
          <ChevronRight size={12} strokeWidth={2} />
        </span>
        <div className="flex flex-col min-w-0 flex-1 gap-0.5">
          <div className="flex items-center gap-1.5 min-w-0">
            {file.warning && (
              <span className="text-amber-400 shrink-0">
                <CircleAlert size={13} strokeWidth={2} />
              </span>
            )}
            <span className={`text-[12.5px] font-medium tracking-tight truncate ${file.exists ? strong : muted}`}>{file.label}</span>
            {file.account && <span className={`shrink-0 text-[10px] font-medium px-1.5 py-[1px] rounded-[5px] ${chip}`}>{file.account}</span>}
          </div>
          {file.note && (
            <span className={`text-[11px] tracking-tight leading-snug ${file.warning ? (isLight ? 'text-amber-700' : 'text-amber-300/90') : muted}`}>{file.note}</span>
          )}
          {file.path && <span className={`text-[10.5px] tracking-tight truncate font-mono ${isLight ? 'text-black/30' : 'text-white/25'}`}>{file.path}</span>}
        </div>
        <span className={`shrink-0 text-[11px] tracking-tight tabular-nums mt-[1px] ${muted}`}>
          {file.exists ? (file.path ? `${formatSize(file.size)}${file.modified ? ` · ${formatAge(file.modified)}` : ''}` : '') : 'Not created'}
        </span>
      </button>
      <AnimatePresence initial={false}>
        {open && expandable && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
            className="overflow-hidden"
          >
            <div className="px-3.5 pb-3 pl-[34px] flex flex-col gap-2">
              <pre
                className={`max-h-[320px] overflow-auto rounded-[10px] px-3 py-2.5 text-[11.5px] leading-[1.55] font-mono whitespace-pre-wrap break-words select-text ${
                  isLight ? 'bg-black/[0.04] text-black/80' : 'bg-black/30 text-white/80'
                }`}
              >
                {file.content}
                {file.truncated && <span className={muted}>{'\n\n'}… file is larger than 256 KB, open it to see the rest</span>}
              </pre>
              {file.path && (
                <div className="flex items-center gap-1.5">
                  <button type="button" className={button} onClick={() => void run(OpenPath)}>
                    <SquareArrowOutUpRight size={12} strokeWidth={2} />
                    Open
                  </button>
                  <button type="button" className={button} onClick={() => void run(RevealPath)}>
                    <FolderOpen size={12} strokeWidth={2} />
                    Show in folder
                  </button>
                  {error && <span className="text-[11px] text-red-400 truncate">{error}</span>}
                </div>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

export function InstructionsSection({ isLight }: { isLight: boolean }) {
  const [projectList, setProjectList] = useState<projects.Project[]>([]);
  const [scope, setScope] = useState<string | null>(null);
  const [sources, setSources] = useState<instructions.Source[]>([]);
  const [driver, setDriver] = useState('claude');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [openId, setOpenId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void Promise.all([ListProjects(), ActiveProject()])
      .then(([saved, active]) => {
        if (cancelled) return;
        setProjectList(saved ?? []);
        setScope(active?.path ?? GLOBAL_ONLY);
      })
      .catch(() => {
        if (!cancelled) setScope(GLOBAL_ONLY);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const load = useCallback(async () => {
    if (scope === null) return;
    setLoading(true);
    try {
      setSources((await InstructionSources(scope === GLOBAL_ONLY ? '' : scope)) ?? []);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, [scope]);

  useEffect(() => {
    void load();
  }, [load]);

  const options = useMemo(
    () => [{ value: GLOBAL_ONLY, label: 'Global only' }, ...projectList.map((project) => ({ value: project.path, label: project.name }))],
    [projectList],
  );

  const source = sources.find((item) => item.driver === driver);
  const groups = useMemo(() => {
    const files = source?.files ?? [];
    const names = [...GROUP_ORDER, ...files.map((file) => file.group).filter((group) => !GROUP_ORDER.includes(group))];
    return names
      .map((name) => ({ name, files: files.filter((file) => file.group === name) }))
      .filter((group) => group.files.length > 0);
  }, [source]);

  const warnings = sources.flatMap((item) => item.files.filter((file) => file.warning).map((file) => ({ driver: item.driver, name: item.name, file })));
  const muted = isLight ? 'text-black/45' : 'text-white/40';

  return (
    <>
      <span className={`text-[12px] tracking-tight leading-snug ${muted}`}>
        Everything that gets added to an agent's context before it reads your message: instruction files, saved memory, and what this app sends itself.
      </span>

      <div className="flex items-center gap-2">
        <div className="flex-1 min-w-0">
          <CleanDropdown value={scope ?? GLOBAL_ONLY} options={options} onChange={(value) => setScope(value)} />
        </div>
        <button
          type="button"
          title="Refresh"
          onClick={() => void load()}
          className={`w-[34px] h-[34px] shrink-0 rounded-[11px] flex items-center justify-center cursor-pointer transition-colors duration-150 active:scale-90 ${
            isLight ? 'bg-black/[0.05] hover:bg-black/[0.09] text-black/60' : 'bg-white/[0.07] hover:bg-white/[0.12] text-white/70'
          }`}
        >
          <span className={loading ? 'animate-spin' : ''}>
            <RefreshCw size={14} strokeWidth={2} />
          </span>
        </button>
      </div>

      {warnings.length > 0 && (
        <div className={`flex flex-col gap-1 px-3 py-2.5 rounded-[12px] border ${isLight ? 'bg-amber-500/10 border-amber-500/25' : 'bg-amber-500/10 border-amber-400/20'}`}>
          {warnings.map(({ driver: owner, name, file }) => (
            <button
              key={file.id}
              type="button"
              onClick={() => {
                setDriver(owner);
                setOpenId(file.id);
              }}
              className={`flex items-start gap-2 text-left text-[11.5px] leading-snug cursor-pointer ${isLight ? 'text-amber-800' : 'text-amber-200/90'}`}
            >
              <span className="mt-[2px] shrink-0">
                <CircleAlert size={12} strokeWidth={2} />
              </span>
              <span>
                <span className="font-semibold">{name}{file.account ? ` · ${file.account}` : ''}:</span> {file.note}
              </span>
            </button>
          ))}
        </div>
      )}

      <div className={`grid grid-cols-4 gap-1 p-1 rounded-[13px] ${isLight ? 'bg-black/[0.04]' : 'bg-white/[0.04]'}`}>
        {sources.map((item) => {
          const selected = item.driver === driver;
          const icon = providerIcon(item.driver, isLight);
          return (
            <button
              key={item.driver}
              type="button"
              onClick={() => setDriver(item.driver)}
              className={`relative h-[32px] px-2 rounded-[10px] flex items-center justify-center gap-1.5 text-[12px] font-medium tracking-tight cursor-pointer transition-colors duration-150 ${
                selected ? (isLight ? 'text-black' : 'text-white') : isLight ? 'text-black/50 hover:text-black/80' : 'text-white/45 hover:text-white/80'
              }`}
            >
              {selected && (
                <motion.span
                  layoutId="instructions-provider-pill"
                  className={`absolute inset-0 rounded-[10px] ${isLight ? 'bg-white shadow-sm' : 'bg-white/[0.1]'}`}
                  transition={{ type: 'spring', stiffness: 520, damping: 40 }}
                />
              )}
              <span className="relative flex items-center gap-1.5 min-w-0">
                {icon && <img src={icon} alt="" className="w-[14px] h-[14px] object-contain" draggable={false} />}
                <span className="truncate">{item.name}</span>
                <span className={`tabular-nums text-[10.5px] ${muted}`}>{activeCount(item)}</span>
              </span>
            </button>
          );
        })}
      </div>

      {error && <span className="text-[11.5px] text-red-400">{error}</span>}

      {groups.map((group) => (
        <SettingsGroup key={group.name} title={group.name} isLight={isLight}>
          {group.files.map((file) => (
            <FileRow
              key={file.id}
              file={file}
              isLight={isLight}
              open={openId === file.id}
              onToggle={() => setOpenId((current) => (current === file.id ? null : file.id))}
            />
          ))}
        </SettingsGroup>
      ))}

      {source && groups.length === 0 && !loading && (
        <span className={`text-[12px] ${muted}`}>Nothing loaded for {source.name}.</span>
      )}
    </>
  );
}
