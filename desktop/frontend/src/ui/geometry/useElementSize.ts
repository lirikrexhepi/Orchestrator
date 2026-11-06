import { useLayoutEffect, useState, type RefObject } from 'react'
import type { Size } from './squircle'

type Listener = (size: Size) => void

const listeners = new WeakMap<Element, Listener>()
let observer: ResizeObserver | null = null

function sharedObserver(): ResizeObserver {
  if (observer) return observer
  observer = new ResizeObserver((entries) => {
    for (const entry of entries) {
      const box = entry.borderBoxSize?.[0]
      const size = box
        ? { width: box.inlineSize, height: box.blockSize }
        : { width: entry.contentRect.width, height: entry.contentRect.height }
      listeners.get(entry.target)?.(size)
    }
  })
  return observer
}

const sameSize = (a: Size | null, b: Size) => a !== null && Math.abs(a.width - b.width) < 0.01 && Math.abs(a.height - b.height) < 0.01

export function useElementSize(ref: RefObject<HTMLElement | null>, enabled = true): Size | null {
  const [size, setSize] = useState<Size | null>(null)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el || !enabled) return
    const update = (next: Size) => setSize((prev) => (sameSize(prev, next) ? prev : next))
    update({ width: el.offsetWidth, height: el.offsetHeight })
    const ro = sharedObserver()
    listeners.set(el, update)
    ro.observe(el, { box: 'border-box' })
    return () => {
      ro.unobserve(el)
      listeners.delete(el)
    }
  }, [ref, enabled])
  return size
}
