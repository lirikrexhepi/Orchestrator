import { useMemo, type CSSProperties } from 'react'
import { lruCache } from './memo'
import { cornerList, isCircle, radiusKey, shapeKey, squirclePath, type CornerRadii, type Size, type SquircleShape } from './squircle'

const clips = lruCache<CSSProperties>(512)

function clipFor(shape: SquircleShape): CSSProperties {
  return clips(shapeKey(shape), () => {
    if (isCircle(shape)) return { borderRadius: 9999 }
    const clip = `path('${squirclePath(shape)}')`
    return { clipPath: clip, WebkitClipPath: clip }
  })
}

export function useShapeStyle(size: Size | null, radius: CornerRadii, smoothing: number): CSSProperties {
  const key = radiusKey(radius)
  return useMemo(() => {
    if (!size || size.width <= 0 || size.height <= 0) {
      const [tl, tr, br, bl] = cornerList(radius)
      return { borderRadius: `${tl}px ${tr}px ${br}px ${bl}px` }
    }
    return clipFor({ width: size.width, height: size.height, radius, smoothing })
  }, [size?.width, size?.height, key, smoothing])
}
