import { chromium } from '../../website/tools/node_modules/playwright-core/index.mjs'
import { spawn, execSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import os from 'node:os'

export const PORT = Number(process.env.PERF_PORT || 9333)
export const PROFILE = process.env.PERF_PROFILE || 'perf'
export const EXE = process.env.PERF_EXE || path.resolve('../../desktop/build/bin/orchestrator-perf.exe')
export const ROOT = path.join(process.env.APPDATA, 'composer-' + PROFILE)
export const LOG = path.join(ROOT, 'orchestrator_debug.log')

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

export function killPerf() {
  try { execSync(`taskkill /F /IM ${path.basename(EXE)}`, { stdio: 'ignore' }) } catch {}
}

export async function launch() {
  killPerf()
  await sleep(800)
  const logStart = fs.existsSync(LOG) ? fs.statSync(LOG).size : 0
  const t0 = Date.now()
  const child = spawn(EXE, [], {
    detached: true,
    stdio: 'ignore',
    env: { ...process.env, ORCHESTRATOR_PROFILE: PROFILE, ORCHESTRATOR_WEBVIEW_ARGS: `--remote-debugging-port=${PORT}` },
  })
  child.unref()
  let cdpAt = 0
  for (let i = 0; i < 400; i++) {
    try {
      const r = await fetch(`http://127.0.0.1:${PORT}/json/version`)
      if (r.ok) { cdpAt = Date.now(); break }
    } catch {}
    await sleep(25)
  }
  const browser = await chromium.connectOverCDP(`http://127.0.0.1:${PORT}`)
  let page
  for (let i = 0; i < 200 && !page; i++) {
    page = browser.contexts().flatMap((c) => c.pages()).find((p) => !p.url().startsWith('about:'))
    if (!page) await sleep(25)
  }
  return { browser, page, t0, cdpAt, logStart }
}

export function goTrace(logStart) {
  const buf = fs.readFileSync(LOG)
  const text = buf.subarray(logStart).toString('utf8')
  return text.split(/\r?\n/).filter((l) => l.includes('[Startup')).map((l) => l.replace(/^\[[^\]]+\]\s+\[INFO \]\s+/, ''))
}

export async function navTimings(page) {
  return page.evaluate(() => {
    const n = performance.getEntriesByType('navigation')[0]
    const paints = Object.fromEntries(performance.getEntriesByType('paint').map((p) => [p.name, Math.round(p.startTime)]))
    const res = performance.getEntriesByType('resource').map((r) => ({ name: r.name.split('/').slice(-1)[0].slice(0, 60), dur: Math.round(r.duration), start: Math.round(r.startTime), size: r.transferSize || r.encodedBodySize }))
    return {
      timeOrigin: performance.timeOrigin,
      domInteractive: Math.round(n?.domInteractive ?? 0),
      domContentLoaded: Math.round(n?.domContentLoadedEventEnd ?? 0),
      load: Math.round(n?.loadEventEnd ?? 0),
      paints,
      res,
      marks: performance.getEntriesByType('mark').map((m) => [m.name, Math.round(m.startTime)]),
    }
  })
}

export async function frameStats(page, action, ms = 1200) {
  await page.evaluate(() => {
    window.__frames = []
    window.__longtasks = []
    const tick = (t) => { window.__frames.push(t); if (window.__frameRun) requestAnimationFrame(tick) }
    window.__frameRun = true
    requestAnimationFrame(tick)
    try {
      window.__lto?.disconnect()
      window.__lto = new PerformanceObserver((l) => { for (const e of l.getEntries()) window.__longtasks.push(Math.round(e.duration)) })
      window.__lto.observe({ type: 'longtask' })
    } catch {}
  })
  const t = Date.now()
  await action()
  const actionMs = Date.now() - t
  await sleep(ms)
  return page.evaluate((actionMs) => {
    window.__frameRun = false
    const f = window.__frames
    const d = f.slice(1).map((x, i) => x - f[i])
    d.sort((a, b) => a - b)
    const pct = (p) => Math.round(d[Math.min(d.length - 1, Math.floor(d.length * p))] * 10) / 10
    return { actionMs, frames: f.length, p50: pct(0.5), p95: pct(0.95), max: Math.round(d[d.length - 1] || 0), over33: d.filter((x) => x > 33.4).length, longtasks: window.__longtasks }
  }, actionMs)
}

export const bindingTimer = (measureSize) => {
  window.__calls = []
  const wrap = (app) => {
    if (!app) return app
    for (const k of Object.keys(app)) {
      const fn = app[k]
      if (typeof fn !== 'function' || fn.__timed) continue
      app[k] = (...args) => {
        const start = performance.now()
        const p = fn(...args)
        Promise.resolve(p).then((r) => {
          let size = 0
          if (measureSize) { try { size = r == null ? 0 : JSON.stringify(r).length } catch {} }
          window.__calls.push({ name: k, start: Math.round(start), ms: Math.round(performance.now() - start), size })
        }, () => window.__calls.push({ name: k, start: Math.round(start), ms: Math.round(performance.now() - start), err: true }))
        return p
      }
      app[k].__timed = true
    }
    return app
  }
  let value
  Object.defineProperty(window, 'go', {
    configurable: true,
    get() { if (value?.main?.App) wrap(value.main.App); return value },
    set(v) { value = v },
  })
}

export async function instrumentedReload(page) {
  await page.context().addInitScript(bindingTimer, !!process.env.PERF_SIZES)
  await page.reload({ waitUntil: 'load' })
}
