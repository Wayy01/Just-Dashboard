"use client"

import { useSyncExternalStore } from "react"

/**
 * What is currently being dragged in the terminal rail.
 *
 * The browser will not let a `dragover` handler read the payload — only its
 * MIME types — which is enough to decide whether a drop is legal and not
 * enough to say anything useful about it ("move *build* into *deploy*"), or to
 * grey out the folder a session is already in. Every drag-and-drop
 * implementation solves this the same way: keep the payload beside the drag
 * rather than inside it, and read it back on the way over.
 *
 * It is module state and not context because a drag fires `dragover` at ~60Hz
 * across the whole rail, and a context above it would re-render the terminal
 * pane on every one of those. The same reasoning as `lib/metrics-crosshair.ts`,
 * for the same reason: pointer-rate updates do not belong in the tree.
 */
export type DragPayload =
  | { kind: "session"; tmuxName: string; folder: string; title: string }
  | { kind: "folder"; name: string }
  | { kind: "window"; session: string; index: number; position: number; name: string }

/**
 * The MIME types the drag advertises. They exist so a `dragover` can refuse a
 * drop it cannot handle *before* the payload is readable — a window dropped on
 * a folder header, say — and so a drag out of the page into another
 * application carries nothing that looks like a file.
 */
export const DRAG_TYPES: Record<DragPayload["kind"], string> = {
  session: "application/x-jd-terminal-session",
  folder: "application/x-jd-terminal-folder",
  window: "application/x-jd-terminal-window",
}

let current: DragPayload | null = null
const listeners = new Set<() => void>()

function emit() {
  for (const listener of listeners) listener()
}

/** Called from `dragstart`: stamps the payload on the event and records it. */
export function beginDrag(event: React.DragEvent, payload: DragPayload, colour?: string | null) {
  current = payload
  event.dataTransfer.effectAllowed = "move"
  event.dataTransfer.setData(DRAG_TYPES[payload.kind], JSON.stringify(payload))
  // Some browsers refuse a drag with no `text/plain`, and a readable label is
  // what a drop into an editor or a chat window would otherwise not have.
  event.dataTransfer.setData("text/plain", labelOf(payload))
  setDragChip(event, labelOf(payload), colour)
  emit()
}

/**
 * Draws the thing under the cursor during a drag.
 *
 * Left to the browser it snapshots the row, and a row whose background is
 * transparent — which every row here is until it is hovered — gets composited
 * onto an opaque white rectangle. The result is a white box with hard corners
 * dragging across a dark page, which looks like a rendering fault rather than a
 * drag. Chrome will not let that be styled: `-webkit-drag` ignores the
 * element's own radius and there is no property for the backing.
 *
 * So the drag image is a purpose-built chip instead. It has to be in the
 * document to be snapshotted and off-screen so it is never seen directly, and
 * it is removed on the next frame because the browser only needs it for the
 * instant it takes the picture.
 */
function setDragChip(event: React.DragEvent, label: string, colour?: string | null) {
  if (typeof document === "undefined") return
  const chip = document.createElement("div")
  chip.textContent = label
  Object.assign(chip.style, {
    position: "fixed",
    top: "-1000px",
    left: "-1000px",
    maxWidth: "220px",
    overflow: "hidden",
    whiteSpace: "nowrap",
    textOverflow: "ellipsis",
    padding: "6px 10px",
    borderRadius: "8px",
    // Opaque, because the whole problem is what shows through a transparent
    // one. The tokens keep it in whichever theme is in force.
    background: "var(--card)",
    color: "var(--foreground)",
    border: `1px solid ${colour ?? "var(--border)"}`,
    borderLeft: `3px solid ${colour ?? "var(--primary)"}`,
    font: "500 12px/1.2 var(--font-sans, system-ui)",
    boxShadow: "0 8px 20px rgb(0 0 0 / 0.35)",
    pointerEvents: "none",
  } satisfies Partial<CSSStyleDeclaration>)
  document.body.appendChild(chip)
  event.dataTransfer.setDragImage(chip, 12, chip.offsetHeight / 2)
  requestAnimationFrame(() => chip.remove())
}

export function endDrag() {
  current = null
  emit()
}

export function dragPayload(): DragPayload | null {
  return current
}

function labelOf(payload: DragPayload): string {
  switch (payload.kind) {
    case "session":
      return payload.title
    case "folder":
      return payload.name
    case "window":
      return payload.name
  }
}

/**
 * The live payload, for components that change appearance while a drag is in
 * flight — a folder that lifts when a session is over the rail, an insertion
 * line between two windows.
 */
export function useDragPayload(): DragPayload | null {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    () => current,
    () => null,
  )
}

/**
 * Reads a payload back on drop. Falls back to the module copy, which is what
 * carries a drag that began in this page; the serialised form is what would
 * survive a drag from another window, and costs nothing to honour.
 */
export function readDrop(event: React.DragEvent, kind: DragPayload["kind"]): DragPayload | null {
  const raw = event.dataTransfer.getData(DRAG_TYPES[kind])
  if (raw) {
    try {
      const parsed = JSON.parse(raw) as DragPayload
      if (parsed?.kind === kind) return parsed
    } catch {
      // A malformed payload is not worth reporting: the module copy below is
      // the one every in-page drag actually uses.
    }
  }
  return current?.kind === kind ? current : null
}

/** True when the event carries something of this kind. */
export function carries(event: React.DragEvent, kind: DragPayload["kind"]): boolean {
  return (
    event.dataTransfer.types.includes(DRAG_TYPES[kind]) ||
    (current?.kind === kind && event.dataTransfer.types.includes("text/plain"))
  )
}
