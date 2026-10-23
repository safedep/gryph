import { useSyncExternalStore } from 'react'

// The inline script in each page head applies the same logic before the first paint. Keep both in step.
export type ThemeChoice = 'system' | 'light' | 'dark'

const KEY = 'gryph-theme'
const BAR = { light: '#FFFFFF', dark: '#0C0A10' }
const listeners = new Set<() => void>()
const darkQuery = () => matchMedia('(prefers-color-scheme: dark)')
let choice: ThemeChoice | null = null

function readStored(): ThemeChoice {
  try {
    const v = localStorage.getItem(KEY)
    return v === 'light' || v === 'dark' ? v : 'system'
  } catch {
    return 'system'
  }
}

const readChoice = () => (choice ??= readStored())

function apply() {
  const c = readChoice()
  const theme = c === 'system' ? (darkQuery().matches ? 'dark' : 'light') : c
  const root = document.documentElement
  root.dataset.theme = theme
  root.dataset.themeChoice = c
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', BAR[theme])
}

function notify() {
  apply()
  listeners.forEach((f) => f())
}

export function setTheme(next: ThemeChoice) {
  choice = next
  try {
    if (next === 'system') localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, next)
  } catch {
    // Private mode or blocked storage. The choice lasts until the page closes.
  }
  notify()
}

function onStorage(e: StorageEvent) {
  if (e.key !== null && e.key !== KEY) return
  choice = readStored()
  notify()
}

function subscribe(f: () => void) {
  listeners.add(f)
  const query = darkQuery()
  query.addEventListener('change', notify)
  addEventListener('storage', onStorage)
  return () => {
    listeners.delete(f)
    query.removeEventListener('change', notify)
    removeEventListener('storage', onStorage)
  }
}

// The server snapshot matches the prerendered page, so hydration is clean.
export const useTheme = () => useSyncExternalStore(subscribe, readChoice, () => 'system' as ThemeChoice)
