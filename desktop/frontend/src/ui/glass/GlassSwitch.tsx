import { useLayoutEffect, useRef } from 'react'
import { SpringDriver } from '../motion/SpringDriver'
import { SPRINGS } from '../motion/spring'
import { prefersReducedMotion } from '../motion/gesture'
import { GlassCircle } from './GlassCircle'
import { GlassPill } from './GlassPill'

export interface GlassSwitchProps {
  checked: boolean
  onChange: (next: boolean) => void
  label: string
  width?: number
  height?: number
  onFill?: string
  offFill?: string
  disabled?: boolean
}

const INSET = 3

type Thumb = 'x' | 'grip'

export function GlassSwitch({ checked, onChange, label, width = 52, height = 32, onFill = 'var(--accent)', offFill = 'var(--surface-2)', disabled }: GlassSwitchProps) {
  const thumb = useRef<HTMLSpanElement | null>(null)
  const driver = useRef<SpringDriver<Thumb> | null>(null)
  const size = height - INSET * 2
  const travel = width - height

  useLayoutEffect(() => {
    const el = thumb.current
    if (!el) return
    const d = new SpringDriver<Thumb>(el, { x: checked ? travel : 0, grip: 0 }, (v, vel) => {
      const stretch = Math.min(0.28, v.grip * 0.18 + Math.abs(vel.x) / (travel * 18))
      return { transform: `translateX(${v.x}px) scale(${1 + stretch}, ${1 - stretch * 0.35})` }
    })
    d.jump({ x: checked ? travel : 0, grip: 0 })
    driver.current = d
    return () => d.stop()
  }, [travel])

  useLayoutEffect(() => {
    driver.current?.to({ x: checked ? travel : 0 }, prefersReducedMotion() ? SPRINGS.snappy : SPRINGS.bouncy)
  }, [checked, travel])

  const grip = (on: boolean) => driver.current?.to({ grip: on ? 1 : 0 }, SPRINGS.press)

  return (
    <GlassPill
      as="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      width={width}
      height={height}
      fill={checked ? onFill : offFill}
      pressable={false}
      className="glass-switch"
      onClick={() => onChange(!checked)}
      onPointerDown={() => grip(true)}
      onPointerUp={() => grip(false)}
      onPointerCancel={() => grip(false)}
      onPointerLeave={() => grip(false)}
    >
      <GlassCircle
        ref={thumb}
        as="span"
        size={size}
        fill="linear-gradient(180deg, #ffffff, #e6e6e6)"
        border={{ light: { intensity: 0.5, backIntensity: 0.3, color: '#ffffff' } }}
        pressable={false}
        style={{ position: 'absolute', left: INSET, top: INSET, borderRadius: '50%', transformOrigin: '50% 50%', boxShadow: '0 2px 6px rgba(0,0,0,0.35)' }}
        aria-hidden
      />
    </GlassPill>
  )
}
