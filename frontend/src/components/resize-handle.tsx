"use client"

import { useCallback, useRef, useState } from "react"
import { cn } from "@/lib/utils"

/**
 * The grab strip between a side panel and what it sits next to.
 *
 * Four things it has to get right, all of which are how these go wrong:
 *
 * - **Pointer capture, not a window listener.** A drag that leaves the strip —
 *   which every drag does immediately — has to keep being delivered, and has
 *   to end on a pointerup that happened over the terminal, an iframe, or
 *   outside the window entirely.
 * - **The panel keeps its own width.** The handle reports where the pointer
 *   is; the page decides what that means, because only the page knows what
 *   else is on the row and how little of it the terminal can be left with.
 * - **A keyboard can move it.** It is a separator with a value, so arrows
 *   nudge and Home resets; a control only a mouse can reach is one an operator
 *   on a laptop trackpad quietly stops using.
 * - **Double-click resets.** The way back from a width that turned out wrong,
 *   without hunting for the exact original number.
 *
 * `side` is which side of the handle the panel it resizes is on, which is what
 * decides whether dragging right makes it bigger or smaller.
 */
export function ResizeHandle({
  side,
  label,
  value,
  min,
  max,
  onChange,
  onReset,
  className,
}: {
  side: "left" | "right"
  label: string
  value: number
  min: number
  max: number
  /** `commit` is false while the pointer is still down — see lib/panel-size. */
  onChange: (px: number, commit?: boolean) => void
  onReset: () => void
  className?: string
}) {
  const [dragging, setDragging] = useState(false)
  const start = useRef({ x: 0, width: 0 })

  // Dragging right grows a panel on the left and shrinks one on the right.
  const sign = side === "left" ? 1 : -1

  const onPointerDown = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      if (event.button !== 0) return
      event.preventDefault()
      start.current = { x: event.clientX, width: value }
      setDragging(true)
      event.currentTarget.setPointerCapture(event.pointerId)
    },
    [value],
  )

  const onPointerMove = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      if (!dragging) return
      onChange(start.current.width + sign * (event.clientX - start.current.x), false)
    },
    [dragging, onChange, sign],
  )

  const stop = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      if (!dragging) return
      setDragging(false)
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId)
      }
      // One write, with the width the drag ended on.
      onChange(start.current.width + sign * (event.clientX - start.current.x), true)
    },
    [dragging, onChange, sign],
  )

  const onKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const step = event.shiftKey ? 48 : 16
    if (event.key === "ArrowLeft") {
      event.preventDefault()
      onChange(value - sign * step)
    } else if (event.key === "ArrowRight") {
      event.preventDefault()
      onChange(value + sign * step)
    } else if (event.key === "Home") {
      event.preventDefault()
      onReset()
    }
  }

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={Math.round(value)}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      title={`${label} — drag, or double-click to reset`}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={stop}
      onPointerCancel={stop}
      onDoubleClick={onReset}
      onKeyDown={onKeyDown}
      className={cn(
        // Wider than it looks: the visible rule is one pixel, the target is
        // eight, which is the difference between a resize you can grab and one
        // you chase. Hidden below `lg`, where the layout stacks and a vertical
        // separator resizes nothing.
        "group relative hidden w-2 shrink-0 cursor-col-resize touch-none select-none lg:block",
        "focus-visible:ring-ring/60 rounded-full outline-none focus-visible:ring-2",
        className,
      )}
    >
      <span
        aria-hidden
        className={cn(
          "absolute inset-y-0 left-1/2 w-px -translate-x-1/2 rounded-full bg-border transition-colors",
          "group-hover:bg-primary/60 group-focus-visible:bg-primary",
          dragging && "bg-primary",
        )}
      />
      {dragging && (
        // While a drag is live the pointer is over the terminal half the time,
        // and xterm's own cursor would fight this one.
        <span className="fixed inset-0 z-50 cursor-col-resize" aria-hidden />
      )}
    </div>
  )
}
