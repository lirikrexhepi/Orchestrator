export type CornerRadii = number | readonly [number, number, number, number]

export interface Size {
  width: number
  height: number
}

export interface SquircleShape extends Size {
  radius: CornerRadii
  smoothing?: number
}

export interface Point {
  x: number
  y: number
}

export interface OutlineSample extends Point {
  nx: number
  ny: number
}

type Segment =
  | { kind: 'line'; from: Point; to: Point }
  | { kind: 'cubic'; from: Point; c1: Point; c2: Point; to: Point }

type Curve = readonly [Point, Point, Point, Point]

interface Corner {
  along: number
  across: number
  curves: Curve[]
}

interface Frame {
  u: Point
  v: Point
}

const FRAMES: readonly Frame[] = [
  { u: { x: 1, y: 0 }, v: { x: 0, y: 1 } },
  { u: { x: 0, y: 1 }, v: { x: -1, y: 0 } },
  { u: { x: -1, y: 0 }, v: { x: 0, y: -1 } },
  { u: { x: 0, y: -1 }, v: { x: 1, y: 0 } },
]

const toRad = (deg: number) => (deg * Math.PI) / 180

export function cornerList(radius: CornerRadii): [number, number, number, number] {
  return typeof radius === 'number' ? [radius, radius, radius, radius] : [radius[0], radius[1], radius[2], radius[3]]
}

export function radiusKey(radius: CornerRadii): string {
  return typeof radius === 'number' ? String(radius) : radius.join(',')
}

export function shapeKey(shape: SquircleShape): string {
  const round = (n: number) => Math.round(n * 100) / 100
  return `${round(shape.width)}x${round(shape.height)}r${radiusKey(shape.radius)}s${shape.smoothing ?? 1}`
}

export function isCircle(shape: SquircleShape): boolean {
  if (Math.abs(shape.width - shape.height) > 0.5) return false
  const budget = Math.min(shape.width, shape.height) / 2
  return cornerList(shape.radius).every((r) => r >= budget)
}

function arcCurve(from: Point, to: Point, center: Point, radius: number): Curve {
  const a0 = Math.atan2(from.y - center.y, from.x - center.x)
  let sweep = Math.atan2(to.y - center.y, to.x - center.x) - a0
  while (sweep > Math.PI) sweep -= Math.PI * 2
  while (sweep < -Math.PI) sweep += Math.PI * 2
  const k = (4 / 3) * Math.tan(sweep / 4) * radius
  const a1 = a0 + sweep
  return [
    from,
    { x: from.x - k * Math.sin(a0), y: from.y + k * Math.cos(a0) },
    { x: to.x + k * Math.sin(a1), y: to.y - k * Math.cos(a1) },
    to,
  ]
}

function transition(r: number, xi: number, p: number): Curve {
  const t = toRad(45 * xi)
  const c = r * Math.tan(t / 2) * Math.cos(t)
  const arcStart = { x: p - r + r * Math.sin(t), y: r - r * Math.cos(t) }
  const b = (arcStart.x - c) / 3
  return [{ x: 0, y: 0 }, { x: 2 * b, y: 0 }, { x: 3 * b, y: 0 }, arcStart]
}

function cornerFor(radius: number, smoothing: number, alongBudget: number, acrossBudget: number): Corner {
  const r = Math.max(0, Math.min(radius, alongBudget, acrossBudget))
  if (r === 0) return { along: 0, across: 0, curves: [] }
  const xiA = Math.max(0, Math.min(smoothing, alongBudget / r - 1))
  const xiB = Math.max(0, Math.min(smoothing, acrossBudget / r - 1))
  const along = (1 + xiA) * r
  const across = (1 + xiB) * r
  const mirror = (p: Point): Point => ({ x: along - p.y, y: across - p.x })
  const head = transition(r, xiA, along)
  const tail = transition(r, xiB, across)
  const tailCurve: Curve = [mirror(tail[3]), mirror(tail[2]), mirror(tail[1]), mirror(tail[0])]
  const curves: Curve[] = [head]
  const arcFrom = head[3]
  const arcTo = tailCurve[0]
  if (Math.hypot(arcTo.x - arcFrom.x, arcTo.y - arcFrom.y) > 1e-9) curves.push(arcCurve(arcFrom, arcTo, { x: along - r, y: r }, r))
  curves.push(tailCurve)
  return { along, across, curves: curves.filter((c) => Math.hypot(c[3].x - c[0].x, c[3].y - c[0].y) > 1e-9) }
}

type CornerName = 'tl' | 'tr' | 'br' | 'bl'

interface Budget {
  radius: number
  x: number
  y: number
}

const NEIGHBOURS: Record<CornerName, { x: CornerName; y: CornerName }> = {
  tl: { x: 'tr', y: 'bl' },
  tr: { x: 'tl', y: 'br' },
  bl: { x: 'br', y: 'tl' },
  br: { x: 'bl', y: 'tr' },
}

function distribute(width: number, height: number, radius: CornerRadii): Record<CornerName, Budget> {
  const [tl, tr, br, bl] = cornerList(radius)
  const radii: Record<CornerName, number> = { tl, tr, bl, br }
  const out = {} as Record<CornerName, Budget>
  const order = (['tl', 'tr', 'bl', 'br'] as const).slice().sort((a, b) => radii[b] - radii[a])
  for (const name of order) {
    const r = radii[name]
    const axis = (other: CornerName, key: 'x' | 'y') => {
      if (r === 0 && radii[other] === 0) return 0
      const side = key === 'x' ? width : height
      const claimed = out[other]
      return claimed ? side - claimed[key] : (r / (r + radii[other])) * side
    }
    const x = axis(NEIGHBOURS[name].x, 'x')
    const y = axis(NEIGHBOURS[name].y, 'y')
    radii[name] = Math.min(r, x, y)
    out[name] = { radius: radii[name], x, y }
  }
  return out
}

function squircleSegments(shape: SquircleShape): Segment[] {
  const { width: w, height: h } = shape
  const smoothing = shape.smoothing ?? 1
  const spread = distribute(w, h, shape.radius)
  const corners = [
    cornerFor(spread.tr.radius, smoothing, spread.tr.x, spread.tr.y),
    cornerFor(spread.br.radius, smoothing, spread.br.y, spread.br.x),
    cornerFor(spread.bl.radius, smoothing, spread.bl.x, spread.bl.y),
    cornerFor(spread.tl.radius, smoothing, spread.tl.y, spread.tl.x),
  ]
  const starts: Point[] = [
    { x: w - corners[0].along, y: 0 },
    { x: w, y: h - corners[1].along },
    { x: corners[2].along, y: h },
    { x: 0, y: corners[3].along },
  ]
  const segments: Segment[] = []
  for (let i = 0; i < 4; i++) {
    const { u, v } = FRAMES[i]
    const origin = starts[i]
    const place = (p: Point): Point => ({ x: origin.x + u.x * p.x + v.x * p.y, y: origin.y + u.y * p.x + v.y * p.y })
    let end = origin
    for (const c of corners[i].curves) {
      const to = place(c[3])
      segments.push({ kind: 'cubic', from: place(c[0]), c1: place(c[1]), c2: place(c[2]), to })
      end = to
    }
    const next = starts[(i + 1) % 4]
    if (Math.hypot(next.x - end.x, next.y - end.y) > 1e-6) segments.push({ kind: 'line', from: end, to: next })
  }
  return segments
}

const num = (n: number) => String(Math.round(n * 1000) / 1000)

export function squirclePath(shape: SquircleShape, offset = 0): string {
  if (shape.width <= 0 || shape.height <= 0) return ''
  const pt = (p: Point) => `${num(p.x + offset)} ${num(p.y + offset)}`
  const segments = squircleSegments(shape)
  if (segments.length === 0) return ''
  let d = `M${pt(segments[0].from)}`
  for (const s of segments) {
    if (s.kind === 'line') d += `L${pt(s.to)}`
    else d += `C${pt(s.c1)} ${pt(s.c2)} ${pt(s.to)}`
  }
  return `${d}Z`
}

function outward(tx: number, ty: number): Point | null {
  const len = Math.hypot(tx, ty)
  return len < 1e-9 ? null : { x: ty / len, y: -tx / len }
}

function cubicAt(s: Extract<Segment, { kind: 'cubic' }>, t: number): OutlineSample | null {
  const m = 1 - t
  const x = m * m * m * s.from.x + 3 * m * m * t * s.c1.x + 3 * m * t * t * s.c2.x + t * t * t * s.to.x
  const y = m * m * m * s.from.y + 3 * m * m * t * s.c1.y + 3 * m * t * t * s.c2.y + t * t * t * s.to.y
  let tx = 3 * m * m * (s.c1.x - s.from.x) + 6 * m * t * (s.c2.x - s.c1.x) + 3 * t * t * (s.to.x - s.c2.x)
  let ty = 3 * m * m * (s.c1.y - s.from.y) + 6 * m * t * (s.c2.y - s.c1.y) + 3 * t * t * (s.to.y - s.c2.y)
  if (Math.hypot(tx, ty) < 1e-9) {
    tx = s.to.x - s.from.x
    ty = s.to.y - s.from.y
  }
  const n = outward(tx, ty)
  return n ? { x, y, nx: n.x, ny: n.y } : null
}

export function sampleOutline(shape: SquircleShape, perSegment = 10): OutlineSample[] {
  if (shape.width <= 0 || shape.height <= 0) return []
  const out: OutlineSample[] = []
  for (const s of squircleSegments(shape)) {
    if (s.kind === 'line') {
      const n = outward(s.to.x - s.from.x, s.to.y - s.from.y)
      if (!n) continue
      out.push({ ...s.from, nx: n.x, ny: n.y }, { ...s.to, nx: n.x, ny: n.y })
    } else {
      for (let i = 0; i <= perSegment; i++) {
        const sample = cubicAt(s, i / perSegment)
        if (sample) out.push(sample)
      }
    }
  }
  return out
}
