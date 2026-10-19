import { launch, navTimings, sleep, killPerf } from './lib.mjs'
import { startProfile, stopProfile } from './cpuprof.mjs'

const { browser, page } = await launch()
await page.waitForLoadState('load')
await sleep(3000)
const results = []
for (let i = 0; i < 3; i++) {
  const cdp = await startProfile(page)
  await page.reload({ waitUntil: 'load' })
  await sleep(2500)
  const prof = await stopProfile(cdp, i === 0 ? 'reload.cpuprofile' : null)
  const nav = await navTimings(page)
  results.push({ dcl: nav.domContentLoaded, fcp: nav.paints['first-contentful-paint'], busy: prof.busyMs })
  if (i === 0) console.log(prof.rows.join('\n'))
}
console.log(JSON.stringify(results))
await browser.close().catch(() => {})
killPerf()
