import React, { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { AnimatePresence, motion } from 'motion/react';
import { useTheme } from '../../themes';

export interface StatFile {
  path: string;
  additions: number;
  deletions: number;
}

export type ChangeKind = 'Code' | 'Tests' | 'Mocks' | 'Styles' | 'Docs' | 'Locales' | 'Config' | 'Generated' | 'Assets' | 'Other';

const ORDER: ChangeKind[] = ['Code', 'Tests', 'Mocks', 'Styles', 'Docs', 'Locales', 'Config', 'Generated', 'Assets', 'Other'];

const GENERATED_NAMES = new Set([
  'package-lock.json',
  'yarn.lock',
  'pnpm-lock.yaml',
  'bun.lockb',
  'bun.lock',
  'go.sum',
  'cargo.lock',
  'poetry.lock',
  'composer.lock',
  'gemfile.lock',
  'uv.lock',
]);
const GENERATED_DIR = /(^|\/)(dist|build|out|vendor|node_modules|wailsjs|generated|__generated__|\.next|coverage)\//;
const GENERATED_FILE = /(\.min\.(js|css)|\.map|\.pb\.go|_generated\.[a-z]+|\.g\.dart|\.gen\.[a-z]+)$/;
const MOCK_DIR = /(^|\/)(__mocks__|mocks?|fixtures?|__fixtures__|testdata|stubs?|__snapshots__)\//;
const MOCK_FILE = /(\.(mocks?|fixtures?|stubs?)\.[a-z0-9]+|\.snap)$/;
const TEST_DIR = /(^|\/)(__tests__|tests?|specs?|e2e|cypress|playwright|evals?)\//;
const TEST_FILE = /([._-](test|spec)s?\.[a-z0-9]+|_test\.go|(^|\/)test_[^/]+\.py|(^|\/)conftest\.py)$/;
const LOCALE_DIR = /(^|\/)(lang|langs|locales?|i18n|l10n|translations?)\//;
const DOC_EXT = /\.(md|mdx|markdown|rst|adoc|txt)$/;
const DOC_DIR = /(^|\/)docs?\//;
const DOC_NAMES = /(^|\/)(license|licence|changelog|authors|contributing|notice)(\.[a-z]+)?$/;
const ASSET_EXT = /\.(png|jpe?g|gif|svg|webp|ico|avif|bmp|tiff?|mp3|mp4|wav|ogg|webm|mov|woff2?|ttf|otf|eot|glb|gltf|obj|fbx|hdr|exr|pdf|zip)$/;
const STYLE_EXT = /\.(css|scss|sass|less|styl|pcss)$/;
const CONFIG_EXT = /\.(json|jsonc|json5|ya?ml|toml|ini|cfg|conf|env|xml|plist|properties|editorconfig|lock)$/;
const CONFIG_NAMES = /(^|\/)(dockerfile|makefile|procfile|justfile|go\.mod|go\.work|\.[^/]+|[^/]+\.config\.[a-z]+|[^/]*rc(\.[a-z]+)?)$/;
const CONFIG_SCRIPT = /(^|\/)([^/]+\.config\.[cm]?[jt]s|\.[^/]+rc\.[cm]?[jt]s)$/;
const CODE_EXT = /\.(tsx?|jsx?|mjs|cjs|mts|cts|go|py|rs|java|kt|kts|swift|c|cc|cpp|cxx|h|hh|hpp|cs|fs|rb|php|vue|svelte|astro|sql|sh|bash|zsh|ps1|psm1|lua|dart|scala|ex|exs|erl|hs|clj|elm|zig|nim|html?|glsl|wgsl|hlsl|proto|graphql|gql|r|m|mm|pl)$/;

export function kindOf(rawPath: string): ChangeKind {
  const path = rawPath.replace(/\\/g, '/').toLowerCase();
  const name = path.split('/').pop() ?? path;
  if (GENERATED_NAMES.has(name) || GENERATED_DIR.test(path) || GENERATED_FILE.test(path)) return 'Generated';
  if (MOCK_DIR.test(path) || MOCK_FILE.test(path)) return 'Mocks';
  if (TEST_DIR.test(path) || TEST_FILE.test(path)) return 'Tests';
  if (DOC_EXT.test(path) || DOC_NAMES.test(path) || (DOC_DIR.test(path) && !CODE_EXT.test(path))) return 'Docs';
  if (LOCALE_DIR.test(path) && !CODE_EXT.test(path)) return 'Locales';
  if (ASSET_EXT.test(path)) return 'Assets';
  if (STYLE_EXT.test(path)) return 'Styles';
  if (CONFIG_SCRIPT.test(path)) return 'Config';
  if (CODE_EXT.test(path)) return 'Code';
  if (CONFIG_EXT.test(path) || CONFIG_NAMES.test(path)) return 'Config';
  return 'Other';
}

export interface BreakdownRow {
  kind: ChangeKind;
  files: number;
  additions: number;
  deletions: number;
}

export function breakdown(files: StatFile[]): { rows: BreakdownRow[]; total: BreakdownRow } {
  const byKind = new Map<ChangeKind, BreakdownRow>();
  for (const file of files) {
    const kind = kindOf(file.path);
    const row = byKind.get(kind) ?? { kind, files: 0, additions: 0, deletions: 0 };
    row.files += 1;
    row.additions += file.additions || 0;
    row.deletions += file.deletions || 0;
    byKind.set(kind, row);
  }
  const rows = ORDER.map((kind) => byKind.get(kind)).filter((row): row is BreakdownRow => Boolean(row));
  const total = rows.reduce<BreakdownRow>(
    (sum, row) => ({
      kind: 'Other',
      files: sum.files + row.files,
      additions: sum.additions + row.additions,
      deletions: sum.deletions + row.deletions,
    }),
    { kind: 'Other', files: 0, additions: 0, deletions: 0 },
  );
  return { rows, total };
}

const number = new Intl.NumberFormat('en-US');
const POPOVER_WIDTH = 236;
const GAP = 6;

export const BreakdownHover: React.FC<{
  files: StatFile[];
  caption?: string;
  className?: string;
  children: React.ReactNode;
}> = ({ files, caption, className = '', children }) => {
  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';
  const anchor = useRef<HTMLSpanElement>(null);
  const timer = useRef<number | null>(null);
  const [open, setOpen] = useState(false);
  const [place, setPlace] = useState<{ left: number; top?: number; bottom?: number; above: boolean } | null>(null);
  const data = useMemo(() => (open ? breakdown(files) : null), [open, files]);

  const clear = () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = null;
  };
  const show = useCallback(() => {
    clear();
    timer.current = window.setTimeout(() => setOpen(true), 180);
  }, []);
  const hide = useCallback(() => {
    clear();
    timer.current = window.setTimeout(() => setOpen(false), 80);
  }, []);

  useLayoutEffect(() => {
    if (!open || !anchor.current) return;
    const rect = anchor.current.getBoundingClientRect();
    const left = Math.min(Math.max(8, rect.right - POPOVER_WIDTH), window.innerWidth - POPOVER_WIDTH - 8);
    const above = rect.bottom + 240 > window.innerHeight && rect.top > 240;
    setPlace(above ? { left, bottom: window.innerHeight - rect.top + GAP, above } : { left, top: rect.bottom + GAP, above });
  }, [open]);

  useLayoutEffect(() => () => clear(), []);

  if (files.length === 0) return <>{children}</>;

  const cell = 'w-14 text-right tabular-nums';
  return (
    <span ref={anchor} className={`inline-flex ${className}`} onMouseEnter={show} onMouseLeave={hide}>
      {children}
      {createPortal(
        <AnimatePresence>
          {open && place && data && (
            <motion.div
              initial={{ opacity: 0, y: place.above ? 4 : -4, scale: 0.98 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: place.above ? 4 : -4, scale: 0.98 }}
              transition={{ duration: 0.14, ease: [0.32, 0.72, 0, 1] }}
              onMouseEnter={show}
              onMouseLeave={hide}
              style={{
                position: 'fixed',
                left: place.left,
                top: place.top,
                bottom: place.bottom,
                width: POPOVER_WIDTH,
              }}
              className={`z-[9999] rounded-2xl border px-3.5 py-3 text-[12.5px] shadow-xl font-(family-name:--app-font) ${
                isLight ? 'border-black/10 bg-white text-black/80 shadow-black/10' : 'border-white/10 bg-[#1c1c20] text-white/85 shadow-black/50'
              }`}
            >
              <div className="flex flex-col">
                {caption && (
                  <div className={`mb-2 truncate whitespace-nowrap text-[11px] ${isLight ? 'text-black/45' : 'text-white/45'}`}>{caption}</div>
                )}
                {data.rows.map((row) => (
                  <div key={row.kind} className="flex h-7 items-center whitespace-nowrap">
                    <span className="min-w-0 flex-1 truncate">{row.kind}</span>
                    <span className={`${cell} ${isLight ? 'text-emerald-600' : 'text-emerald-400'}`}>+{number.format(row.additions)}</span>
                    <span className={`${cell} ${isLight ? 'text-rose-600' : 'text-rose-400'}`}>−{number.format(row.deletions)}</span>
                  </div>
                ))}
                <div className={`my-1.5 border-t ${isLight ? 'border-black/10' : 'border-white/10'}`} />
                <div className="flex h-7 items-center whitespace-nowrap font-medium">
                  <span className="min-w-0 flex-1 truncate">
                    Total
                    <span className={`ml-1.5 font-normal ${isLight ? 'text-black/40' : 'text-white/40'}`}>
                      {data.total.files} {data.total.files === 1 ? 'file' : 'files'}
                    </span>
                  </span>
                  <span className={`${cell} ${isLight ? 'text-emerald-600' : 'text-emerald-400'}`}>+{number.format(data.total.additions)}</span>
                  <span className={`${cell} ${isLight ? 'text-rose-600' : 'text-rose-400'}`}>−{number.format(data.total.deletions)}</span>
                </div>
              </div>
            </motion.div>
          )}
        </AnimatePresence>,
        document.body,
      )}
    </span>
  );
};
