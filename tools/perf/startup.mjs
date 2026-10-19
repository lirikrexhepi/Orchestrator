import { launch, goTrace, navTimings, sleep, killPerf } from './lib.mjs'

const runs = Number(process.argv[2] || 3)
const out = []
for (let i = 0; i < runs; i++) {
  const { browser, page, t0, cdpAt, logStart } = await launch()
  await page.waitForLoadState('load')
  let readyAt = 0
  for (let k = 0; k < 400; k++) {
    const n = await page.evaluate(() => document.querySelectorAll('#root *').length).catch(() => 0)
    if (n > 50) { readyAt = Date.now(); break }
    await sleep(20)
  }
  await sleep(2500)
  const nav = await navTimings(page)
  const settled = await page.evaluate(() => document.querySelectorAll('#root *').length)
  const r = {
    cdpMs: cdpAt - t0,
    navStartMs: Math.round(nav.timeOrigin - t0),
    domInteractive: nav.domInteractive,
    dcl: nav.domContentLoaded,
    load: nav.load,
    fcp: nav.paints['first-contentful-paint'],
    rootPopulatedMs: readyAt - t0,
    domNodes: settled,
    go: goTrace(logStart),
    res: nav.res,
  }
  out.push(r)
  console.log(JSON.stringify({ ...r, res: undefined }, null, 0))
  if (i === 0) console.log(JSON.stringify(nav.res))
  await browser.close().catch(() => {})
  killPerf()
  await sleep(1000)
}
