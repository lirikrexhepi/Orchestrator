import { SPRINGS, springAt, type SpringConfig } from './spring'

export type Values<K extends string> = Record<K, number>
export type Render<K extends string> = (value: Values<K>, velocity: Values<K>) => Keyframe
export type SpringSpec<K extends string> = SpringConfig | Partial<Record<K, SpringConfig>>

function isConfig<K extends string>(spec: SpringSpec<K>): spec is SpringConfig {
  return typeof (spec as SpringConfig).damping === 'number'
}

interface Channel {
  from: number
  to: number
  velocity: number
  config: SpringConfig
}

const FRAME = 1 / 120
const MAX_SECONDS = 3

export class SpringDriver<K extends string> {
  private readonly keys: K[]
  private channels: Record<K, Channel>
  private animation: Animation | null = null
  private duration = 0
  private started = 0

  constructor(
    private readonly el: HTMLElement,
    initial: Values<K>,
    private readonly render: Render<K>,
    private readonly epsilon = 0.0005,
  ) {
    this.keys = Object.keys(initial) as K[]
    this.channels = {} as Record<K, Channel>
    for (const k of this.keys) this.channels[k] = { from: initial[k], to: initial[k], velocity: 0, config: SPRINGS.smooth }
  }

  private elapsed(): number {
    const a = this.animation
    if (!a) return Infinity
    const t = a.currentTime
    return (typeof t === 'number' ? t : performance.now() - this.started) / 1000
  }

  state(): { value: Values<K>; velocity: Values<K> } {
    const t = this.elapsed()
    const value = {} as Values<K>
    const velocity = {} as Values<K>
    for (const k of this.keys) {
      const c = this.channels[k]
      if (t >= this.duration) {
        value[k] = c.to
        velocity[k] = this.animation ? 0 : c.velocity
      } else {
        const s = springAt(c.from, c.to, c.velocity, c.config, t)
        value[k] = s.value
        velocity[k] = s.velocity
      }
    }
    return { value, velocity }
  }

  target(): Values<K> {
    const out = {} as Values<K>
    for (const k of this.keys) out[k] = this.channels[k].to
    return out
  }

  to(target: Partial<Values<K>>, spec?: SpringSpec<K>, velocity?: Partial<Values<K>>): number {
    const now = this.state()
    for (const k of this.keys) {
      const c = this.channels[k]
      const next = target[k]
      const config = spec === undefined ? undefined : isConfig(spec) ? spec : spec[k]
      this.channels[k] = {
        from: now.value[k],
        to: next ?? c.to,
        velocity: velocity?.[k] ?? now.velocity[k],
        config: next !== undefined && config ? config : c.config,
      }
    }
    return this.play()
  }

  jump(values: Partial<Values<K>>, velocity?: Partial<Values<K>>) {
    const now = this.state()
    const value = {} as Values<K>
    const speed = {} as Values<K>
    for (const k of this.keys) {
      value[k] = values[k] ?? now.value[k]
      speed[k] = velocity?.[k] ?? 0
      this.channels[k] = { from: value[k], to: value[k], velocity: speed[k], config: this.channels[k].config }
    }
    this.animation?.cancel()
    this.animation = null
    this.duration = 0
    Object.assign(this.el.style, this.render(value, speed))
  }

  stop() {
    this.animation?.cancel()
    this.animation = null
  }

  private play(): number {
    const frames: Keyframe[] = []
    const rest = {} as Values<K>
    const still = {} as Values<K>
    for (const k of this.keys) {
      rest[k] = this.channels[k].to
      still[k] = 0
    }
    let t = 0
    for (;;) {
      const value = {} as Values<K>
      const velocity = {} as Values<K>
      let settled = true
      for (const k of this.keys) {
        const c = this.channels[k]
        const s = springAt(c.from, c.to, c.velocity, c.config, t)
        value[k] = s.value
        velocity[k] = s.velocity
        if (Math.abs(s.value - c.to) > this.epsilon || Math.abs(s.velocity) > this.epsilon * 10) settled = false
      }
      if (settled || t >= MAX_SECONDS) break
      frames.push(this.render(value, velocity))
      t += FRAME
    }
    const previous = this.animation
    if (frames.length === 0) {
      previous?.cancel()
      this.animation = null
      this.duration = 0
      Object.assign(this.el.style, this.render(rest, still))
      return 0
    }
    frames.push(this.render(rest, still))
    this.duration = t
    const animation = this.el.animate(frames, { duration: t * 1000, easing: 'linear', fill: 'forwards' })
    previous?.cancel()
    this.animation = animation
    this.started = performance.now()
    animation.onfinish = () => {
      if (this.animation !== animation) return
      Object.assign(this.el.style, this.render(rest, still))
      animation.cancel()
      this.animation = null
      for (const k of this.keys) this.channels[k] = { ...this.channels[k], from: rest[k], velocity: 0 }
    }
    return t * 1000
  }
}
