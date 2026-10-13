import { useSyncExternalStore } from 'react'
import { readLocal, writeLocal } from './cache'
import { APP_ICONS, DEFAULT_APP_ICON, appIconSrc } from './appIcons'

const KEY = 'orchestrator_app_icon'
const listeners = new Set<() => void>()

function valid(id: unknown): id is string {
  return typeof id === 'string' && APP_ICONS.some((icon) => icon.id === id)
}

let current: string = (() => {
  const stored = readLocal<string>(KEY, DEFAULT_APP_ICON)
  return valid(stored) ? stored : DEFAULT_APP_ICON
})()

function setLink(selector: string, href: string) {
  const el = document.querySelector<HTMLLinkElement>(selector)
  if (el) el.href = href
}

function applyToDom(id: string) {
  if (typeof document === 'undefined') return
  const src = appIconSrc(id)
  setLink('link[rel="icon"][sizes="192x192"]', src)
  setLink('link[rel="apple-touch-icon"]', src)
}

export function getAppIcon(): string {
  return current
}

export function useAppIcon(): string {
  return useSyncExternalStore(
    (notify) => {
      listeners.add(notify)
      return () => listeners.delete(notify)
    },
    () => current,
  )
}

export function setAppIcon(id: string) {
  if (!valid(id) || id === current) return
  current = id
  writeLocal(KEY, id)
  applyToDom(id)
  listeners.forEach((l) => l())
}

export function initAppIcon() {
  applyToDom(current)
}
