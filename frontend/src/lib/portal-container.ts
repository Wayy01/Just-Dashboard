"use client"

import { useSyncExternalStore } from "react"

/**
 * Where a portalled surface — a dialog, a menu, a popover, a tooltip — should
 * actually be mounted.
 *
 * Radix portals to `document.body`, which is correct everywhere except inside
 * the browser's real fullscreen: the fullscreen element is the only thing the
 * compositor paints, so a menu appended to `body` is rendered *behind* it and
 * is, from the operator's side, a button that does nothing. That is not a
 * terminal-page problem — it is every portal in the product, and it appears
 * the moment any page offers a fullscreen view — so the fix lives in the
 * primitives rather than at the call sites, and `container` is threaded
 * through each of them from here.
 *
 * Returns `null` when nothing is fullscreen, which is what Radix already
 * treats as "use the body".
 */
export function usePortalContainer(): HTMLElement | null {
  return useSyncExternalStore(subscribe, snapshot, () => null)
}

function subscribe(onChange: () => void) {
  // `fullscreenchange` fires on the element in the standard, but bubbles to
  // the document, so one listener covers whichever element was requested.
  document.addEventListener("fullscreenchange", onChange)
  document.addEventListener("webkitfullscreenchange", onChange)
  return () => {
    document.removeEventListener("fullscreenchange", onChange)
    document.removeEventListener("webkitfullscreenchange", onChange)
  }
}

function snapshot(): HTMLElement | null {
  const el = document.fullscreenElement
  return el instanceof HTMLElement ? el : null
}
