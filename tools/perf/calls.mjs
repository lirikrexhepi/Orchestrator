import { launch, sleep, killPerf, instrumentedReload } from './lib.mjs'
const { browser, page } = await launch()
await page.waitForLoadState('load')
await sleep(2000)
await instrumentedReload(page)
const t = Date.now()
let detected = 0
for (let i = 0; i < 600; i++) {
  const txt = await page.evaluate(() => document.body.innerText.includes('Detecting CLIs')).catch(() => true)
  if (!txt && i > 2) { detected = Date.now() - t; break }
  await sleep(25)
}
await sleep(4000)
const calls = await page.evaluate(() => window.__calls)
console.log('detecting CLIs cleared after reload ms:', detected)
const agg = {}
for (const c of calls) { const a = (agg[c.name] ||= { n: 0, total: 0, max: 0, size: 0, first: c.start }); a.n++; a.total += c.ms; a.max = Math.max(a.max, c.ms); a.size = Math.max(a.size, c.size || 0) }
console.log(Object.entries(agg).sort((a, b) => b[1].max - a[1].max).map(([k, v]) => `${k.padEnd(28)} n=${v.n} max=${v.max}ms total=${v.total}ms first@${v.first} size=${v.size}`).join('\n'))
await browser.close().catch(() => {})
killPerf()
