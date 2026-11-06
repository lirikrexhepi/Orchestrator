import { SpringDriver } from './SpringDriver'
import { SPRINGS } from './spring'
import { capture, prefersReducedMotion, rubberband } from './gesture'

export interface PressGelOptions {
  grow?: number
  maxScale?: number
  stretch?: number
  pull?: number
  highlight?: number
  drag?: boolean
  reference?: number
}

export const PRESS_GEL_DEFAULTS: Required<PressGelOptions> = {
  grow: 2.5,
  maxScale: 1.035,
  stretch: 0.035,
  pull: 0.12,
  highlight: 1,
  drag: true,
  reference: 44,
}

const TAP_SLOP = 10
const RELEASE_SLACK = 24

type Gel = 'tx' | 'ty' | 'qx' | 'qy' | 'p'

const REST: Record<Gel, number> = { tx: 0, ty: 0, qx: 0, qy: 0, p: 1 }

function gelTransform(v: Record<Gel, number>): string {
  if (v.tx === 0 && v.ty === 0 && v.qx === 0 && v.qy === 0 && v.p === 1) return 'none'
  const mag = Math.hypot(v.qx, v.qy)
  const angle = mag > 1e-5 ? (Math.atan2(v.qy, v.qx) * 180) / Math.PI : 0
  return `translate(${v.tx}px, ${v.ty}px) rotate(${angle}deg) scale(${1 + mag}, ${1 - mag * 0.5}) rotate(${-angle}deg) scale(${v.p})`
}

export function attachPressGel(el: HTMLElement, glowEl: HTMLElement | null, options: PressGelOptions = {}): () => void {
  const o: Required<PressGelOptions> = { ...PRESS_GEL_DEFAULTS, ...options }
  let body: SpringDriver<Gel> | null = null
  let light: SpringDriver<'o'> | null = null

  let pointer: number | null = null
  let pointerType = ''
  let origin = { x: 0, y: 0 }
  let latest = { x: 0, y: 0 }
  let moved = false
  let frame = 0
  let reduced = false
  let reach = 0
  let damp = 1
  let scaleToLocal = 1
  let rect = { left: 0, top: 0 }

  const drivers = () => {
    body ??= new SpringDriver<Gel>(el, { ...REST }, (v) => ({ transform: gelTransform(v) }))
    if (glowEl) light ??= new SpringDriver<'o'>(glowEl, { o: 0 }, (v) => ({ opacity: String(v.o) }))
    return body
  }

  const placeGlow = (x: number, y: number) => {
    if (!glowEl) return
    const half = glowEl.offsetWidth / 2
    glowEl.style.transform = `translate(${(x - rect.left) * scaleToLocal - half}px, ${(y - rect.top) * scaleToLocal - half}px)`
  }

  const flush = () => {
    frame = 0
    if (pointer === null || !body) return
    placeGlow(latest.x, latest.y)
    if (reduced || !o.drag) return
    const dx = latest.x - origin.x
    const dy = latest.y - origin.y
    const dist = Math.hypot(dx, dy)
    const mag = dist > 0 ? o.stretch * damp * Math.tanh(dist / reach) : 0
    body.to(
      {
        tx: rubberband(dx, reach) * o.pull * damp,
        ty: rubberband(dy, reach) * o.pull * damp,
        qx: dist > 0 ? (dx / dist) * mag : 0,
        qy: dist > 0 ? (dy / dist) * mag : 0,
      },
      SPRINGS.follow,
    )
  }

  const onDown = (e: PointerEvent) => {
    if (pointer !== null || !e.isPrimary) return
    if (e.pointerType === 'mouse' && e.button !== 0) return
    if ((el as HTMLButtonElement).disabled) return
    const driver = drivers()
    pointer = e.pointerId
    pointerType = e.pointerType
    origin = { x: e.clientX, y: e.clientY }
    latest = origin
    moved = false
    reduced = prefersReducedMotion()
    const r = el.getBoundingClientRect()
    const w = el.offsetWidth
    const h = el.offsetHeight
    rect = { left: r.left, top: r.top }
    scaleToLocal = r.width > 0 ? w / r.width : 1
    reach = Math.min(Math.min(w, h), 64)
    damp = Math.min(1, o.reference / Math.max(w, h, 1))
    if (o.drag) capture(el, e.pointerId)
    placeGlow(e.clientX, e.clientY)
    light?.to({ o: o.highlight }, SPRINGS.snappy)
    if (!reduced) driver.to({ p: Math.min(o.maxScale, 1 + o.grow / Math.max(w, h, 1)) }, SPRINGS.press)
  }

  const onMove = (e: PointerEvent) => {
    if (e.pointerId !== pointer) return
    latest = { x: e.clientX, y: e.clientY }
    if (Math.hypot(latest.x - origin.x, latest.y - origin.y) > TAP_SLOP) moved = true
    if (!frame) frame = requestAnimationFrame(flush)
  }

  const isInside = (x: number, y: number) => {
    const r = el.getBoundingClientRect()
    return x >= r.left - RELEASE_SLACK && x <= r.right + RELEASE_SLACK && y >= r.top - RELEASE_SLACK && y <= r.bottom + RELEASE_SLACK
  }

  const swallowNativeClick = () => {
    const stop = (ev: Event) => {
      if (!ev.isTrusted) return
      ev.preventDefault()
      ev.stopPropagation()
      el.removeEventListener('click', stop, { capture: true })
    }
    el.addEventListener('click', stop, { capture: true })
    window.setTimeout(() => el.removeEventListener('click', stop, { capture: true }), 400)
  }

  const settle = () => {
    pointer = null
    if (frame) cancelAnimationFrame(frame)
    frame = 0
    light?.to({ o: 0 }, SPRINGS.smooth)
    body?.to({ ...REST }, reduced ? SPRINGS.smooth : SPRINGS.release)
  }

  const onUp = (e: PointerEvent) => {
    if (e.pointerId !== pointer) return
    const inside = isInside(e.clientX, e.clientY)
    const synthesize = inside && moved && pointerType !== 'mouse'
    if (!inside || synthesize) swallowNativeClick()
    settle()
    if (synthesize) el.click()
  }

  const onCancel = (e: PointerEvent) => {
    if (e.pointerId === pointer) settle()
  }

  el.addEventListener('pointerdown', onDown)
  el.addEventListener('pointermove', onMove)
  el.addEventListener('pointerup', onUp)
  el.addEventListener('pointercancel', onCancel)
  return () => {
    el.removeEventListener('pointerdown', onDown)
    el.removeEventListener('pointermove', onMove)
    el.removeEventListener('pointerup', onUp)
    el.removeEventListener('pointercancel', onCancel)
    if (frame) cancelAnimationFrame(frame)
    body?.stop()
    light?.stop()
    el.style.transform = ''
  }
}
