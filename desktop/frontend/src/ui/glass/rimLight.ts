import { lruCache } from '../geometry/memo'
import { sampleOutline, shapeKey, type SquircleShape } from '../geometry/squircle'

const gradients = lruCache<string>(256)

export function cachedRimGradient(shape: SquircleShape, light: RimLight): string {
  return gradients(`${shapeKey(shape)}|${rimLightKey(light)}`, () => rimGradient(shape, light))
}

export interface RimLight {
  angle: number
  color: string
  intensity: number
  backIntensity: number
  base: number
  falloff: number
}

export const DEFAULT_RIM_LIGHT: RimLight = {
  angle: 303,
  color: 'var(--rim)',
  intensity: 0.65,
  backIntensity: 0.64,
  base: 0,
  falloff: 2.4,
}

export function rimLightKey(light: RimLight): string {
  return `${light.angle}|${light.color}|${light.intensity}|${light.backIntensity}|${light.base}|${light.falloff}`
}

interface Stop {
  deg: number
  alpha: number
}

interface Rgb {
  r: number
  g: number
  b: number
  a: number
}

function parseColor(color: string): Rgb | null {
  const c = color.trim()
  const hex = /^#([0-9a-f]{3,8})$/i.exec(c)
  if (hex) {
    const h = hex[1]
    if (h.length === 3 || h.length === 4) {
      const v = h.split('').map((ch) => parseInt(ch + ch, 16))
      return { r: v[0], g: v[1], b: v[2], a: h.length === 4 ? v[3] / 255 : 1 }
    }
    if (h.length === 6 || h.length === 8) {
      const v = h.match(/../g)!.map((pair) => parseInt(pair, 16))
      return { r: v[0], g: v[1], b: v[2], a: h.length === 8 ? v[3] / 255 : 1 }
    }
    return null
  }
  const fn = /^rgba?\(([^)]+)\)$/i.exec(c)
  if (!fn) return null
  const parts = fn[1].split(/[\s,/]+/).filter(Boolean)
  if (parts.length < 3) return null
  const channel = (p: string) => (p.endsWith('%') ? (parseFloat(p) / 100) * 255 : parseFloat(p))
  const alpha = parts[3] === undefined ? 1 : parts[3].endsWith('%') ? parseFloat(parts[3]) / 100 : parseFloat(parts[3])
  return { r: channel(parts[0]), g: channel(parts[1]), b: channel(parts[2]), a: alpha }
}

function paint(color: Rgb | null, raw: string, alpha: number): string {
  if (!color) return `color-mix(in srgb, ${raw} ${Math.round(alpha * 10000) / 100}%, transparent)`
  const a = Math.round(alpha * color.a * 10000) / 10000
  return `rgba(${Math.round(color.r)},${Math.round(color.g)},${Math.round(color.b)},${a})`
}

function simplify(stops: Stop[], tolerance: number): Stop[] {
  if (stops.length <= 2) return stops
  const keep = new Uint8Array(stops.length)
  keep[0] = 1
  keep[stops.length - 1] = 1
  const pending: [number, number][] = [[0, stops.length - 1]]
  while (pending.length > 0) {
    const [lo, hi] = pending.pop()!
    const a = stops[lo]
    const b = stops[hi]
    const span = b.deg - a.deg
    let worst = -1
    let worstAt = -1
    for (let i = lo + 1; i < hi; i++) {
      const t = span > 1e-9 ? (stops[i].deg - a.deg) / span : 0
      const err = Math.abs(a.alpha + (b.alpha - a.alpha) * t - stops[i].alpha)
      if (err > worst) {
        worst = err
        worstAt = i
      }
    }
    if (worst > tolerance) {
      keep[worstAt] = 1
      pending.push([lo, worstAt], [worstAt, hi])
    }
  }
  return stops.filter((_, i) => keep[i] === 1)
}

export function rimGradient(shape: SquircleShape, light: RimLight): string {
  const samples = sampleOutline(shape, 12)
  if (samples.length === 0) return 'none'
  const cx = shape.width / 2
  const cy = shape.height / 2
  const rad = (light.angle * Math.PI) / 180
  const lx = Math.sin(rad)
  const ly = -Math.cos(rad)
  const stops: Stop[] = samples
    .map((s) => {
      const facing = s.nx * lx + s.ny * ly
      const peak = facing >= 0 ? light.intensity : light.backIntensity
      const alpha = light.base + (peak - light.base) * Math.pow(Math.abs(facing), light.falloff)
      let deg = (Math.atan2(s.x - cx, -(s.y - cy)) * 180) / Math.PI
      if (deg < 0) deg += 360
      return { deg, alpha: Math.min(1, Math.max(0, alpha)) }
    })
    .sort((a, b) => a.deg - b.deg)
  const first = stops[0]
  const last = stops[stops.length - 1]
  const gap = 360 - last.deg + first.deg
  const seam = gap > 1e-9 ? last.alpha + ((first.alpha - last.alpha) * (360 - last.deg)) / gap : first.alpha
  const ring = simplify([{ deg: 0, alpha: seam }, ...stops, { deg: 360, alpha: seam }], 0.004)
  const color = parseColor(light.color)
  return `conic-gradient(from 0deg at 50% 50%, ${ring
    .map((s) => `${paint(color, light.color, s.alpha)} ${Math.round(s.deg * 100) / 100}deg`)
    .join(', ')})`
}
