import { useCallback, type ForwardedRef, type MutableRefObject } from 'react'

export function useMergedRef<T>(own: MutableRefObject<T | null>, forwarded: ForwardedRef<T>) {
  return useCallback(
    (node: T | null) => {
      own.current = node
      if (typeof forwarded === 'function') forwarded(node)
      else if (forwarded) forwarded.current = node
    },
    [own, forwarded],
  )
}
