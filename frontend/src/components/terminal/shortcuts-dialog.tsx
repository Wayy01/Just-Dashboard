"use client"

import { useEffect, useMemo, useState } from "react"
import { Cross, RotateCounterClockwise, Warning } from "@/components/icons"
import { cn } from "@/lib/utils"
import {
  SHORTCUTS,
  type ShortcutAction,
  bindShortcut,
  chordIsUsable,
  chordOf,
  formatChord,
  isCustomised,
  resetAllShortcuts,
  resetShortcut,
  useKeymap,
} from "@/lib/terminal-keymap"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

/**
 * The shortcut sheet, which is also where they are changed.
 *
 * One surface rather than two: a read-only list of chords is a thing people
 * open once, and a settings page they have to go and find is a thing they never
 * open at all. Clicking a chord records the next one pressed, which is the only
 * interaction anybody expects here.
 */
export function ShortcutsDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const map = useKeymap()
  const [recording, setRecording] = useState<ShortcutAction | null>(null)
  const [rejected, setRejected] = useState<string | null>(null)

  const groups = useMemo(() => {
    const out = new Map<string, typeof SHORTCUTS>()
    for (const spec of SHORTCUTS) out.set(spec.group, [...(out.get(spec.group) ?? []), spec])
    return [...out.entries()]
  }, [])

  /** Which other action already answers to a chord — a binding cannot be in two places. */
  const conflictOf = (chord: string, self: ShortcutAction) =>
    SHORTCUTS.find((s) => s.action !== self && map[s.action] === chord)

  useEffect(() => {
    if (!recording) return
    const onKey = (event: KeyboardEvent) => {
      // Every key while recording belongs to the recorder, including the ones
      // the browser wants — otherwise Ctrl+Alt+T would open a terminal in the
      // desktop and never reach the dialog.
      event.preventDefault()
      event.stopPropagation()
      if (event.key === "Escape") {
        setRecording(null)
        setRejected(null)
        return
      }
      const chord = chordOf(event)
      if (!chord) return
      const usable = chordIsUsable(chord)
      if (!usable.ok) {
        setRejected(usable.why ?? "That chord cannot be used here.")
        return
      }
      // A conflict is resolved by taking the binding rather than refusing it:
      // the operator is looking at the list and can see what moved.
      const clash = conflictOf(chord, recording)
      if (clash) bindShortcut(clash.action, "")
      bindShortcut(recording, chord)
      setRecording(null)
      setRejected(null)
    }
    window.addEventListener("keydown", onKey, { capture: true })
    return () => window.removeEventListener("keydown", onKey, { capture: true })
  }, [recording, map]) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setRecording(null)
        setRejected(null)
        onOpenChange(next)
      }}
    >
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Keyboard shortcuts</DialogTitle>
          <DialogDescription>
            Click a chord to rebind it. Ctrl+Alt and Ctrl+Shift are the two families neither the
            browser nor the shell inside the pane has a use for.
          </DialogDescription>
        </DialogHeader>

        {rejected && (
          <p className="flex items-center gap-2 rounded-md border border-warning/50 bg-warning/10 px-2.5 py-1.5 text-xs text-warning">
            <Warning className="size-3.5 shrink-0" />
            {rejected}
          </p>
        )}

        <div className="max-h-[55vh] space-y-4 overflow-y-auto pr-1">
          {groups.map(([group, specs]) => (
            <section key={group}>
              <p className="eyebrow mb-1.5">{group}</p>
              <div className="space-y-0.5">
                {specs.map((spec) => {
                  const chord = map[spec.action]
                  const isRecording = recording === spec.action
                  return (
                    <div
                      key={spec.action}
                      className="group flex items-center gap-2 rounded-md px-1.5 py-1 text-xs transition-colors hover:bg-[var(--row-hover)]"
                    >
                      <span className="min-w-0 flex-1 truncate">{spec.label}</span>
                      {isCustomised(spec.action) && !isRecording && (
                        <button
                          className="opacity-0 transition-opacity group-hover:opacity-100"
                          title="Back to the default"
                          onClick={() => resetShortcut(spec.action)}
                        >
                          <RotateCounterClockwise className="size-3 text-muted-foreground hover:text-foreground" />
                        </button>
                      )}
                      {chord && !isRecording && (
                        <button
                          className="opacity-0 transition-opacity group-hover:opacity-100"
                          title="Unbind"
                          onClick={() => bindShortcut(spec.action, "")}
                        >
                          <Cross className="size-3 text-muted-foreground hover:text-destructive" />
                        </button>
                      )}
                      <button
                        onClick={() => {
                          setRejected(null)
                          setRecording(isRecording ? null : spec.action)
                        }}
                        className={cn(
                          "min-w-28 shrink-0 rounded-md border px-2 py-0.5 text-center font-mono text-[11px] transition-colors",
                          isRecording
                            ? "animate-pulse border-primary bg-primary/15 text-primary"
                            : chord
                              ? "raised border-hairline bg-control text-foreground hover:border-primary/50"
                              : "border-dashed border-hairline text-muted-foreground hover:border-primary/50",
                        )}
                      >
                        {isRecording ? "press a chord" : formatChord(chord)}
                      </button>
                    </div>
                  )
                })}
              </div>
            </section>
          ))}
        </div>

        {/*
          The conventions that are not rebindable, and are exactly the things
          somebody discovers by finding them broken. tmux owns the wheel inside
          a pane — that is what makes it scroll history instead of walking
          through commands — but the pointer belongs to the page, so selecting
          and copying work as they do in any other application.
        */}
        <section className="rounded-md border border-hairline bg-surface-sunken p-2.5">
          <p className="eyebrow mb-1.5">Mouse &amp; clipboard</p>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
            {[
              ["Wheel", "Scroll back through the session's history"],
              ["Click", "Focus the pane under the pointer"],
              ["Drag", "Select, and it stays selected"],
              ["Ctrl + C", "Copy the selection — or interrupt, when nothing is selected"],
              ["Ctrl + V", "Paste"],
              ["Middle click", "Paste"],
              ["Alt + drag", "Give the mouse to the program in the pane (vim, htop)"],
              ["Ctrl + wheel", "Text size"],
            ].map(([keys, what]) => (
              <div key={keys} className="contents">
                <dt className="font-mono text-[11px] whitespace-nowrap text-foreground">{keys}</dt>
                <dd className="text-muted-foreground">{what}</dd>
              </div>
            ))}
          </dl>
        </section>

        <DialogFooter className="sm:justify-between">
          <Button size="sm" variant="ghost" onClick={() => resetAllShortcuts()}>
            <RotateCounterClockwise className="size-3.5" />
            Reset all
          </Button>
          <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
