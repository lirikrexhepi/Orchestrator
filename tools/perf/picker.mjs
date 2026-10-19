import { launch, sleep } from './lib.mjs'
const { browser, page } = await launch()
await page.waitForLoadState('load')
await sleep(2000)
await page.getByText('Gemini 3.8 Flash').first().click()
await sleep(600)
const items = await page.evaluate(() => [...document.querySelectorAll('button,[role=option],[role=menuitem],li')].map((b) => (b.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 50)).filter(Boolean))
console.log(JSON.stringify(items))
await browser.close().catch(() => {})
