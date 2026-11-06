import { memo, useMemo, useRef, type CSSProperties } from 'react'
import { radiusKey, type CornerRadii } from '../geometry/squircle'
import { useElementSize } from '../geometry/useElementSize'
import { cachedRingMask, strokeOutset, type RingGlow, type StrokeAlign } from './ringMask'
import { DEFAULT_RIM_LIGHT, cachedRimGradient, rimLightKey, type RimLight } from './rimLight'

export interface GlassBorderStyle {
  strokeWidth?: number
  align?: StrokeAlign
  light?: Partial<RimLight>
  glow?: RingGlow
}

export interface GlassBorderProps extends GlassBorderStyle {
  radius: CornerRadii
  smoothing?: number
  width?: number
  height?: number
  className?: string
  style?: CSSProperties
}

export const GlassBorder = memo(function GlassBorder({
  radius,
  smoothing = 1,
  width,
  height,
  strokeWidth = 1,
  align = 'inside',
  light,
  glow,
  className,
  style,
}: GlassBorderProps) {
  const ref = useRef<HTMLDivElement | null>(null)
  const outset = strokeOutset(strokeWidth, align)
  const fixed = width !== undefined && height !== undefined
  const measured = useElementSize(ref, !fixed)
  const w = fixed ? width : measured ? measured.width - outset * 2 : 0
  const h = fixed ? height : measured ? measured.height - outset * 2 : 0
  const rim: RimLight = { ...DEFAULT_RIM_LIGHT, ...light }
  const lightKey = rimLightKey(rim)
  const key = radiusKey(radius)

  const paint = useMemo<CSSProperties | null>(() => {
    if (w <= 0 || h <= 0) return null
    const shape = { width: w, height: h, radius, smoothing }
    const mask = cachedRingMask(shape, strokeWidth, align, glow)
    return {
      backgroundImage: cachedRimGradient(shape, rim),
      WebkitMaskImage: mask,
      maskImage: mask,
      WebkitMaskSize: '100% 100%',
      maskSize: '100% 100%',
      WebkitMaskRepeat: 'no-repeat',
      maskRepeat: 'no-repeat',
    }
  }, [w, h, key, smoothing, lightKey, strokeWidth, align, glow?.width, glow?.opacity])

  return (
    <div
      ref={ref}
      aria-hidden
      className={className}
      style={{ position: 'absolute', inset: -outset, pointerEvents: 'none', ...paint, ...style }}
    />
  )
})
