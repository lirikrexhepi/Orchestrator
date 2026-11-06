export function rubberband(offset: number, dimension: number, constant = 0.55): number {
  if (dimension <= 0) return 0
  return (offset * dimension * constant) / (dimension + constant * Math.abs(offset))
}

export function project(velocity: number, decelerationRate = 0.998): number {
  return ((velocity / 1000) * decelerationRate) / (1 - decelerationRate)
}

export function clampRubber(value: number, min: number, max: number, dimension: number): number {
  if (value < min) return min + rubberband(value - min, dimension)
  if (value > max) return max + rubberband(value - max, dimension)
  return value
}

interface Sample {
  t: number
  x: number
  y: number
}

export class VelocityTracker {
  private samples: Sample[] = []

  reset(x: number, y: number) {
    this.samples = [{ t: performance.now(), x, y }]
  }

  push(x: number, y: number) {
    const t = performance.now()
    this.samples.push({ t, x, y })
    while (this.samples.length > 2 && t - this.samples[0].t > 100) this.samples.shift()
  }

  velocity(): { x: number; y: number } {
    const first = this.samples[0]
    const last = this.samples[this.samples.length - 1]
    if (!first || !last || performance.now() - last.t > 80) return { x: 0, y: 0 }
    const dt = (last.t - first.t) / 1000
    if (dt <= 0) return { x: 0, y: 0 }
    return { x: (last.x - first.x) / dt, y: (last.y - first.y) / dt }
  }
}

export function capture(el: Element, pointerId: number) {
  try {
    el.setPointerCapture(pointerId)
  } catch {
    return
  }
}

export function prefersReducedMotion(): boolean {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
}
