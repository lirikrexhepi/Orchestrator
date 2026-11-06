import React from 'react';
import { LayoutGrid, materialIconSvg } from '../common/icons';
import { MorphButtons } from '../../liquid-morph/glass/MorphButtons';
import type { AgentCardMode } from './AgentWindow';

interface AgentViewSwitcherProps {
  mode: AgentCardMode;
  onChange: (mode: AgentCardMode) => void;
  tasks: { done: number; total: number };
  servers: number;
  changes: number;
  isLight: boolean;
}

const DARK = {
  '--ma-fill': '#2a2a2a',
  '--ma-hover': '#363636',
  '--ma-fg': 'rgba(255,255,255,0.92)',
  '--ma-rim': '#ffffff',
  '--ma-rim-hi': '0.55',
  '--ma-rim-lo': '0.06',
  '--font': 'var(--app-font)',
} as React.CSSProperties;

const LIGHT = {
  '--ma-fill': '#ffffff',
  '--ma-hover': '#efeff2',
  '--ma-fg': '#111111',
  '--ma-rim': '#000000',
  '--ma-rim-hi': '0.12',
  '--ma-rim-lo': '0.05',
  '--ma-shadow': '0.05',
  '--font': 'var(--app-font)',
} as React.CSSProperties;

const ICONS: Record<AgentCardMode, string> = {
  chat: materialIconSvg('chat'),
  tasklist: materialIconSvg('checklist'),
  browser: materialIconSvg('globe'),
  servers: materialIconSvg('terminal'),
  changes: materialIconSvg('fork_right'),
};

export const AgentViewSwitcher: React.FC<AgentViewSwitcherProps> = ({ mode, onChange, tasks, servers, changes, isLight }) => {
  const available = React.useMemo(() => {
    const views: { id: AgentCardMode; label: string }[] = [{ id: 'chat', label: 'Chat' }];
    if (tasks.total > 0) views.push({ id: 'tasklist', label: `Tasks ${tasks.done}/${tasks.total}` });
    if (servers > 0) {
      views.push({ id: 'browser', label: 'Preview' });
      views.push({ id: 'servers', label: servers > 1 ? `Servers ${servers}` : 'Servers' });
    }
    if (changes > 0) views.push({ id: 'changes', label: `Changes ${changes}` });
    return views;
  }, [tasks.done, tasks.total, servers, changes]);

  const onChangeRef = React.useRef(onChange);
  onChangeRef.current = onChange;

  const items = React.useMemo(
    () => [
      {
        id: 'views',
        label: 'Views',
        icon: <LayoutGrid size={15} strokeWidth={1.75} />,
        actions: available.map((view) => ({
          id: view.id,
          label: view.label,
          icon: ICONS[view.id],
          selected: view.id === mode,
          dismiss: view.id === mode,
          onSelect: () => onChangeRef.current(view.id),
        })),
      },
    ],
    [available, mode],
  );

  if (available.length <= 1) {
    return null;
  }

  return (
    <div style={isLight ? LIGHT : DARK} onClick={(e) => e.stopPropagation()} className="shrink-0 pointer-events-auto">
      <MorphButtons size={28} gap={6} anchor="end" items={items} />
    </div>
  );
};
