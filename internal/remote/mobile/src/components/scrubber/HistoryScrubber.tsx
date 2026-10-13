import { useCallback, useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import type { ThreadSummary } from '../../types'
import { VelocityTracker, capture, prefersReducedMotion, springAt } from '../../ui'
import { hapticTick } from '../../platform/haptics'
import { useHiddenChats } from '../../hiddenChats'
import { ModelStack } from '../../screens/drawer/ModelStack'
import { relative } from '../../format'
import { SCRUB, driftStarts, edgeDrift, gain, magnet, slotPose, tickLength } from './scrubMath'

interface HistoryScrubberProps {
  chats: ThreadSummary[]
  currentId?: string | null
  onPick: (threadId: string) => void
}

interface Gesture {
  pointer: number
  x0: number
  y0: number
  x: number
  y: number
  lastY: number
  frameTop: number
  driftUp: number
  driftDown: number
  timer: number
  active: boolean
  done: boolean
  raw: number
  focus: number
  velocity: number
  max: number
  center: number
  armed: boolean
  armness: number
  armDistance: number
  last: number
  lastMove: number
  raf: number
  rounded: number
  hapticDue: boolean
  tracker: VelocityTracker
}

interface Session {
  chats: ThreadSummary[]
  focus0: number
}

const EASE = 'cubic-bezier(0.22, 1, 0.36, 1)'
const VISIBLE_RANGE = 2.8

function recent(chats: ThreadSummary[], hidden: ReadonlySet<string>): ThreadSummary[] {
  return chats
    .filter((c) => c.kind !== 'coordinator' && !hidden.has(c.threadId))
    .slice()
    .sort((a, b) => (b.lastActivity ?? 0) - (a.lastActivity ?? 0))
    .slice(0, SCRUB.maxChats)
}

function agentCount(c: ThreadSummary): number {
  return (c.busy ? 1 : 0) + (c.runningSubagents ?? 0)
}

function metaOf(c: ThreadSummary): string {
  const total = agentCount(c)
  const working = total > 0 ? `${total} agent${total === 1 ? '' : 's'} running` : ''
  return [c.projectName || 'No project', working, relative(c.lastActivity)].filter(Boolean).join(' · ')
}

function clampRaw(raw: number, max: number): number {
  return Math.min(max + SCRUB.overscroll, Math.max(-SCRUB.overscroll, raw))
}

function cssPx(el: Element, name: string): number {
  return parseFloat(getComputedStyle(el).getPropertyValue(name)) || 0
}

export function HistoryScrubber({ chats, currentId, onPick }: HistoryScrubberProps) {
  const zone = useRef<HTMLDivElement | null>(null)
  const veil = useRef<HTMLDivElement | null>(null)
  const list = useRef<HTMLDivElement | null>(null)
  const items = useRef<(HTMLDivElement | null)[]>([])
  const shown = useRef<boolean[]>([])
  const gesture = useRef<Gesture | null>(null)
  const [session, setSession] = useState<Session | null>(null)
  const sessionRef = useRef<Session | null>(null)
  sessionRef.current = session
  const hidden = useHiddenChats()
  const latest = useRef({ chats, currentId, onPick, hidden })
  latest.current = { chats, currentId, onPick, hidden }

  const render = useCallback(() => {
    const g = gesture.current
    const s = sessionRef.current
    if (!g || !s) return
    items.current.forEach((el, i) => {
      if (!el) return
      const d = i - g.focus
      const visible = Math.abs(d) < VISIBLE_RANGE
      if (shown.current[i] !== visible) {
        shown.current[i] = visible
        el.style.visibility = visible ? 'visible' : 'hidden'
      }
      if (!visible) return
      const pose = slotPose(d, g.armness)
      const pull = Math.abs(d) < 0.5 ? Math.min(Math.max(0, g.x0 - g.x), g.armDistance) * 0.22 : 0
      el.style.transform = `translate3d(${pose.x - pull}px, ${g.center + pose.y - SCRUB.itemHeight / 2}px, 0) scale(${pose.scale})`
      el.style.opacity = String(pose.opacity)
      el.classList.toggle('armed', g.armed && Math.abs(d) < 0.5)
    })
  }, [])

  const advance = useCallback((g: Gesture, now: number) => {
    const dt = Math.min(0.05, Math.max(0, (now - g.last) / 1000))
    g.last = now
    const drift = g.armed ? 0 : edgeDrift(g.y, g.driftUp, g.driftDown)
    if (drift !== 0) {
      g.raw = clampRaw(g.raw + drift * dt, g.max)
      g.lastMove = now
    }
    const idle = now - g.lastMove > SCRUB.idleMs
    if (idle) g.raw = Math.min(g.max, Math.max(0, Math.round(g.raw)))
    const speed = Math.abs(g.tracker.velocity().y) + Math.abs(drift) * SCRUB.pxPerChat
    const target = idle ? g.raw : magnet(g.raw, speed, g.max)
    if (dt > 0) {
      const s = springAt(g.focus, target, g.velocity, idle ? SCRUB.settle : SCRUB.follow, dt)
      g.focus = s.value
      g.velocity = s.velocity
      if (Math.abs(target - g.focus) < 0.0005 && Math.abs(g.velocity) < 0.005) {
        g.focus = target
        g.velocity = 0
      }
      g.armness += ((g.armed ? 1 : 0) - g.armness) * (1 - Math.exp(-dt * 16))
    }
    const rounded = Math.round(target)
    if (rounded !== g.rounded) {
      g.rounded = rounded
      g.hapticDue = true
    }
  }, [])

  const tick = useCallback(
    (now: number) => {
      const g = gesture.current
      if (!g || !sessionRef.current || !g.active || g.done) return
      advance(g, now)
      render()
      g.raf = requestAnimationFrame(tick)
    },
    [advance, render],
  )

  const fadeAndClose = (ms: number, then?: () => void) => {
    const opts: KeyframeAnimationOptions = { duration: ms, easing: EASE, fill: 'forwards' }
    veil.current?.animate([{ opacity: 1 }, { opacity: 0 }], opts)
    list.current?.animate([{ opacity: 1 }, { opacity: 0 }], opts)
    window.setTimeout(() => {
      setSession(null)
      then?.()
    }, ms)
  }

  const stop = (g: Gesture) => {
    window.clearTimeout(g.timer)
    if (g.raf) cancelAnimationFrame(g.raf)
    g.raf = 0
  }

  const commit = (g: Gesture) => {
    const s = sessionRef.current
    const index = Math.round(g.focus)
    const picked = s?.chats[index]
    const el = items.current[index]
    if (el) {
      el.animate(
        [
          { transform: el.style.transform, opacity: el.style.opacity || '1' },
          { transform: `${el.style.transform} translateX(-28px)`, opacity: 0 },
        ],
        { duration: 220, easing: EASE, fill: 'forwards' },
      )
    }
    fadeAndClose(220, () => {
      if (picked && picked.threadId !== latest.current.currentId) latest.current.onPick(picked.threadId)
    })
  }

  const activate = () => {
    const g = gesture.current
    const frameEl = zone.current?.closest('.chat-frame')
    if (!g || !frameEl) return
    const pool = recent(latest.current.chats, latest.current.hidden)
    if (pool.length === 0) {
      gesture.current = null
      zone.current?.classList.remove('active')
      return
    }
    const rect = frameEl.getBoundingClientRect()
    const head = cssPx(frameEl, '--head-h')
    const dock = rect.height - cssPx(frameEl, '--dock-h')
    const top = head + SCRUB.step * 2.3
    const bottom = dock - SCRUB.step * 2.3
    const found = Math.max(0, pool.findIndex((c) => c.threadId === latest.current.currentId))
    g.max = pool.length - 1
    g.raw = found
    g.focus = found
    g.velocity = 0
    g.rounded = found
    const drift = driftStarts(g.y0, head, dock)
    g.driftUp = drift.up
    g.driftDown = drift.down
    g.center = Math.min(Math.max(g.y0, top), Math.max(top, bottom))
    g.armDistance = Math.max(SCRUB.armMin, window.innerWidth * SCRUB.armFraction)
    g.last = performance.now()
    g.lastMove = g.last
    g.active = true
    setSession({ chats: pool, focus0: g.focus })
  }

  useLayoutEffect(() => {
    const g = gesture.current
    if (!session || !g) return
    shown.current = []
    render()
    const calm = prefersReducedMotion()
    veil.current?.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 220, easing: EASE })
    list.current?.animate(
      [
        { opacity: 0, transform: calm ? 'none' : 'translateX(-10px)' },
        { opacity: 1, transform: 'none' },
      ],
      { duration: 260, easing: EASE },
    )
    if (!g.raf) g.raf = requestAnimationFrame(tick)
  }, [session, render, tick])

  useEffect(() => {
    const el = zone.current
    if (!el) return
    const block = (e: TouchEvent) => e.preventDefault()
    el.addEventListener('touchstart', block, { passive: false })
    el.addEventListener('touchmove', block, { passive: false })
    return () => {
      el.removeEventListener('touchstart', block)
      el.removeEventListener('touchmove', block)
    }
  }, [])

  useEffect(
    () => () => {
      const g = gesture.current
      if (g) stop(g)
    },
    [],
  )

  const onDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.stopPropagation()
    if (gesture.current || !e.isPrimary) return
    const frameEl = zone.current?.closest('.chat-frame')
    const top = frameEl ? frameEl.getBoundingClientRect().top : 0
    const y = e.clientY - top
    capture(e.currentTarget, e.pointerId)
    zone.current?.classList.add('active')
    const tracker = new VelocityTracker()
    tracker.reset(0, y)
    gesture.current = {
      pointer: e.pointerId,
      x0: e.clientX,
      y0: y,
      x: e.clientX,
      y,
      lastY: y,
      frameTop: top,
      driftUp: 0,
      driftDown: 0,
      timer: window.setTimeout(activate, SCRUB.hold),
      active: false,
      done: false,
      raw: 0,
      focus: 0,
      velocity: 0,
      max: 0,
      center: 0,
      armed: false,
      armness: 0,
      armDistance: SCRUB.armMin,
      last: 0,
      lastMove: 0,
      raf: 0,
      rounded: 0,
      hapticDue: false,
      tracker,
    }
  }

  const onMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const g = gesture.current
    if (!g || e.pointerId !== g.pointer || g.done) return
    const y = e.clientY - g.frameTop
    if (!g.active) {
      if (Math.hypot(e.clientX - g.x0, y - g.y0) < SCRUB.slop) return
      window.clearTimeout(g.timer)
      activate()
      if (gesture.current !== g || !g.active) return
    }
    const now = performance.now()
    g.x = e.clientX
    g.y = y
    g.tracker.push(0, y)
    const dy = y - g.lastY
    g.lastY = y
    if (!g.armed && dy !== 0) {
      g.raw = clampRaw(g.raw + (dy / SCRUB.pxPerChat) * gain(Math.abs(g.tracker.velocity().y)), g.max)
      g.lastMove = now
    }
    const dx = g.x0 - g.x
    const wasArmed = g.armed
    if (!g.armed && dx >= g.armDistance) g.armed = true
    else if (g.armed && dx < g.armDistance * SCRUB.disarmRatio) g.armed = false
    advance(g, now)
    render()
    if (g.armed !== wasArmed || g.hapticDue) {
      g.hapticDue = false
      hapticTick()
    }
  }

  const onEnd = (e: ReactPointerEvent<HTMLDivElement>) => {
    const g = gesture.current
    if (!g || e.pointerId !== g.pointer) return
    stop(g)
    gesture.current = null
    zone.current?.classList.remove('active')
    if (!g.active || g.done) return
    g.done = true
    if (g.armed && e.type === 'pointerup') {
      hapticTick()
      commit(g)
    } else {
      fadeAndClose(180)
    }
  }

  const anyWorking = chats.some((c) => agentCount(c) > 0)

  return (
    <>
      {session ? (
        <>
          <div ref={veil} className="scrub-veil" aria-hidden />
          <div ref={list} className="scrub-list" role="listbox" aria-label="Recent chats">
            {session.chats.map((c, i) => (
              <div
                key={c.threadId}
                ref={(el) => {
                  items.current[i] = el
                }}
                role="option"
                aria-selected={c.threadId === currentId}
                className="scrub-item"
                data-working={agentCount(c) > 0 || undefined}
              >
                <ModelStack drivers={c.drivers && c.drivers.length > 0 ? c.drivers : c.driver ? [c.driver] : []} />
                <span className="scrub-text">
                  <span className="scrub-label">{c.title || c.preview || 'Untitled chat'}</span>
                  <span className="scrub-meta" data-working={agentCount(c) > 0 || undefined}>{metaOf(c)}</span>
                </span>
              </div>
            ))}
          </div>
        </>
      ) : null}
      <div
        ref={zone}
        className={anyWorking ? 'scrub-zone has-working' : 'scrub-zone'}
        aria-label="Hold and slide to switch chats"
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onEnd}
        onPointerCancel={onEnd}
        onContextMenu={(e) => e.preventDefault()}
      >
        {anyWorking ? <span className="scrub-working" aria-label="Agents working" /> : null}
        {Array.from({ length: SCRUB.ticks }, (_, k) => (
          <span key={k} className="scrub-tick" style={{ width: tickLength(k, false) }} />
        ))}
      </div>
    </>
  )
}
