import { launch, sleep, killPerf } from './lib.mjs'
const { browser, page } = await launch()
await page.waitForLoadState('load')
await sleep(2500)
const ev = []
await page.evaluate(() => { window.__tid = null; window.runtime.EventsOn('agent:events', (b) => { for (const e of b) if (!window.__tid && e.threadId && e.threadId !== 'coordinator') { window.__tid = e.threadId; window.__turn = e.turnId } }) })
const ta = page.locator('textarea').first()
await ta.click()
await ta.fill('Reply with exactly the word ok.')
await ta.press('Enter')
for (let i = 0; i < 200; i++) { if (await page.evaluate(() => window.__tid && window.__turn)) break; await sleep(100) }
await sleep(8000)
const stream = (n) => page.evaluate(({ n }) => new Promise((res) => {
  const tid = window.__tid, turn = window.__turn + '-s' + Math.random()
  window.runtime.EventsEmit('agent:events', [{ kind: 'turn.started', threadId: tid, turnId: turn, at: Date.now() }])
  let i = 0
  const id = setInterval(() => {
    const evs = [{ kind: 'agent.message', threadId: tid, turnId: turn, itemId: turn + 'm', delta: true, text: 'Streaming line number ' + i + ' with some more words to grow.\n\n', at: Date.now() }]
    if (i % 25 === 0) evs.push({ kind: 'tool.call', threadId: tid, turnId: turn, itemId: 't' + i, tool: { id: turn + 't' + i, name: 'Bash', input: { command: 'ls' } }, at: Date.now() })
    window.runtime.EventsEmit('agent:events', evs)
    if (++i >= n) { clearInterval(id); res() }
  }, 16)
}), { n })
const feed = async () => page.evaluate(() => {
  const els = [...document.querySelectorAll('*')].filter((e) => e.scrollHeight > e.clientHeight + 50 && /(auto|scroll)/.test(getComputedStyle(e).overflowY))
  els.sort((a, b) => b.scrollHeight - a.scrollHeight)
  const el = els[0]; window.__feed = el
  return { top: Math.round(el.scrollTop), height: el.scrollHeight, client: el.clientHeight }
})
await stream(150)
await sleep(800)
const pinnedState = await feed()
console.log('after first stream (should be at bottom):', pinnedState, 'distance', pinnedState.height - pinnedState.top - pinnedState.client)
const box = await page.evaluate(() => { const r = window.__feed.getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2 } })
await page.mouse.move(box.x, box.y)
const s2 = stream(300)
await sleep(1200)
for (let k = 0; k < 6; k++) { await page.mouse.wheel(0, -300); await sleep(60) }
await sleep(700)
const afterUp = await feed()
const samples = []
for (let k = 0; k < 8; k++) { await sleep(400); samples.push((await feed()).top) }
await s2
await sleep(800)
const end = await feed()
console.log('after wheel-up:', afterUp.top, 'samples while streaming:', samples.join(','), 'end:', end.top, 'height', end.height)
const held = samples.every((t) => Math.abs(t - afterUp.top) < 30) && Math.abs(end.top - afterUp.top) < 30
console.log('HOLD POSITION WHILE STREAMING:', held ? 'PASS' : 'FAIL')
for (let k = 0; k < 40; k++) { await page.mouse.wheel(0, 600); await sleep(40) }
await sleep(1200)
const mid = await feed(); console.log('after wheel-down before stream, distance:', mid.height - mid.top - mid.client)
await stream(120)
await sleep(1000)
const re = await feed()
const dist = re.height - re.top - re.client
console.log('after scrolling back down + more stream, distance from bottom:', dist, dist < 60 ? 'PASS (following resumed)' : 'FAIL')
await browser.close().catch(() => {})
killPerf()
