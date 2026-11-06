export function lruCache<T>(limit: number): (key: string, compute: () => T) => T {
  const cache = new Map<string, T>()
  return (key, compute) => {
    const hit = cache.get(key)
    if (hit !== undefined) {
      cache.delete(key)
      cache.set(key, hit)
      return hit
    }
    const value = compute()
    cache.set(key, value)
    if (cache.size > limit) cache.delete(cache.keys().next().value as string)
    return value
  }
}
