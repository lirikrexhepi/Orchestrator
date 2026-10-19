import { launch, sleep, killPerf, instrumentedReload, frameStats } from './lib.mjs'
import { startProfile, stopProfile } from './cpuprof.mjs'
const project = process.argv[2] || 'configurator'
const chat = process.argv[3] || 'Alright now what i want you'
const { browser, page } = await launch()
await page.waitForLoadState('load')
await sleep(1500)
await instrumentedReload(page)
await sleep(2500)
await page.locator('[title*="Project:"], [aria-label*="Project:"]').first().click()
await sleep(700)
await page.evaluate((project) => { const els = [...document.querySelectorAll('[aria-label],[title]')].filter((e) => /^\d+ chats$/.test(e.getAttribute('aria-label') || e.getAttribute('title') || '')); const depth = (e) => { let n = e; for (let i = 0; i < 8 && n; i++) { if (n.textContent.includes(project)) return i; n = n.parentElement } return 99 }; els.sort((x, y) => depth(x) - depth(y)); els[0].click() }, project)
await sleep(900)
await page.evaluate(() => { window.__calls.length = 0 })
const cdp = await startProfile(page)
const target = page.getByText(chat).first()
await target.scrollIntoViewIfNeeded()
await page.evaluate(() => {
  window.__dom = []
  const t0 = performance.now()
  const tick = () => { window.__dom.push([Math.round(performance.now() - t0), document.getElementsByTagName('*').length]); if (performance.now() - t0 < 6000) requestAnimationFrame(tick) }
  requestAnimationFrame(tick)
})
const stats = await frameStats(page, () => target.click(), 6000)
const prof = await stopProfile(cdp, 'openchat.cpuprofile')
const calls = await page.evaluate(() => window.__calls)
const dom = await page.evaluate(() => window.__dom)
const final = dom[dom.length - 1][1]
const settled = dom.find(([, n]) => n >= final * 0.98)
console.log('frames', JSON.stringify(stats))
console.log('calls', JSON.stringify(calls.filter((c) => c.ms > 5)))
console.log('dom settled at', settled, 'final nodes', final, 'first change', dom.find(([, n]) => n !== dom[0][1]))
console.log('busy', prof.busyMs, '\n' + prof.rows.slice(0, 18).join('\n'))
await page.screenshot({ path: 'shot-openchat.png' })
await browser.close().catch(() => {})
killPerf()
