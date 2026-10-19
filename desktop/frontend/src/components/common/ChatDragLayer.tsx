import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ChatDragCard, endChatDragCard, isChatDrag, subscribeChatDragCard } from './chatDrag';
import { renderChatIcon } from './DynamicIsland';
import { MaterialIcon } from './icons';
import { useTheme } from '../../themes';

const CARD_WIDTH = 232;
const GRAB_X = 30;
const GRAB_Y = 26;
const BASE_TILT = -3;

type Phase = 'drag' | 'drop' | 'cancel';

interface Motion {
  x: number;
  y: number;
  tx: number;
  ty: number;
  px: number;
  rot: number;
  scale: number;
  opacity: number;
  phase: Phase;
  frame: number;
  homeX: number;
  homeY: number;
  startedAt: number;
}

let hintTarget: HTMLElement | null = null;

export function setChatDragHint(hint: string | null) {
  if (!hintTarget) return;
  hintTarget.textContent = hint ?? '';
  hintTarget.style.display = hint ? 'flex' : 'none';
}

export function markChatContextTarget(event: React.DragEvent) {
  if (!isChatDrag(event.dataTransfer)) return;
  event.dataTransfer.dropEffect = 'copy';
  setChatDragHint('Attach as context');
}

export function leaveChatContextTarget(event: React.DragEvent) {
  const next = event.relatedTarget as Node | null;
  if (next && event.currentTarget.contains(next)) return;
  setChatDragHint(null);
}

function folderName(path?: string): string {
  if (!path) return '';
  const parts = path.replace(/[\\/]+$/, '').split(/[\\/]/);
  return parts[parts.length - 1] || path;
}

export const ChatDragLayer: React.FC = () => {
  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';
  const [card, setCard] = useState<ChatDragCard | null>(null);
  const cardRef = useRef<HTMLDivElement>(null);
  const hintRef = useRef<HTMLSpanElement>(null);
  const state = useRef<Motion | null>(null);

  useEffect(() => {
    hintTarget = hintRef.current;
    return () => {
      if (hintTarget === hintRef.current) hintTarget = null;
    };
  });

  useEffect(() => {
    const step = () => {
      const s = state.current;
      const el = cardRef.current;
      if (!s) return;
      if (el) {
        const follow = s.phase === 'drag' ? 0.34 : 0.22;
        const tx = s.phase === 'cancel' ? s.homeX : s.tx;
        const ty = s.phase === 'cancel' ? s.homeY : s.ty;
        s.x += (tx - s.x) * follow;
        s.y += (ty - s.y) * follow;
        const vx = s.x - s.px;
        s.px = s.x;
        const tilt = s.phase === 'drag' ? BASE_TILT + Math.max(-9, Math.min(9, vx * 0.45)) : 0;
        s.rot += (tilt - s.rot) * 0.16;
        const scaleTarget = s.phase === 'drag' ? 1 : s.phase === 'drop' ? 0.4 : 0.9;
        const opacityTarget = s.phase === 'drag' ? 1 : 0;
        s.scale += (scaleTarget - s.scale) * (s.phase === 'drop' ? 0.24 : 0.2);
        s.opacity += (opacityTarget - s.opacity) * (s.phase === 'drag' ? 0.3 : 0.2);
        el.style.transform = `translate3d(${s.x - GRAB_X}px, ${s.y - GRAB_Y}px, 0) rotate(${s.rot}deg) scale(${s.scale})`;
        el.style.opacity = String(s.opacity);
        if (s.phase !== 'drag' && s.opacity < 0.02) {
          state.current = null;
          setCard(null);
          return;
        }
      }
      s.frame = requestAnimationFrame(step);
    };

    const finish = (phase: Phase) => {
      const s = state.current;
      if (!s || s.phase !== 'drag') return;
      s.phase = phase;
      setChatDragHint(null);
      endChatDragCard();
    };

    const onDragOver = (event: DragEvent) => {
      const s = state.current;
      if (!s || s.phase !== 'drag') return;
      s.tx = event.clientX;
      s.ty = event.clientY;
    };
    const onDrop = () => finish('drop');
    const onDragEnd = (event: DragEvent) => finish(event.dataTransfer?.dropEffect === 'none' ? 'cancel' : 'drop');
    const onPointer = () => {
      const s = state.current;
      if (s && performance.now() - s.startedAt > 250) finish('cancel');
    };

    const unsubscribe = subscribeChatDragCard((next) => {
      if (!next) return;
      const startX = next.origin ? next.origin.left + GRAB_X : next.x;
      const startY = next.origin ? next.origin.top + GRAB_Y : next.y;
      if (state.current) cancelAnimationFrame(state.current.frame);
      state.current = {
        x: startX,
        y: startY,
        tx: next.x,
        ty: next.y,
        px: startX,
        rot: 0,
        scale: 0.94,
        opacity: 0,
        phase: 'drag',
        frame: 0,
        homeX: startX,
        homeY: startY,
        startedAt: performance.now(),
      };
      setCard(next);
      state.current.frame = requestAnimationFrame(step);
    });

    document.addEventListener('dragover', onDragOver, true);
    document.addEventListener('drop', onDrop, true);
    document.addEventListener('dragend', onDragEnd, true);
    document.addEventListener('mousemove', onPointer, true);
    document.addEventListener('pointerdown', onPointer, true);
    return () => {
      unsubscribe();
      document.removeEventListener('dragover', onDragOver, true);
      document.removeEventListener('drop', onDrop, true);
      document.removeEventListener('dragend', onDragEnd, true);
      document.removeEventListener('mousemove', onPointer, true);
      document.removeEventListener('pointerdown', onPointer, true);
      if (state.current) cancelAnimationFrame(state.current.frame);
    };
  }, []);

  const payload = card?.payload;
  const project = folderName(payload?.projectPath);
  const surface = isLight
    ? 'bg-white border-black/[0.08] text-[#111] shadow-[0_22px_48px_-12px_rgba(0,0,0,0.35),0_2px_6px_rgba(0,0,0,0.06)]'
    : 'bg-[#1b1b1e] border-white/10 text-white shadow-[0_24px_56px_-12px_rgba(0,0,0,0.85)]';
  const muted = isLight ? 'text-black/50' : 'text-white/50';
  const chip = isLight ? 'bg-black/[0.05] text-black/60' : 'bg-white/[0.08] text-white/60';
  const hint = isLight ? 'bg-[#0A84FF] text-white' : 'bg-[#0A84FF] text-white';

  return createPortal(
    <div
      ref={cardRef}
      aria-hidden
      style={{
        position: 'fixed',
        left: 0,
        top: 0,
        width: CARD_WIDTH,
        zIndex: 5000,
        pointerEvents: 'none',
        opacity: 0,
        transformOrigin: `${GRAB_X}px ${GRAB_Y}px`,
        willChange: 'transform, opacity',
        display: payload ? 'block' : 'none',
      }}
    >
      <div className={`rounded-2xl border px-3.5 py-3 font-(family-name:--app-font) ${surface}`}>
        <div className="flex items-center gap-2">
          <span className="flex h-[20px] w-[20px] shrink-0 items-center justify-center">
            {payload && renderChatIcon(payload.driver, payload.model, isLight, 'w-[17px] h-[17px]')}
          </span>
          <span className="min-w-0 flex-1 truncate text-[14px] font-semibold tracking-tight">{payload?.title}</span>
        </div>
        {payload?.subtitle && <div className={`mt-1 truncate text-[12px] tabular-nums ${muted}`}>{payload.subtitle}</div>}
        {project && (
          <div className={`mt-2 inline-flex max-w-full items-center gap-1 rounded-full px-2 py-[3px] text-[11px] ${chip}`}>
            <MaterialIcon name="folder" size={12} />
            <span className="truncate">{project}</span>
          </div>
        )}
      </div>
      <span
        ref={hintRef}
        style={{ display: 'none' }}
        className={`absolute -bottom-3 left-3 items-center rounded-full px-2.5 py-[3px] text-[11px] font-medium shadow-md ${hint}`}
      />
    </div>,
    document.body,
  );
};
