"use client"

import { useCallback, useMemo, useSyncExternalStore } from "react"
import { DEFAULT_MODE, THEME_STORAGE_KEY, type ThemeMode } from "@/lib/themes"

/**
 * The active mode: light or dark.
 *
 * The document itself is the store: the blocking script in the root layout
 * has already put `.light` or `.dark` on <html> before anything painted —
 * always exactly one of the two, never neither and never both — so reading
 * it back is both the cheapest source of truth and the one that cannot
 * disagree with what is on screen. React subscribes to it rather than
 * holding a second copy that has to be synchronised in an effect.
 *
 * The choice is kept in localStorage rather than on the account: it belongs
 * to the screen you are sitting at, and picking light on a laptop should not
 * change what your phone looks like.
 */

const CHANGE_EVENT = "just-dashboard:themechange"

/** Writes a mode to the document. */
export function applyMode(mode: ThemeMode): ThemeMode {
  const root = document.documentElement
  root.className = mode === "dark" ? "dark" : "light"
  root.style.colorScheme = mode
  return mode
}

function subscribe(onChange: () => void) {
  // `storage` covers the other tabs; the custom event covers this one, which
  // never receives its own storage event.
  window.addEventListener("storage", onChange)
  window.addEventListener(CHANGE_EVENT, onChange)
  return () => {
    window.removeEventListener("storage", onChange)
    window.removeEventListener(CHANGE_EVENT, onChange)
  }
}

function getSnapshot(): ThemeMode {
  return document.documentElement.classList.contains("dark") ? "dark" : "light"
}

function getServerSnapshot(): ThemeMode {
  return DEFAULT_MODE
}

type ThemeContextValue = {
  mode: ThemeMode
  setMode: (mode: ThemeMode) => void
  /** Flips to the other mode — the only control the UI actually offers. */
  toggle: () => void
}

export function useTheme(): ThemeContextValue {
  const mode = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)

  const setMode = useCallback((next: ThemeMode) => {
    applyMode(next)
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, next)
    } catch {
      // Storage can be unavailable in a locked-down browser profile. The mode
      // still applies for this page; it just will not be remembered.
    }
    window.dispatchEvent(new Event(CHANGE_EVENT))
  }, [])

  const toggle = useCallback(() => {
    setMode(getSnapshot() === "dark" ? "light" : "dark")
  }, [setMode])

  return useMemo(() => ({ mode, setMode, toggle }), [mode, setMode, toggle])
}
