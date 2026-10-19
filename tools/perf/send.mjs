import { launch, sleep, killPerf, instrumentedReload, frameStats } from './lib.mjs'
import { startProfile, stopProfile } from './cpuprof.mjs'
const { browser, page } = await launch()
await page.waitForLoadState('load')
await sleep(1500)
await instrumentedReload(page)
await sleep(2500)
await page.evaluate(() => {
  window.__ev = []
  window.runtime.EventsOn('agent:events', (b) => { for (const e of b) window.__ev.push({ t: performance.now(), kind: e.kind, threadId: e.threadId, turnId: e.turnId, delta: e.delta }) })
})
const ta = page.locator('textarea').first()
await ta.click()
const marker = 'perfprobe' + Date.now()
await ta.fill(`Reply with exactly the word ${marker} and nothing else.`)
const t = await page.evaluate((marker) => {
  window.__seen = {}
  const t0 = performance.now()
  const mo = new MutationObserver(() => {
    const txt = document.body.innerText
    if (!window.__seen.user && txt.includes('Reply with exactly the word')) window.__seen.user = performance.now() - t0
    const n = txt.split(marker).length - 1
    if (!window.__seen.reply && n >= 2) window.__seen.reply = performance.now() - t0
  })
  mo.observe(document.body, { subtree: true, childList: true, characterData: true })
  window.__t0 = t0
  return t0
}, marker)
const stats = await frameStats(page, () => ta.press('Enter'), 12000)
const seen = await page.evaluate(() => window.__seen)
const ev = await page.evaluate(() => window.__ev.map((e) => ({ ...e, t: Math.round(e.t - window.__t0) })))
console.log('send frames', JSON.stringify(stats))
console.log('seen (ms after Enter)', JSON.stringify(seen))
console.log('events', JSON.stringify(ev.filter((e) => !e.delta).slice(0, 12)))
const tid = ev.find((e) => e.threadId && e.threadId !== 'coordinator')?.threadId
const turn = ev.find((e) => e.turnId)?.turnId
console.log('thread', tid, turn)
await page.screenshot({ path: 'shot-send.png' })
if (tid) {
  const cdp = await startProfile(page)
  const s2 = await frameStats(page, () => page.evaluate(({ tid, turn }) => new Promise((res) => {
    const words = 'Lorem ipsum dolor sit amet, consectetur **adipiscing** elit, sed do `eiusmod` tempor incididunt ut labore et dolore magna aliqua.\n\n- item one\n- item two\n\n'
    let i = 0
    window.runtime.EventsEmit('agent:events', [{ kind: 'turn.started', threadId: tid, turnId: turn + '-syn', at: Date.now() }])
    const id = setInterval(() => {
      const text = words.slice((i * 13) % words.length, ((i * 13) % words.length) + 13)
      window.runtime.EventsEmit('agent:events', [{ kind: 'agent.message', threadId: tid, turnId: turn + '-syn', itemId: 'syn-msg', delta: true, text, at: Date.now() }])
      if (++i >= 500) { clearInterval(id); window.runtime.EventsEmit('agent:events', [{ kind: 'turn.completed', threadId: tid, turnId: turn + '-syn', at: Date.now() }]); res() }
    }, 16)
  }), { tid, turn }), 800)
  const prof = await stopProfile(cdp, 'stream.cpuprofile')
  console.log('synthetic stream 500 deltas @16ms', JSON.stringify(s2))
  console.log('busy', prof.busyMs, '\n' + prof.rows.slice(0, 20).join('\n'))
}
await browser.close().catch(() => {})
killPerf()
