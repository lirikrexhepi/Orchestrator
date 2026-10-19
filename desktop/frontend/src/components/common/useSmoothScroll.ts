import { useRef, useEffect, useCallback, type RefObject } from 'react';
import Lenis from 'lenis';

export interface SmoothScrollOptions {
  /** Multiplier for wheel delta. Default: 1.0 */
  speed?: number;
  /** Whether smooth scroll is enabled. Default: true */
  enabled?: boolean;
}

const active = new Set<Lenis>();
let frame = 0;

function tick(time: number) {
  for (const lenis of active) lenis.raf(time);
  frame = active.size > 0 ? requestAnimationFrame(tick) : 0;
}

function track(lenis: Lenis) {
  active.add(lenis);
  if (!frame) frame = requestAnimationFrame(tick);
}

function untrack(lenis: Lenis) {
  active.delete(lenis);
  if (active.size === 0 && frame) {
    cancelAnimationFrame(frame);
    frame = 0;
  }
}

/**
 * Attaches Lenis smooth gliding scroll to a scrollable container matching
 * the Valhalla project configuration (duration: 1.3, quintic exponential easing, smoothWheel: true).
 */
export function useSmoothScroll<T extends HTMLElement = HTMLDivElement>(
  targetRef: RefObject<T | null>,
  options: SmoothScrollOptions = {},
) {
  const { speed = 1.0, enabled = true } = options;
  const lenisRef = useRef<Lenis | null>(null);

  const attached = useRef<{ el: T; enabled: boolean; speed: number; destroy: () => void } | null>(null);

  useEffect(() => {
    const el = targetRef.current;
    const current = attached.current;
    if (current && current.el === el && current.enabled === enabled && current.speed === speed) return;
    current?.destroy();
    attached.current = null;
    if (!el || !enabled) return;

    const content = (el.firstElementChild as HTMLElement) || el;

    const lenis = new Lenis({
      wrapper: el,
      content,
      eventsTarget: el,
      duration: 1.3,
      easing: (t: number) => Math.min(1, 1.001 - Math.pow(2, -10 * t)),
      smoothWheel: true,
      wheelMultiplier: 0.95 * speed,
      touchMultiplier: 1.5,
      autoResize: true,
    });

    lenisRef.current = lenis;

    track(lenis);

    const sizes = new ResizeObserver(() => lenis.dimensions.resize());
    sizes.observe(content);

    attached.current = {
      el,
      enabled,
      speed,
      destroy: () => {
        sizes.disconnect();
        untrack(lenis);
        lenis.destroy();
        if (lenisRef.current === lenis) lenisRef.current = null;
      },
    };
  });

  useEffect(
    () => () => {
      attached.current?.destroy();
      attached.current = null;
    },
    [],
  );

  const scrollTo = useCallback((target: number, smooth = true) => {
    if (lenisRef.current) {
      lenisRef.current.scrollTo(target, { immediate: !smooth });
    } else if (targetRef.current) {
      targetRef.current.scrollTop = target;
    }
  }, [targetRef]);

  const stopGlide = useCallback(() => {
    if (lenisRef.current) {
      lenisRef.current.stop();
      lenisRef.current.start();
    }
  }, []);

  return { scrollTo, stopGlide, lenis: lenisRef };
}

export default useSmoothScroll;
