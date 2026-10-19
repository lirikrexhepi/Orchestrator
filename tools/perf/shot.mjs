import { launch, sleep } from './lib.mjs'
const { browser, page } = await launch()
await page.waitForLoadState('load')
await sleep(2500)
await page.screenshot({ path: 'shot-home.png' })
const info = await page.evaluate(() => ({ w: innerWidth, h: innerHeight, dpr: devicePixelRatio, buttons: [...document.querySelectorAll('button,[role=button]')].map((b) => (b.getAttribute('aria-label') || b.title || b.textContent || '').trim().slice(0, 40)).filter(Boolean) }))
console.log(JSON.stringify(info))
await browser.close().catch(() => {})
