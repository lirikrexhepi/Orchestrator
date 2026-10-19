import { glass, shape, springAt, SPRINGS } from './kit.js'

const LINKS = {
  download: '',
}

const root = document.documentElement
const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches
const clamp = (v, lo = 0, hi = 1) => Math.min(hi, Math.max(lo, v))

const ACCENT = 'linear-gradient(180deg, #2a2d35 0%, #0b0d12 100%)'
const BLUE = 'linear-gradient(180deg, #3b97ff 0%, #0a6bdc 100%)'
const PILL = 9999

document.querySelectorAll('[data-download]').forEach((el) => {
  if (LINKS.download) {
    el.href = LINKS.download
    el.setAttribute('download', '')
  }
})

const island = document.getElementById('island')
glass(island, { radius: PILL, fill: 'rgba(12, 12, 15, 0.97)', pressable: false, rim: false })

document.querySelectorAll('.btn').forEach((el) => {
  const accent = el.classList.contains('accent')
  const inIsland = Boolean(el.closest('.island'))
  glass(el, { radius: PILL, fill: inIsland ? BLUE : accent ? ACCENT : 'var(--glass-control)' })
})

document.querySelectorAll('.frame').forEach((el) => {
  const radius = Number(el.dataset.shape) || 30
  el.style.borderRadius = '0'
  shape(el, radius)
  const rim = el.querySelector('.rim')
  if (rim) glass(rim, { radius, fill: 'transparent', pressable: false, light: { intensity: 0.5, backIntensity: 0.45 } })
})

const phoneRadius = () => (innerWidth < 900 ? 40 : 46)
document.querySelectorAll('.phone').forEach((el) => {
  const outer = phoneRadius()
  el.style.borderRadius = '0'
  shape(el, outer)
  glass(el, { radius: outer, fill: '#0a0a0a', pressable: false, light: { intensity: 0.6, backIntensity: 0.5 } })
  const screen = el.querySelector('.screen')
  if (screen) {
    screen.style.borderRadius = '0'
    shape(screen, outer - 8)
  }
})

requestAnimationFrame(() => root.classList.add('ready'))

function springFrames(cfg, frame) {
  const frames = []
  let t = 0
  for (;;) {
    const { value, velocity } = springAt(0, 1, 0, cfg, t)
    frames.push(frame(value))
    if ((Math.abs(value - 1) < 0.0006 && Math.abs(velocity) < 0.006) || t > 2.4) break
    t += 1 / 90
  }
  frames.push(frame(1))
  return { frames, duration: t * 1000 }
}

function spring(el, { y = 0, x = 0, from = 1, cfg = SPRINGS.bouncy, delay = 0, rotate = 0, done } = {}) {
  const finish = () => {
    el.classList.add('in')
    done?.()
  }
  if (reduce) {
    finish()
    return
  }
  const { frames, duration } = springFrames(cfg, (p) => ({
    opacity: String(clamp(p * 1.6)),
    transform: `translate3d(${((1 - p) * x).toFixed(2)}px, ${((1 - p) * y).toFixed(2)}px, 0) rotate(${((1 - p) * rotate).toFixed(3)}deg) scale(${(from + (1 - from) * p).toFixed(4)})`,
  }))
  const anim = el.animate(frames, { duration, delay, easing: 'linear', fill: 'backwards' })
  anim.onfinish = finish
}

const delayOf = (el) => parseFloat(getComputedStyle(el).getPropertyValue('--d')) || 0

const revealer = new IntersectionObserver(
  (entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue
      const el = entry.target
      revealer.unobserve(el)
      spring(el, { y: 56, from: 0.95, cfg: SPRINGS.bouncy, delay: delayOf(el) })
    }
  },
  { rootMargin: '0px 0px -10% 0px', threshold: 0.05 },
)
document.querySelectorAll('[data-reveal]').forEach((el) => revealer.observe(el))

const navClip = document.querySelector('.nav-clip')
if (navClip) {
  const full = navClip.scrollWidth
  let collapsed = false
  let running = null
  const setNav = (shut) => {
    if (shut === collapsed) return
    collapsed = shut
    const start = navClip.getBoundingClientRect().width
    const end = shut ? 0 : full
    running?.cancel()
    if (reduce) {
      navClip.style.width = `${end}px`
      return
    }
    const { frames, duration } = springFrames(SPRINGS.snappy, (p) => ({
      width: `${(start + (end - start) * p).toFixed(2)}px`,
      opacity: String(clamp(shut ? 1 - p : p)),
    }))
    running = navClip.animate(frames, { duration, easing: 'linear', fill: 'forwards' })
  }
  addEventListener('scroll', () => setNav(scrollY > 140), { passive: true })
}

const lifts = [...document.querySelectorAll('[data-lift]')]
const phones = document.querySelector('.phones')
const cards = phones ? [...phones.querySelectorAll('.track .phone')] : []
const caption = phones?.querySelector('.caption')
const titleEl = phones?.querySelector('[data-title]')
const lineEl = phones?.querySelector('[data-line]')
const ticksEl = phones?.querySelector('.ticks')
const track = phones?.querySelector('.track')

const wall = document.querySelector('.wall')
const wallCards = wall ? [...wall.querySelectorAll('.wall-track .frame')] : []
const wallTrack = wall?.querySelector('.wall-track')
const wallState = { t: 0, v: 0, target: 0 }
let lastFrame = performance.now()

function layoutWall(t) {
  const th = wallTrack.clientHeight
  const tw = wallTrack.clientWidth
  const w = Math.min(th * 0.94 * 1.6, tw * 0.72)
  const n = wallCards.length
  wallCards.forEach((card, i) => {
    let d = (((i - t) % n) + n) % n
    if (d > n / 2) d -= n
    const a = Math.abs(d)
    const scale = Math.max(0.55, 1 - a * 0.17)
    const x = Math.sign(d) * (a <= 1 ? a * 0.7 : 0.7 + (a - 1) * 0.5) * w
    card.style.width = `${w.toFixed(1)}px`
    card.style.transform = `translate3d(calc(-50% + ${x.toFixed(1)}px), -50%, 0) scale(${scale.toFixed(4)})`
    card.style.opacity = clamp(1.6 - Math.max(0, a - 0.9) * 0.75).toFixed(3)
    card.style.zIndex = String(100 - Math.round(a * 10))
    card.style.filter = a < 0.5 ? '' : `brightness(${(1 - Math.min(a, 2) * 0.12).toFixed(3)})`
  })
}

function readWall() {
  if (!wall) return
  const rect = wall.getBoundingClientRect()
  const total = wall.offsetHeight - innerHeight
  wallState.target = clamp(-rect.top / total) * 4
}

function stepWall(now) {
  const dt = Math.min(0.05, (now - lastFrame) / 1000)
  lastFrame = now
  if (!wall) return
  if (reduce) {
    wallState.t = wallState.target
  } else {
    const k = 190
    const c = 2 * Math.sqrt(k) * 0.78
    const acc = (wallState.target - wallState.t) * k - wallState.v * c
    wallState.v += acc * dt
    wallState.t += wallState.v * dt
    if (Math.abs(wallState.target - wallState.t) < 0.0006 && Math.abs(wallState.v) < 0.004) {
      wallState.t = wallState.target
      wallState.v = 0
    }
  }
  layoutWall(wallState.t)
}

const state = { t: 0, target: 0, shown: -1, swap: 0 }
let ticks = []
if (phones && ticksEl) {
  phones.style.setProperty('--n', String(cards.length))
  ticks = cards.map(() => ticksEl.appendChild(document.createElement('i')))
}

function setCaption(i) {
  if (!caption || state.shown === i) return
  const first = state.shown === -1
  state.shown = i
  ticks.forEach((tick, k) => tick.classList.toggle('on', k === i))
  const apply = () => {
    titleEl.textContent = cards[i].dataset.title
    lineEl.textContent = cards[i].dataset.line
    caption.classList.remove('swap')
  }
  if (first || reduce) {
    apply()
    return
  }
  caption.classList.add('swap')
  clearTimeout(state.swap)
  state.swap = setTimeout(apply, 170)
}

function layoutPhones(t) {
  const w = track.clientWidth
  const narrow = w < 900
  const first = cards[0]
  const pw = first.offsetWidth
  const step = pw * (narrow ? 1.12 : 1.16)
  const cx = w * (narrow ? 0.5 : 0.7)
  cards.forEach((card, i) => {
    const d = i - t
    const a = Math.abs(d)
    const scale = 1 - Math.min(a, 2) * 0.1
    const fade = narrow ? clamp(1 - Math.max(0, a - 0.35) * 1.6) : d < 0 ? clamp(1 - a * 1.7) : clamp(1 - Math.max(0, a - 0.4) * 0.5)
    const x = cx - pw / 2 + d * step
    card.style.transform = `translate3d(${x.toFixed(1)}px, -50%, 0) scale(${scale.toFixed(4)})`
    card.style.opacity = fade.toFixed(3)
    card.style.zIndex = String(100 - Math.round(a * 10))
    card.style.pointerEvents = 'none'
  })
}

function readPhones() {
  if (!phones) return
  const rect = phones.getBoundingClientRect()
  const total = phones.offsetHeight - innerHeight
  const p = clamp(-rect.top / total)
  state.target = p * (cards.length - 1)
}

function readLifts() {
  const vh = innerHeight
  for (const el of lifts) {
    const rect = el.getBoundingClientRect()
    const p = clamp((vh - rect.top) / (vh * 0.62))
    const eased = 1 - Math.pow(1 - p, 3)
    el.style.setProperty('--p', eased.toFixed(4))
  }
}

let ticking = false
function onScroll() {
  if (ticking) return
  ticking = true
  requestAnimationFrame(() => {
    ticking = false
    readLifts()
    readPhones()
    readWall()
    if (reduce) {
      state.t = state.target
    }
  })
}

function frame(now) {
  stepWall(now)
  if (phones && cards.length) {
    const diff = state.target - state.t
    if (Math.abs(diff) > 0.0005) state.t += diff * (reduce ? 1 : 0.11)
    else state.t = state.target
    layoutPhones(state.t)
    setCaption(clamp(Math.round(state.t), 0, cards.length - 1))
  }
  requestAnimationFrame(frame)
}

addEventListener('scroll', onScroll, { passive: true })
addEventListener('resize', onScroll)
readLifts()
readPhones()
readWall()
wallState.t = wallState.target
if (wall) layoutWall(wallState.t)
state.t = state.target
if (phones && cards.length) {
  layoutPhones(state.t)
  setCaption(clamp(Math.round(state.t), 0, cards.length - 1))
}
requestAnimationFrame(frame)

