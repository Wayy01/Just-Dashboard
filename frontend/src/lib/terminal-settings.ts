"use client"

import { useSyncExternalStore } from "react"

/**
 * How the terminal looks and behaves, kept in the browser.
 *
 * On the screen and not on the account, for the same reason the theme is: a
 * font size is a property of the display you are sitting at, and an operator
 * who turns it up on a laptop has not asked for 18px on the wall-mounted
 * dashboard. It is also why this is localStorage rather than the settings
 * table — the server has no business holding it, and a round trip to change a
 * font size would be absurd.
 *
 * One store rather than per-pane state so that every terminal on the page
 * agrees, including the one that mounts after the setting was changed.
 */
export type TerminalSettings = {
  fontSize: number
  fontFamily: string
  lineHeight: number
  letterSpacing: number
  cursorStyle: "block" | "underline" | "bar"
  cursorBlink: boolean
  scrollback: number
  /** Selecting text copies it, the way a native Linux terminal does. */
  copyOnSelect: boolean
  /**
   * Ask before pasting anything containing a newline.
   *
   * The failure this prevents is the well-known one: a multi-line block copied
   * out of a README is not "text in the prompt", it is every line but the last
   * *executed*, in order, with no chance to read them. Every serious terminal
   * has arrived at some form of this — bracketed paste in the shell, a
   * confirmation in the emulator — and a browser terminal wired to a
   * root-equivalent shell is the last place to leave it off.
   */
  confirmMultilinePaste: boolean
  /** A bell flashes the pane; the shell asking for attention is worth seeing. */
  visualBell: boolean
  /** And, where the browser allows it, raises a notification. */
  notifyOnBell: boolean
}

export const TERMINAL_FONTS = [
  { id: 'ui-monospace, "SF Mono", Menlo, Consolas, monospace', label: "System monospace" },
  { id: '"JetBrains Mono", ui-monospace, monospace', label: "JetBrains Mono" },
  { id: '"Fira Code", ui-monospace, monospace', label: "Fira Code" },
  { id: '"Cascadia Code", ui-monospace, monospace', label: "Cascadia Code" },
  { id: '"IBM Plex Mono", ui-monospace, monospace', label: "IBM Plex Mono" },
  { id: "monospace", label: "Browser default" },
] as const

export const FONT_MIN = 8
export const FONT_MAX = 28

const DEFAULTS: TerminalSettings = {
  fontSize: 13,
  fontFamily: TERMINAL_FONTS[0].id,
  lineHeight: 1.2,
  letterSpacing: 0,
  cursorStyle: "block",
  cursorBlink: true,
  // Deep enough to hold the output of a real build or a long tail. The buffer
  // is the reason "scroll up and read what scrolled past" works at all, and a
  // shallow one is indistinguishable from a broken pager.
  scrollback: 50000,
  copyOnSelect: false,
  confirmMultilinePaste: true,
  visualBell: true,
  notifyOnBell: false,
}

const KEY = "jd.terminal.settings"

let current: TerminalSettings = DEFAULTS
let loaded = false
const listeners = new Set<() => void>()

function load(): TerminalSettings {
  if (loaded || typeof window === "undefined") return current
  loaded = true
  try {
    const raw = window.localStorage.getItem(KEY)
    if (raw) current = { ...DEFAULTS, ...(JSON.parse(raw) as Partial<TerminalSettings>) }
  } catch {
    // A corrupt or unreadable store is not worth surfacing: the defaults are
    // a working terminal, which is what the operator came for.
  }
  return current
}

export function terminalSettings(): TerminalSettings {
  return load()
}

export function setTerminalSettings(patch: Partial<TerminalSettings>) {
  current = { ...load(), ...patch }
  try {
    window.localStorage.setItem(KEY, JSON.stringify(current))
  } catch {
    // Private browsing, or storage that is full. The setting still applies for
    // this session, which is better than refusing to change it.
  }
  for (const listener of listeners) listener()
}

export function resetTerminalSettings() {
  current = { ...DEFAULTS }
  try {
    window.localStorage.removeItem(KEY)
  } catch {
    // Nothing to do; `current` is already the defaults.
  }
  for (const listener of listeners) listener()
}

export function useTerminalSettings(): TerminalSettings {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    () => load(),
    () => DEFAULTS,
  )
}

/**
 * Command snippets: the handful of one-liners an operator types on every
 * server, kept so they can be sent rather than retyped.
 *
 * Deliberately local and deliberately unshared. A stored command that runs on
 * a root-equivalent shell is a small piece of automation, and one account
 * being able to leave a snippet where another account will click it is a
 * different and much larger feature than this is.
 */
export type Snippet = { id: string; label: string; command: string }

const SNIPPET_KEY = "jd.terminal.snippets"

const DEFAULT_SNIPPETS: Snippet[] = [
  { id: "df", label: "Disk usage", command: "df -h" },
  { id: "top", label: "Live processes", command: "htop || top" },
  { id: "ports", label: "Listening ports", command: "ss -tulpn" },
  { id: "compose", label: "Compose status", command: "docker compose ps" },
  { id: "journal", label: "System log", command: "journalctl -n 100 --no-pager" },
]

let snippets: Snippet[] | null = null
const snippetListeners = new Set<() => void>()

function loadSnippets(): Snippet[] {
  if (snippets) return snippets
  if (typeof window === "undefined") return DEFAULT_SNIPPETS
  try {
    const raw = window.localStorage.getItem(SNIPPET_KEY)
    snippets = raw ? (JSON.parse(raw) as Snippet[]) : DEFAULT_SNIPPETS
  } catch {
    snippets = DEFAULT_SNIPPETS
  }
  return snippets
}

export function setSnippets(next: Snippet[]) {
  snippets = next
  try {
    window.localStorage.setItem(SNIPPET_KEY, JSON.stringify(next))
  } catch {
    // As above: the change applies for this session either way.
  }
  for (const listener of snippetListeners) listener()
}

export function useSnippets(): Snippet[] {
  return useSyncExternalStore(
    (listener) => {
      snippetListeners.add(listener)
      return () => snippetListeners.delete(listener)
    },
    () => loadSnippets(),
    () => DEFAULT_SNIPPETS,
  )
}
