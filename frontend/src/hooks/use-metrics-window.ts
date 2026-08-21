"use client"

import { useCallback, useState } from "react"
import { useRangePreference } from "@/hooks/use-metrics-history"
import type { MetricsWindow, RangeKey } from "@/lib/metrics-range"

export type WindowControls = {
  window: MetricsWindow
  /** True when the charts are showing a dragged span rather than a named range. */
  zoomed: boolean
  /** How many zoom steps can be undone. */
  depth: number
  setRange: (key: RangeKey) => void
  zoomTo: (from: number, to: number) => void
  /** Steps back to the previous window, ending at the named range. */
  zoomOut: () => void
  /** Abandons the whole zoom stack and returns to the named range. */
  reset: () => void
  /** Slides the current window by a fraction of its own width. */
  pan: (fraction: number) => void
}

/**
 * The window the charts are drawn over, and the history of how it was reached.
 *
 * A stack rather than a single "zoomed" flag, because zooming is exploratory:
 * you narrow to an hour, then to five minutes inside it, and the way back is
 * to the hour rather than all the way out to the day you started from. Losing
 * the intermediate step is what makes zoom-and-reset tedious enough that
 * people stop using it.
 *
 * Deliberately component state rather than a persisted preference: the named
 * range is a standing choice worth remembering across reloads (and is, in
 * localStorage), whereas a zoom is a question being asked right now. Restoring
 * yesterday's zoom on a fresh page load would show a window with no data in it
 * and no obvious way out.
 */
export function useMetricsWindow(): WindowControls {
  const [range, setRange] = useRangePreference()
  const [stack, setStack] = useState<{ from: number; to: number }[]>([])

  const top = stack[stack.length - 1]
  const window: MetricsWindow = top ? { key: range, from: top.from, to: top.to } : { key: range }

  const chooseRange = useCallback(
    (key: RangeKey) => {
      // Picking a range is a statement about what you want to see, so it
      // discards the zoom rather than applying it inside the new range.
      setStack([])
      setRange(key)
    },
    [setRange],
  )

  const zoomTo = useCallback((from: number, to: number) => {
    if (to <= from) return
    setStack((prev) => [...prev, { from, to }])
  }, [])

  const zoomOut = useCallback(() => setStack((prev) => prev.slice(0, -1)), [])
  const reset = useCallback(() => setStack([]), [])

  /**
   * Moves the window along the timeline without changing its width.
   *
   * Only meaningful while zoomed — a named range is anchored to now, and
   * panning it would silently turn it into a fixed span that stops following
   * the clock while still being labelled "1h".
   */
  const pan = useCallback((fraction: number) => {
    setStack((prev) => {
      const current = prev[prev.length - 1]
      if (!current) return prev
      const width = current.to - current.from
      const shift = width * fraction
      const moved = { from: current.from + shift, to: current.to + shift }
      // Never past the present: there is nothing recorded there, and a chart
      // half full of empty future reads as data that stopped arriving.
      const now = Date.now()
      if (moved.to > now) {
        return [...prev.slice(0, -1), { from: now - width, to: now }]
      }
      return [...prev.slice(0, -1), moved]
    })
  }, [])

  return {
    window,
    zoomed: Boolean(top),
    depth: stack.length,
    setRange: chooseRange,
    zoomTo,
    zoomOut,
    reset,
    pan,
  }
}
