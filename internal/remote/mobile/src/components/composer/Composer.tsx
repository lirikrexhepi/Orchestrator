import { useEffect, useRef, useState, type PointerEvent, type TouchEvent } from 'react'
import { Clock3, Mic, Paperclip, Send, Square } from '../../icons'
import { VoiceWave } from './VoiceWave'
import { GlassCircle, GlassSquircle, SEND_NUDGE } from '../../ui'
import { interrupt, removeQueued, send, useStore } from '../../store'
import { PcDownNotice } from '../status/PcDownNotice'
import { AttachmentStrip } from './AttachmentStrip'
import { QueuedList } from './QueuedList'
import { SubagentPill } from './SubagentPill'
import { useAttachments } from './useAttachments'
import { useDictation } from './useDictation'
import type { FileRef } from '../../types'

interface ComposerProps {
  threadId?: string
  placeholder: string
  onCreate?: (text: string, files: FileRef[]) => Promise<boolean>
}

const MAX_INPUT_HEIGHT = 132
const MIN_INPUT_HEIGHT = 40
const MIRRORED = ['fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'fontStretch', 'letterSpacing', 'lineHeight', 'paddingTop', 'paddingBottom', 'paddingLeft', 'paddingRight', 'borderTopWidth', 'borderBottomWidth', 'boxSizing', 'wordBreak', 'overflowWrap', 'whiteSpace', 'tabSize', 'textTransform', 'wordSpacing'] as const

let mirror: HTMLTextAreaElement | null = null

function contentHeight(el: HTMLTextAreaElement): number {
  if (!mirror) {
    mirror = document.createElement('textarea')
    mirror.setAttribute('aria-hidden', 'true')
    mirror.tabIndex = -1
    mirror.rows = 1
    Object.assign(mirror.style, { position: 'fixed', left: '-10000px', top: '0', visibility: 'hidden', pointerEvents: 'none', overflow: 'hidden', height: '0', minHeight: '0', maxHeight: 'none' })
    document.body.appendChild(mirror)
  }
  const style = getComputedStyle(el)
  for (const key of MIRRORED) mirror.style[key] = style[key]
  mirror.style.width = `${el.getBoundingClientRect().width}px`
  mirror.value = el.value
  return mirror.scrollHeight
}
const ICON = { size: 24, strokeWidth: 1.75 } as const
const BUTTON = 44
const TAP_MS = 350
const TAP_SLOP = 10

export function Composer({ threadId, placeholder, onCreate }: ComposerProps) {
  const thread = useStore((s) => (threadId ? s.threads[threadId] : undefined))
  const pcDown = useStore((s) => s.pcDown)
  const [text, setText] = useState('')
  const [creating, setCreating] = useState(false)
  const field = useRef<HTMLTextAreaElement | null>(null)
  const touch = useRef({ at: 0, x: 0, y: 0 })
  const attachments = useAttachments(field)
  const dictation = useDictation((spoken) => {
    if (!spoken) return
    setText((prev) => (prev.trim() ? `${prev.trimEnd()} ${spoken}` : spoken))
  })
  const liveText = dictation.listening && dictation.draft ? (text.trim() ? `${text.trimEnd()} ${dictation.draft}` : dictation.draft) : text

  const busy = Boolean(thread?.busy)
  const waiting = thread?.blocks.some(
    (b) => (b.type === 'approval_request' && b.status === 'pending') || (b.type === 'tool_question' && !b.answered),
  )
  const hasContent = liveText.trim().length > 0 || attachments.files.length > 0
  const canSend = hasContent && !attachments.uploading && !creating && !pcDown

  function grow() {
    const el = field.current
    if (!el) return
    const natural = contentHeight(el)
    const target = Math.max(MIN_INPUT_HEIGHT, Math.min(natural, MAX_INPUT_HEIGHT))
    el.style.overflowY = natural > MAX_INPUT_HEIGHT ? 'auto' : 'hidden'
    if (Math.abs(el.getBoundingClientRect().height - target) > 0.5 || !el.style.height) el.style.height = `${target}px`
  }

  useEffect(grow, [text, dictation.draft])

  const submit = () => {
    if (!canSend) return
    const body = liveText.trim()
    if (dictation.listening) dictation.cancel()
    if (!threadId) {
      if (!onCreate || (!body && attachments.files.length === 0)) return
      setCreating(true)
      const files = attachments.files.map((f) => f.ref)
      void onCreate(body, files).then((ok) => {
        setCreating(false)
        if (ok) {
          setText('')
          attachments.clear()
        }
      })
      return
    }
    void send(threadId, body, attachments.files.map((f) => f.ref))
    setText('')
    attachments.clear()
    requestAnimationFrame(grow)
  }

  const focusQuietly = () => {
    const el = field.current
    if (!el) return
    el.focus({ preventScroll: true })
    const end = el.value.length
    el.setSelectionRange(end, end)
  }

  const focusField = (e: PointerEvent<HTMLElement>) => {
    if ((e.target as Element).closest('button, textarea, input')) return
    const el = field.current
    if (el && document.activeElement === el) {
      if (el.selectionStart === el.selectionEnd) e.preventDefault()
      return
    }
    e.preventDefault()
    focusQuietly()
  }

  const onFieldTouchStart = (e: TouchEvent<HTMLTextAreaElement>) => {
    const t = e.touches[0]
    touch.current = { at: e.timeStamp, x: t.clientX, y: t.clientY }
  }

  const onFieldTouchEnd = (e: TouchEvent<HTMLTextAreaElement>) => {
    if (document.activeElement === e.currentTarget) return
    const t = e.changedTouches[0]
    const start = touch.current
    const tap = e.timeStamp - start.at < TAP_MS && Math.hypot(t.clientX - start.x, t.clientY - start.y) < TAP_SLOP
    if (!tap) return
    e.preventDefault()
    focusQuietly()
  }

  const onMic = () => {
    if (dictation.supported) dictation.toggle()
    else focusQuietly()
  }

  return (
    <div className="composer-stack">
      {waiting ? (
        <div className="hint" role="status">
          <Clock3 size={14} aria-hidden /> Waiting for your answer above
        </div>
      ) : null}
      {thread ? <QueuedList items={thread.queued} onRemove={(id) => threadId && removeQueued(threadId, id)} /> : null}
      <AttachmentStrip files={attachments.files} onRemove={attachments.remove} />
      {attachments.error ? <div className="hint composer-error">{attachments.error}</div> : null}
      <PcDownNotice placement="dock" />
      {thread ? <SubagentPill subagents={thread.subagents ?? []} blocks={thread.blocks} busy={busy} /> : null}
      <GlassSquircle radius={30} fill="var(--composer-fill)" className="composer-surface" onPointerDown={focusField}>
        <button className="composer-attach" onClick={attachments.open} disabled={attachments.uploading} aria-label="Attach a file">
          <Paperclip {...ICON} className={attachments.uploading ? 'spin' : undefined} aria-hidden />
        </button>
        <input
          ref={attachments.input}
          className="composer-file"
          type="file"
          accept="image/*,.heic,.heif,application/pdf,.pdf,text/plain,.txt,.md,.log"
          multiple
          tabIndex={-1}
          aria-hidden
          onChange={(e) => void attachments.add(e.target.files)}
        />
        <textarea
          ref={field}
          className="composer-input"
          rows={1}
          value={liveText}
          placeholder={pcDown ? 'PC not responding' : dictation.listening ? 'Listening…' : placeholder}
          aria-label="Message"
          enterKeyHint="send"
          onTouchStart={onFieldTouchStart}
          onTouchEnd={onFieldTouchEnd}
          onPaste={(e) => {
            const dt = e.clipboardData
            if (!dt) return
            const files = dt.files && dt.files.length > 0 ? Array.from(dt.files) : Array.from(dt.items || []).filter((i) => i.kind === 'file').map((i) => i.getAsFile()).filter((f): f is File => Boolean(f))
            if (files.length === 0) return
            e.preventDefault()
            void attachments.add(files)
          }}
          onChange={(e) => {
            const next = e.target.value
            if (dictation.listening && dictation.draft && next.endsWith(dictation.draft)) {
              setText(next.slice(0, next.length - dictation.draft.length).trimEnd())
            } else {
              setText(next)
            }
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !('ontouchstart' in window)) {
              e.preventDefault()
              submit()
            }
          }}
        />
        <GlassCircle
          size={BUTTON}
          fill={dictation.listening ? 'var(--voice-live)' : 'var(--voice)'}
          onClick={onMic}
          aria-label={dictation.listening ? 'Stop dictation' : 'Dictate'}
          aria-pressed={dictation.listening}
          className={dictation.listening ? 'composer-mic listening on-accent' : 'composer-mic on-accent'}
        >
          {dictation.listening ? <VoiceWave active activity={dictation.activity} /> : <Mic {...ICON} aria-hidden />}
        </GlassCircle>
        {busy && !canSend ? (
          <GlassCircle size={BUTTON} fill="var(--send)" onClick={() => threadId && void interrupt(threadId)} aria-label="Stop responding" className="on-accent">
            <Square size={16} fill="currentColor" aria-hidden />
          </GlassCircle>
        ) : (
          <GlassCircle size={BUTTON} fill="var(--send)" onClick={submit} disabled={!canSend} aria-label={busy ? 'Queue message' : 'Send'} className="composer-send on-accent">
            <Send {...ICON} style={SEND_NUDGE} aria-hidden />
          </GlassCircle>
        )}
      </GlassSquircle>
    </div>
  )
}
