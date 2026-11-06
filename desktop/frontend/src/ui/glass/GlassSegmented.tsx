import { useEffect, useLayoutEffect, useRef, type ReactNode } from 'react'
import { SpringDriver } from '../motion/SpringDriver'
import { SPRINGS } from '../motion/spring'
import { VelocityTracker, capture, clampRubber, prefersReducedMotion, project } from '../motion/gesture'
import type { GlassBorderStyle } from './GlassBorder'
import { GlassSquircle, type Frost } from './GlassSquircle'

export interface SegmentOption<T extends string> {
  value: T
  label: string
  icon: ReactNode
}

export interface GlassSegmentedProps<T extends string> {
  options: readonly SegmentOption<T>[]
  value: T
  onChange: (value: T) => void
  height: number
  padding?: number
  gap?: number
  fill?: string
  lensFill?: string
  border?: GlassBorderStyle | false
  lensBorder?: GlassBorderStyle | false
  frost?: number | Frost
  className?: string
}

const DRAG_SLOP = 6

export function GlassSegmented<T extends string>({
  options,
  value,
  onChange,
  height,
  padding = 3,
  gap = 0,
  fill,
  lensFill = 'rgba(var(--ink), 0.16)',
  border = {},
  lensBorder = {},
  frost,
  className,
}: GlassSegmentedProps<T>) {
  const item = height - padding * 2
  const step = item + gap
  const width = padding * 2 + item * options.length + gap * (options.length - 1)
  const index = Math.max(0, options.findIndex((o) => o.value === value))
  const max = step * (options.length - 1)

  const track = useRef<HTMLElement | null>(null)
  const lens = useRef<HTMLElement | null>(null)
  const driver = useRef<SpringDriver<'x'> | null>(null)
  const dragging = useRef(false)
  const latest = useRef({ options, onChange, index })
  latest.current = { options, onChange, index }

  useLayoutEffect(() => {
    const el = lens.current
    if (!el) return
    const d = new SpringDriver<'x'>(el, { x: index * step }, (v, vel) => {
      const stretch = Math.min(0.06, Math.abs(vel.x) / (item * 60))
      return { transform: `translateX(${v.x}px) scale(${1 + stretch}, ${1 - stretch * 0.4})` }
    })
    d.jump({ x: index * step })
    driver.current = d
    return () => {
      d.stop()
      driver.current = null
    }
  }, [step, item])

  useEffect(() => {
    if (dragging.current) return
    driver.current?.to({ x: index * step }, prefersReducedMotion() ? SPRINGS.snappy : SPRINGS.bouncy)
  }, [index, step])

  useEffect(() => {
    const el = track.current
    if (!el) return
    const tracker = new VelocityTracker()
    let pointer: number | null = null
    let grab = 0
    let startX = 0
    let active = false

    const lensX = () => driver.current?.state().value.x ?? 0

    const onDown = (e: PointerEvent) => {
      if (pointer !== null || !e.isPrimary || !driver.current) return
      const left = el.getBoundingClientRect().left + padding
      const x = lensX()
      const local = e.clientX - left
      if (local < x || local > x + item) return
      pointer = e.pointerId
      grab = local - x
      startX = e.clientX
      active = false
      tracker.reset(e.clientX, 0)
      capture(el, e.pointerId)
    }

    const onMove = (e: PointerEvent) => {
      if (e.pointerId !== pointer || !driver.current) return
      tracker.push(e.clientX, 0)
      if (!active && Math.abs(e.clientX - startX) < DRAG_SLOP) return
      active = true
      dragging.current = true
      const left = el.getBoundingClientRect().left + padding
      const x = clampRubber(e.clientX - left - grab, 0, max, item)
      driver.current.jump({ x }, { x: tracker.velocity().x })
    }

    const finish = (e: PointerEvent, commit: boolean) => {
      if (e.pointerId !== pointer) return
      pointer = null
      if (!active || !driver.current) return
      active = false
      dragging.current = false
      const stop = (ev: Event) => {
        ev.preventDefault()
        ev.stopPropagation()
      }
      el.addEventListener('click', stop, { capture: true, once: true })
      window.setTimeout(() => el.removeEventListener('click', stop, { capture: true }), 400)
      const { options: opts, onChange: change, index: current } = latest.current
      const velocity = tracker.velocity().x
      const projected = lensX() + project(velocity, 0.99)
      const next = commit ? Math.max(0, Math.min(opts.length - 1, Math.round(projected / step))) : current
      driver.current.to({ x: next * step }, SPRINGS.bouncy, { x: velocity })
      if (next !== current) change(opts[next].value)
    }

    const onUp = (e: PointerEvent) => finish(e, true)
    const onCancel = (e: PointerEvent) => finish(e, false)
    el.addEventListener('pointerdown', onDown)
    el.addEventListener('pointermove', onMove)
    el.addEventListener('pointerup', onUp)
    el.addEventListener('pointercancel', onCancel)
    return () => {
      el.removeEventListener('pointerdown', onDown)
      el.removeEventListener('pointermove', onMove)
      el.removeEventListener('pointerup', onUp)
      el.removeEventListener('pointercancel', onCancel)
    }
  }, [padding, item, step, max])

  return (
    <GlassSquircle
      ref={track}
      role="radiogroup"
      width={width}
      height={height}
      radius={height / 2}
      fill={fill}
      border={border}
      frost={frost}
      className={className}
      style={{ display: 'flex', alignItems: 'center', gap, padding, touchAction: 'none' }}
    >
      <GlassSquircle
        ref={lens}
        as="span"
        aria-hidden
        width={item}
        height={item}
        radius={item / 2}
        fill={lensFill}
        border={lensBorder}
        style={{ position: 'absolute', left: padding, top: padding }}
      />
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          aria-label={o.label}
          onClick={() => o.value !== value && onChange(o.value)}
          style={{ width: item, height: item, display: 'grid', placeItems: 'center', position: 'relative', flexShrink: 0 }}
        >
          {o.icon}
        </button>
      ))}
    </GlassSquircle>
  )
}
