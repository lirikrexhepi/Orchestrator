import React, { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { MaterialIcon } from '../common/icons';
import { useTheme } from '../../themes';

export interface UserTurnMenuProps {
  turnId?: string;
  getDelete: () => ((turnId: string) => Promise<void> | void) | undefined;
  children: React.ReactNode;
}

const MENU_WIDTH = 248;

export const UserTurnMenu: React.FC<UserTurnMenuProps> = ({ turnId, getDelete, children }) => {
  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';
  const [menu, setMenu] = useState<{ x: number; y: number; canDelete: boolean } | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);

  const close = useCallback(() => {
    setMenu(null);
    setConfirming(false);
  }, []);

  useEffect(() => {
    if (!menu) return;
    const onPointer = (event: PointerEvent) => {
      if (panelRef.current?.contains(event.target as Node)) return;
      close();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };
    document.addEventListener('pointerdown', onPointer, true);
    window.addEventListener('keydown', onKey, true);
    window.addEventListener('wheel', close, { passive: true });
    window.addEventListener('resize', close);
    return () => {
      document.removeEventListener('pointerdown', onPointer, true);
      window.removeEventListener('keydown', onKey, true);
      window.removeEventListener('wheel', close);
      window.removeEventListener('resize', close);
    };
  }, [menu, close]);

  if (!turnId) return <>{children}</>;

  const onContextMenu = (event: React.MouseEvent) => {
    if (window.getSelection()?.toString()) return;
    event.preventDefault();
    event.stopPropagation();
    setConfirming(false);
    setMenu({
      x: Math.min(event.clientX, window.innerWidth - MENU_WIDTH - 8),
      y: Math.min(event.clientY, window.innerHeight - 96),
      canDelete: Boolean(getDelete()),
    });
  };

  const onDelete = async () => {
    const remove = getDelete();
    if (!remove || busy) return;
    if (!confirming) {
      setConfirming(true);
      return;
    }
    setBusy(true);
    try {
      await remove(turnId);
    } finally {
      setBusy(false);
      close();
    }
  };

  const panel = isLight
    ? 'bg-white border-black/10 text-[#111] shadow-[0_16px_40px_-8px_rgba(0,0,0,0.25)]'
    : 'bg-[#141416] border-white/10 text-white shadow-[0_16px_40px_-8px_rgba(0,0,0,0.7)]';
  const muted = isLight ? 'text-black/45' : 'text-white/45';
  const hover = isLight ? 'hover:bg-rose-500/10' : 'hover:bg-rose-500/15';

  return (
    <div className="contents" onContextMenu={onContextMenu}>
      {children}
      {menu &&
        createPortal(
          <div
            ref={panelRef}
            onContextMenu={(event) => event.preventDefault()}
            style={{ position: 'fixed', left: menu.x, top: menu.y, width: MENU_WIDTH, zIndex: 3000 }}
            className={`rounded-xl border p-1 font-(family-name:--app-font) ${panel}`}
          >
            {menu.canDelete ? (
              <button
                type="button"
                onClick={onDelete}
                disabled={busy}
                className={`flex w-full items-start gap-2.5 rounded-lg px-2.5 py-2 text-left cursor-pointer transition-colors ${hover} disabled:opacity-60`}
              >
                <span className="mt-[1px] inline-flex text-rose-500">
                  <MaterialIcon name={busy ? 'hourglass_empty' : 'delete'} size={15} />
                </span>
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="text-[12.5px] font-medium text-rose-500">
                    {busy ? 'Deleting…' : confirming ? 'Click again to delete' : 'Delete message and reply'}
                  </span>
                  <span className={`text-[11px] leading-snug ${muted}`}>
                    The agent will forget this message was ever sent.
                  </span>
                </span>
              </button>
            ) : (
              <div className={`px-2.5 py-2 text-[12px] ${muted}`}>Wait for the agent to finish to delete this message.</div>
            )}
          </div>,
          document.body,
        )}
    </div>
  );
};
