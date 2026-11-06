import { useEffect, type RefObject } from 'react'
import { PRESS_GEL_DEFAULTS, attachPressGel, type PressGelOptions } from './pressGel'

export type { PressGelOptions } from './pressGel'

export function pressGelKey(options: PressGelOptions | null): string {
  return options ? JSON.stringify({ ...PRESS_GEL_DEFAULTS, ...options }) : ''
}

export function usePressGel(target: RefObject<HTMLElement | null>, glow: RefObject<HTMLElement | null>, options: PressGelOptions | null) {
  const key = pressGelKey(options)
  useEffect(() => {
    const el = target.current
    if (!el || !key) return
    return attachPressGel(el, glow.current, JSON.parse(key))
  }, [target, glow, key])
}
