import { forwardRef } from 'react'
import { GlassSquircle, type GlassSquircleProps } from './GlassSquircle'

export interface GlassPillProps extends Omit<GlassSquircleProps, 'height' | 'radius' | 'smoothing'> {
  height: number
}

export const GlassPill = forwardRef<HTMLElement, GlassPillProps>(function GlassPill({ height, style, ...rest }, ref) {
  return (
    <GlassSquircle
      ref={ref}
      height={height}
      radius={height / 2}
      style={{ display: 'inline-flex', alignItems: 'center', ...style }}
      {...rest}
    />
  )
})
