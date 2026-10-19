import fs from 'node:fs'

export async function startProfile(page) {
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('Profiler.enable')
  await cdp.send('Profiler.setSamplingInterval', { interval: 200 })
  await cdp.send('Profiler.start')
  return cdp
}

export async function stopProfile(cdp, file) {
  const { profile } = await cdp.send('Profiler.stop')
  if (file) fs.writeFileSync(file, JSON.stringify(profile))
  return summarize(profile)
}

export function summarize(profile, top = 25) {
  const byId = new Map(profile.nodes.map((n) => [n.id, n]))
  const self = new Map()
  const dt = profile.timeDeltas
  const counts = new Map()
  profile.samples.forEach((id, i) => counts.set(id, (counts.get(id) || 0) + (dt[i] || 0)))
  let total = 0
  let idle = 0
  for (const [id, us] of counts) {
    const n = byId.get(id)
    const cf = n.callFrame
    const name = cf.functionName || '(anon)'
    if (name === '(idle)' || name === '(program)') { idle += us; continue }
    total += us
    const url = cf.url.split('/').slice(-1)[0]
    const key = `${name} ${url}:${cf.lineNumber}:${cf.columnNumber}`
    self.set(key, (self.get(key) || 0) + us)
  }
  const rows = [...self.entries()].sort((a, b) => b[1] - a[1]).slice(0, top).map(([k, us]) => `${(us / 1000).toFixed(1).padStart(7)}ms  ${k}`)
  return { busyMs: Math.round(total / 1000), idleMs: Math.round(idle / 1000), rows }
}
