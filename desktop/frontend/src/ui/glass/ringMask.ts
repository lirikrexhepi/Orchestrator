import { lruCache } from '../geometry/memo'
import { shapeKey, squirclePath, type SquircleShape } from '../geometry/squircle'

const masks = lruCache<string>(256)

export function cachedRingMask(shape: SquircleShape, strokeWidth: number, align: StrokeAlign, glow?: RingGlow): string {
  const glowKey = glow ? `${glow.width}/${glow.opacity}` : '-'
  return masks(`${shapeKey(shape)}|${strokeWidth}|${align}|${glowKey}`, () => ringMask(shape, strokeWidth, align, glow))
}

export type StrokeAlign = 'inside' | 'center' | 'outside'

export interface RingGlow {
  width: number
  opacity: number
}

export function strokeOutset(strokeWidth: number, align: StrokeAlign): number {
  if (align === 'outside') return strokeWidth
  if (align === 'center') return strokeWidth / 2
  return 0
}

export function ringMask(shape: SquircleShape, strokeWidth: number, align: StrokeAlign, glow?: RingGlow): string {
  const outset = strokeOutset(strokeWidth, align)
  const w = shape.width + outset * 2
  const h = shape.height + outset * 2
  const d = squirclePath(shape, outset)
  if (!d) return 'none'
  const doubled = strokeWidth * 2
  const defs: string[] = [`<clipPath id="in"><path d="${d}"/></clipPath>`]
  let stroke: string
  if (align === 'inside') {
    stroke = `<path d="${d}" fill="none" stroke="#fff" stroke-width="${doubled}" clip-path="url(#in)"/>`
  } else if (align === 'outside') {
    defs.push(`<mask id="out" maskUnits="userSpaceOnUse" x="0" y="0" width="${w}" height="${h}"><rect width="${w}" height="${h}" fill="#fff"/><path d="${d}" fill="#000"/></mask>`)
    stroke = `<path d="${d}" fill="none" stroke="#fff" stroke-width="${doubled}" mask="url(#out)"/>`
  } else {
    stroke = `<path d="${d}" fill="none" stroke="#fff" stroke-width="${strokeWidth}"/>`
  }
  let halo = ''
  if (glow && glow.width > 0 && glow.opacity > 0) {
    defs.push(`<filter id="soft" x="-20%" y="-20%" width="140%" height="140%"><feGaussianBlur stdDeviation="${glow.width / 2}"/></filter>`)
    halo = `<g clip-path="url(#in)"><path d="${d}" fill="none" stroke="#fff" stroke-opacity="${glow.opacity}" stroke-width="${glow.width * 2}" filter="url(#soft)"/></g>`
  }
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}"><defs>${defs.join('')}</defs>${halo}${stroke}</svg>`
  return `url("data:image/svg+xml,${encodeURIComponent(svg)}")`
}
