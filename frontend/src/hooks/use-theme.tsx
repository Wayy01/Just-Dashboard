"use client"

import { useCallback, useMemo, useSyncExternalStore } from "react"
import {
  DEFAULT_THEME,
  THEME_STORAGE_KEY,
  resolveTheme,
  type Theme,
  type ThemeMode,
} from "@/lib/themes"

/**
 * The active palette.
 *
 * The document itself is the store: the blocking script in the root layout has
 * already put `data-theme` on <html> before anything painted, so reading it
 * back is both the cheapest source of truth and the one that cannot disagree
 * with what is on screen. React subscribes to it rather than holding a second
 * copy that has to be synchronised in an effect.
 *
 * The choice is kept in localStorage rather than on the account: it belongs to
 * the screen you are sitting at, and picking a light theme on a laptop should
 * not change what your phone looks like.
 */

const CHANGE_EVENT = "just-dashboard:themechange"

/** Writes a theme to the document. Returns the theme that actually applied. */
export function applyTheme(id: string): Theme {
  const theme = resolveTheme(id)
  const root = document.documentElement
  root.dataset.theme = theme.id
  root.classList.toggle("dark", theme.mode === "dark")
  root.style.colorScheme = theme.mode
  return theme
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

function getSnapshot(): string {
  return document.documentElement.dataset.theme || DEFAULT_THEME
}

function getServerSnapshot(): string {
  return DEFAULT_THEME
}

type ThemeContextValue = {
  /** The active theme, always a real entry from the registry. */
  theme: Theme
  themeId: string
  mode: ThemeMode
  setTheme: (id: string) => void
}

export function useTheme(): ThemeContextValue {
  const themeId = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)

  const setTheme = useCallback((id: string) => {
    const theme = applyTheme(id)
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, theme.id)
    } catch {
      // Storage can be unavailable in a locked-down browser profile. The theme
      // still applies for this page; it just will not be remembered.
    }
    window.dispatchEvent(new Event(CHANGE_EVENT))
  }, [])

  return useMemo(() => {
    const theme = resolveTheme(themeId)
    return { theme, themeId: theme.id, mode: theme.mode, setTheme }
  }, [themeId, setTheme])
}
