import { createElement, forwardRef, useRef, type CSSProperties, type HTMLAttributes } from 'react'
import type { CornerRadii } from '../geometry/squircle'
import { useElementSize } from '../geometry/useElementSize'
import { useMergedRef } from '../geometry/useMergedRef'
import { useShapeStyle } from '../geometry/useShapeStyle'
import { usePressGel, type PressGelOptions } from '../motion/usePressGel'
import { GlassBorder, type GlassBorderStyle } from './GlassBorder'

export type SurfaceTag = 'div' | 'span' | 'button' | 'section' | 'label' | 'a'

export interface Frost {
  blur: number
  saturate?: number
}

export interface GlassSquircleProps extends HTMLAttributes<HTMLElement> {
  as?: SurfaceTag
  width?: number
  height?: number
  radius: CornerRadii
  smoothing?: number
  fill?: string
  border?: GlassBorderStyle | false
  frost?: number | Frost
  pressable?: boolean | PressGelOptions
  highlight?: string
  type?: 'button' | 'submit' | 'reset'
  disabled?: boolean
  href?: string
}

const DEFAULT_HIGHLIGHT = 'radial-gradient(closest-side, rgba(var(--ink),0.09), rgba(var(--ink),0.035) 55%, rgba(var(--ink),0))'

function frostStyle(frost: number | Frost | undefined): CSSProperties | null {
  if (frost === undefined) return null
  const f = typeof frost === 'number' ? { blur: frost } : frost
  if (f.blur <= 0) return null
  const value = `blur(${f.blur}px) saturate(${f.saturate ?? 1.4})`
  return { backdropFilter: value, WebkitBackdropFilter: value }
}

function pressOptions(pressable: boolean | PressGelOptions | undefined, as: SurfaceTag, disabled: boolean | undefined): PressGelOptions | null {
  if (disabled) return null
  if (pressable === undefined) return as === 'button' ? {} : null
  if (pressable === false) return null
  return pressable === true ? {} : pressable
}

export const GlassSquircle = forwardRef<HTMLElement, GlassSquircleProps>(function GlassSquircle(
  { as = 'div', width, height, radius, smoothing = 1, fill, border = {}, frost, pressable, highlight = DEFAULT_HIGHLIGHT, type, disabled, style, children, ...rest },
  ref,
) {
  const own = useRef<HTMLElement | null>(null)
  const glow = useRef<HTMLSpanElement | null>(null)
  const setRef = useMergedRef(own, ref)
  const fixed = width !== undefined && height !== undefined
  const measured = useElementSize(own)
  const size = measured ?? (fixed ? { width, height } : null)
  const shape = useShapeStyle(size, radius, smoothing)
  const press = pressOptions(pressable, as, disabled)
  usePressGel(own, glow, press)
  const glowSize = size ? Math.min(size.width, size.height) * 2 : 0

  return createElement(
    as,
    {
      ...rest,
      ref: setRef,
      disabled: as === 'button' ? disabled : undefined,
      type: as === 'button' ? type ?? 'button' : undefined,
      style: {
        position: 'relative',
        isolation: 'isolate',
        width,
        height,
        touchAction: press && press.drag !== false ? 'none' : undefined,
        ...style,
      },
    },
    <span aria-hidden style={{ position: 'absolute', inset: 0, zIndex: -1, background: fill, pointerEvents: 'none', ...frostStyle(frost), ...shape }} />,
    press ? (
      <span aria-hidden style={{ position: 'absolute', inset: 0, zIndex: -1, overflow: 'hidden', pointerEvents: 'none', ...shape }}>
        <span
          ref={glow}
          style={{ position: 'absolute', left: 0, top: 0, width: glowSize, height: glowSize, background: highlight, opacity: 0 }}
        />
      </span>
    ) : null,
    children,
    border && size ? (
      <GlassBorder {...border} radius={radius} smoothing={smoothing} width={size.width} height={size.height} />
    ) : null,
  )
})
