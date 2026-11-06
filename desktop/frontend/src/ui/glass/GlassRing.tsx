import type { ReactNode } from 'react'
import { GlassBorder, type GlassBorderStyle } from './GlassBorder'

export interface GlassRingProps {
  size: number
  progress: number
  color: string
  strokeWidth?: number
  trackOpacity?: number
  border?: GlassBorderStyle | false
  children?: ReactNode
}

const RING_BORDER: GlassBorderStyle = { light: { intensity: 0.55, backIntensity: 0.5 } }
const EASE = 'cubic-bezier(0.22, 1, 0.36, 1)'

export function GlassRing({ size, progress, color, strokeWidth = 1.6, trackOpacity = 0.32, border = RING_BORDER, children }: GlassRingProps) {
  const center = size / 2
  const r = (size - strokeWidth) / 2
  const circumference = 2 * Math.PI * r
  const filled = Math.min(1, Math.max(0, progress))
  return (
    <span style={{ position: 'relative', width: size, height: size, display: 'grid', placeItems: 'center', flexShrink: 0 }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden style={{ position: 'absolute', inset: 0, transform: 'rotate(-90deg)' }}>
        <circle cx={center} cy={center} r={r} fill="none" strokeWidth={strokeWidth} style={{ stroke: color, strokeOpacity: trackOpacity, transition: `stroke 480ms ${EASE}` }} />
        <circle
          cx={center}
          cy={center}
          r={r}
          fill="none"
          strokeWidth={strokeWidth}
          strokeLinecap="round"
          strokeDasharray={circumference}
          style={{
            stroke: color,
            strokeDashoffset: circumference * (1 - filled),
            opacity: filled > 0 ? 1 : 0,
            transition: `stroke-dashoffset 480ms ${EASE}, stroke 480ms ${EASE}`,
          }}
        />
      </svg>
      {border ? <GlassBorder {...border} strokeWidth={border.strokeWidth ?? strokeWidth} radius={center} width={size} height={size} /> : null}
      <span style={{ position: 'relative', display: 'grid', placeItems: 'center' }}>{children}</span>
    </span>
  )
}
