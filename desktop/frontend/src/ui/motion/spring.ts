export interface SpringConfig {
  damping: number
  response: number
}

export const SPRINGS = {
  smooth: { damping: 1, response: 0.4 },
  snappy: { damping: 1, response: 0.26 },
  press: { damping: 0.86, response: 0.28 },
  release: { damping: 0.72, response: 0.4 },
  follow: { damping: 0.9, response: 0.2 },
  bouncy: { damping: 0.8, response: 0.38 },
  sheet: { damping: 0.8, response: 0.3 },
  drawer: { damping: 0.84, response: 0.46 },
} as const satisfies Record<string, SpringConfig>

export interface SpringState {
  value: number
  velocity: number
}

export function springAt(from: number, to: number, velocity: number, config: SpringConfig, t: number): SpringState {
  const omega = (2 * Math.PI) / Math.max(config.response, 0.001)
  const zeta = Math.max(config.damping, 0)
  const x0 = from - to
  if (zeta < 1) {
    const wd = omega * Math.sqrt(1 - zeta * zeta)
    const b = (velocity + zeta * omega * x0) / wd
    const e = Math.exp(-zeta * omega * t)
    const cos = Math.cos(wd * t)
    const sin = Math.sin(wd * t)
    return {
      value: to + e * (x0 * cos + b * sin),
      velocity: e * ((b * wd - zeta * omega * x0) * cos - (x0 * wd + zeta * omega * b) * sin),
    }
  }
  if (zeta === 1) {
    const b = velocity + omega * x0
    const e = Math.exp(-omega * t)
    return { value: to + e * (x0 + b * t), velocity: e * (b - omega * (x0 + b * t)) }
  }
  const s = Math.sqrt(zeta * zeta - 1)
  const r1 = -omega * (zeta - s)
  const r2 = -omega * (zeta + s)
  const c1 = (velocity - r2 * x0) / (r1 - r2)
  const c2 = x0 - c1
  const e1 = Math.exp(r1 * t)
  const e2 = Math.exp(r2 * t)
  return { value: to + c1 * e1 + c2 * e2, velocity: c1 * r1 * e1 + c2 * r2 * e2 }
}
