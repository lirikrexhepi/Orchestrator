import { useEffect, useLayoutEffect, useRef, type ReactNode } from 'react'
import { SpringDriver, type Render } from '../motion/SpringDriver'
import { SPRINGS, type SpringConfig } from '../motion/spring'
import type { GlassBorderStyle } from './GlassBorder'
import { GlassSquircle } from './GlassSquircle'

export interface MorphSurfaceProps {
  width: number
  height: number
  maxHeight: number
  radius: number
  fill: string
  border?: GlassBorderStyle | false
  spring?: SpringConfig
  children?: ReactNode
}

const BLEND = 40

function maxOffset(maxHeight: number, radius: number): number {
  const corner = Math.min(radius * 2, maxHeight / 2)
  return Math.max(0, maxHeight - BLEND - corner * 2)
}

const shift = (factor: number): Render<'d'> => (v) => ({ transform: `translateY(${v.d * factor}px)` })

export function MorphSurface({ width, height, maxHeight, radius, fill, border = {}, spring = SPRINGS.bouncy, children }: MorphSurfaceProps) {
  const topWindow = useRef<HTMLDivElement | null>(null)
  const topCopy = useRef<HTMLDivElement | null>(null)
  const bottomWindow = useRef<HTMLDivElement | null>(null)
  const bottomCopy = useRef<HTMLDivElement | null>(null)
  const clip = useRef<HTMLDivElement | null>(null)
  const content = useRef<HTMLDivElement | null>(null)
  const drivers = useRef<SpringDriver<'d'>[]>([])
  const offset = Math.min(Math.max(0, maxHeight - height), maxOffset(maxHeight, radius))
  const first = useRef(offset)

  useLayoutEffect(() => {
    const parts: [HTMLDivElement | null, number][] = [
      [topWindow.current, 0.5],
      [topCopy.current, 0.5],
      [bottomWindow.current, 0.5],
      [bottomCopy.current, -0.5],
      [clip.current, 1],
      [content.current, -1],
    ]
    const list = parts
      .filter((p): p is [HTMLDivElement, number] => p[0] !== null)
      .map(([el, factor]) => new SpringDriver<'d'>(el, { d: first.current }, shift(factor)))
    list.forEach((d) => d.jump({ d: first.current }))
    drivers.current = list
    return () => list.forEach((d) => d.stop())
  }, [])

  useEffect(() => {
    drivers.current.forEach((d) => d.to({ d: offset }, spring))
  }, [offset, spring])

  const half = maxHeight / 2
  const mask = `linear-gradient(#000 calc(100% - ${BLEND}px), transparent)`
  const shape = { width, height: maxHeight, radius, fill, border }

  return (
    <div style={{ position: 'absolute', left: 0, bottom: 0, width, height: maxHeight, pointerEvents: 'none' }}>
      <div ref={bottomWindow} aria-hidden style={{ position: 'absolute', left: 0, right: 0, top: half - BLEND / 2, height: half + BLEND / 2, overflow: 'hidden' }}>
        <div ref={bottomCopy} style={{ position: 'absolute', left: 0, bottom: 0, width, height: maxHeight }}>
          <GlassSquircle {...shape} />
        </div>
      </div>
      <div
        ref={topWindow}
        aria-hidden
        style={{ position: 'absolute', left: 0, right: 0, top: 0, height: half + BLEND / 2, overflow: 'hidden', WebkitMaskImage: mask, maskImage: mask }}
      >
        <div ref={topCopy} style={{ position: 'absolute', left: 0, top: 0, width, height: maxHeight }}>
          <GlassSquircle {...shape} />
        </div>
      </div>
      <div ref={clip} style={{ position: 'absolute', inset: 0, overflow: 'hidden', borderRadius: radius }}>
        <div ref={content} style={{ position: 'absolute', inset: 0 }}>
          {children}
        </div>
      </div>
    </div>
  )
}
