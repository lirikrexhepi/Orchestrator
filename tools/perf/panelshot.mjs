import { launch, sleep, killPerf } from './lib.mjs'
const { browser, page } = await launch()
await page.waitForLoadState('load')
await sleep(2500)
await page.locator('[title*="Project:"], [aria-label*="Project:"]').first().click()
await sleep(700)
await page.evaluate(() => { const els = [...document.querySelectorAll('[aria-label],[title]')].filter((e) => /^\d+ chats$/.test(e.getAttribute('aria-label') || e.getAttribute('title') || '')); const depth = (e) => { let n = e; for (let i = 0; i < 8 && n; i++) { if (n.textContent.includes('configurator')) return i; n = n.parentElement } return 99 }; els.sort((x, y) => depth(x) - depth(y)); els[0].click() })
await sleep(900)
await page.getByText('Alright now what i want you').first().click()
await sleep(3000)
const pill = page.locator('[aria-label="Subagents"]').first()
if (await pill.count()) { await pill.click(); await sleep(800) }
await page.screenshot({ path: 'panel-1.png' })
const row = page.locator('button', { hasText: /Running|Done|Failed|Stopped/ }).first()
if (await row.count()) { await row.click(); await sleep(900) }
await page.screenshot({ path: 'panel-2.png' })
await browser.close().catch(() => {})
killPerf()
