import { forwardRef } from 'react'
import { GlassSquircle, type GlassSquircleProps } from './GlassSquircle'

export interface GlassCircleProps extends Omit<GlassSquircleProps, 'width' | 'height' | 'radius' | 'smoothing'> {
  size: number
}

export const GlassCircle = forwardRef<HTMLElement, GlassCircleProps>(function GlassCircle(
  { size, as = 'button', style, ...rest },
  ref,
) {
  return (
    <GlassSquircle
      ref={ref}
      as={as}
      width={size}
      height={size}
      radius={size / 2}
      style={{ display: 'grid', placeItems: 'center', flexShrink: 0, ...style }}
      {...rest}
    />
  )
})
