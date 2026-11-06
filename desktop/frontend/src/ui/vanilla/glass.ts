import { isCircle, squirclePath, type CornerRadii } from '../geometry/squircle'
import { cachedRingMask, strokeOutset, type RingGlow, type StrokeAlign } from '../glass/ringMask'
import { DEFAULT_RIM_LIGHT, cachedRimGradient, type RimLight } from '../glass/rimLight'
import { attachPressGel, type PressGelOptions } from '../motion/pressGel'

export interface GlassOptions {
  radius: CornerRadii
  smoothing?: number
  fill?: string
  light?: Partial<RimLight>
  strokeWidth?: number
  align?: StrokeAlign
  glow?: RingGlow
  rim?: boolean
  frost?: number
  pressable?: boolean | PressGelOptions
  highlight?: string
}

const DEFAULT_HIGHLIGHT = 'radial-gradient(closest-side, rgba(var(--ink),0.09), rgba(var(--ink),0.035) 55%, rgba(var(--ink),0))'

function layer(css: string): HTMLSpanElement {
  const span = document.createElement('span')
  span.setAttribute('aria-hidden', 'true')
  span.style.cssText = css
  return span
}

function setClip(target: HTMLElement, clip: string | null) {
  if (clip === null) {
    target.style.clipPath = ''
    target.style.setProperty('-webkit-clip-path', '')
    target.style.borderRadius = '9999px'
    return
  }
  target.style.borderRadius = ''
  target.style.clipPath = clip
  target.style.setProperty('-webkit-clip-path', clip)
}

export function glass(el: HTMLElement, options: GlassOptions): () => void {
  const smoothing = options.smoothing ?? 1
  const strokeWidth = options.strokeWidth ?? 1
  const align = options.align ?? 'inside'
  const light: RimLight = { ...DEFAULT_RIM_LIGHT, ...options.light }
  const outset = strokeOutset(strokeWidth, align)
  const press = options.pressable === false ? null : options.pressable === true || options.pressable === undefined ? (el.tagName === 'BUTTON' || el.tagName === 'A' ? {} : null) : options.pressable

  if (getComputedStyle(el).position === 'static') el.style.position = 'relative'
  el.style.isolation = 'isolate'
  if (press && press.drag !== false) el.style.touchAction = 'none'

  const fill = layer('position:absolute;inset:0;z-index:-1;pointer-events:none')
  fill.style.background = options.fill ?? 'var(--glass-control)'
  if (options.frost && options.frost > 0) {
    const frost = `blur(${options.frost}px) saturate(1.4)`
    fill.style.backdropFilter = frost
    fill.style.setProperty('-webkit-backdrop-filter', frost)
  }
  el.prepend(fill)

  let glowClip: HTMLSpanElement | null = null
  let glowDot: HTMLSpanElement | null = null
  if (press) {
    glowClip = layer('position:absolute;inset:0;z-index:-1;overflow:hidden;pointer-events:none')
    glowDot = layer('position:absolute;left:0;top:0;opacity:0')
    glowDot.style.background = options.highlight ?? DEFAULT_HIGHLIGHT
    glowClip.appendChild(glowDot)
    fill.after(glowClip)
  }

  const rim = options.rim === false ? null : layer(`position:absolute;inset:${-outset}px;pointer-events:none;mask-size:100% 100%;-webkit-mask-size:100% 100%;mask-repeat:no-repeat;-webkit-mask-repeat:no-repeat`)
  if (rim) el.appendChild(rim)

  const paint = (width: number, height: number) => {
    if (width <= 0 || height <= 0) return
    const shape = { width, height, radius: options.radius, smoothing }
    const clip = isCircle(shape) ? null : `path('${squirclePath(shape)}')`
    setClip(fill, clip)
    if (glowClip) setClip(glowClip, clip)
    if (glowDot) {
      const size = Math.min(width, height) * 2
      glowDot.style.width = `${size}px`
      glowDot.style.height = `${size}px`
    }
    if (rim) {
      const mask = cachedRingMask(shape, strokeWidth, align, options.glow)
      rim.style.backgroundImage = cachedRimGradient(shape, light)
      rim.style.maskImage = mask
      rim.style.setProperty('-webkit-mask-image', mask)
    }
  }

  paint(el.offsetWidth, el.offsetHeight)
  const ro = new ResizeObserver((entries) => {
    const box = entries[0]?.borderBoxSize?.[0]
    if (box) paint(box.inlineSize, box.blockSize)
    else paint(el.offsetWidth, el.offsetHeight)
  })
  ro.observe(el, { box: 'border-box' })
  const detach = press ? attachPressGel(el, glowDot, press) : () => undefined

  return () => {
    ro.disconnect()
    detach()
    fill.remove()
    glowClip?.remove()
    rim?.remove()
  }
}

export function glassAll(selector = '[data-glass]'): () => void {
  const stops = Array.from(document.querySelectorAll<HTMLElement>(selector)).map((el) => {
    const radius = el.dataset.glass === 'pill' ? 9999 : Number(el.dataset.glass) || 20
    return glass(el, { radius, fill: el.dataset.fill, frost: el.dataset.frost ? Number(el.dataset.frost) : undefined })
  })
  return () => stops.forEach((stop) => stop())
}
