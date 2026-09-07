"use client"

import { useRef, useState } from "react"
import {
  ArrowUp,
  Code,
  Cross,
  FolderOpen,
  GitBranch,
  Lightning,
  TerminalWindow,
} from "@/components/icons"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

const STARTERS = [
  {
    label: "Explore files",
    detail: "See what's in this folder",
    command: "ls -lah",
    icon: FolderOpen,
  },
  {
    label: "Review changes",
    detail: "Check your working tree",
    command: "git status --short",
    icon: GitBranch,
  },
  {
    label: "Inspect services",
    detail: "List running containers",
    command: "docker ps",
    icon: Code,
  },
  {
    label: "Check disk space",
    detail: "Find available storage",
    command: "df -h",
    icon: Lightning,
  },
]

export function CommandStarters({
  onPick,
  onClose,
}: {
  onPick: (command: string) => void
  onClose: () => void
}) {
  return (
    <section
      className="terminal-starters relative mx-auto w-full max-w-2xl shrink-0 px-5 pt-5 pb-4 sm:px-7"
      aria-label="Command starters"
    >
      <button
        type="button"
        aria-label="Dismiss command starters"
        onClick={onClose}
        className="absolute top-3 right-3 flex size-8 items-center justify-center rounded-md text-muted-foreground hover:bg-accent"
      >
        <Cross className="size-4" />
      </button>
      <p className="mb-1 text-xs text-muted-foreground">Choose a starting point. Edit it below.</p>
      <h2 className="text-xl font-medium tracking-tight">What are you working on?</h2>
      <div className="mt-4 grid grid-cols-2 gap-2">
        {STARTERS.map(({ label, detail, command, icon: Icon }) => (
          <button
            key={label}
            type="button"
            onClick={() => onPick(command)}
            className="group rounded-xl border border-hairline bg-card/70 p-3 text-left transition-colors hover:border-primary/30 hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
          >
            <Icon className="mb-2 size-4 text-muted-foreground group-hover:text-foreground" />
            <span className="block text-xs font-medium">{label}</span>
            <span className="mt-1 hidden text-[11px] text-muted-foreground sm:block">{detail}</span>
          </button>
        ))}
      </div>
    </section>
  )
}

export function CommandComposer({
  draft,
  onDraft,
  onSubmit,
  onFocusTerminal,
  connected,
  contextLabel,
  inputRef,
}: {
  draft: string
  onDraft: (value: string) => void
  onSubmit: () => void
  onFocusTerminal: () => void
  connected: boolean
  contextLabel: string
  inputRef: React.RefObject<HTMLTextAreaElement | null>
}) {
  const composing = useRef(false)
  // Drafts are deliberately ephemeral: commands can contain secrets. They never
  // enter localStorage, telemetry, or a second persistent command history.
  const [expanded, setExpanded] = useState(false)
  return (
    <div className="terminal-composer shrink-0 px-3 pb-3 sm:px-4 sm:pb-4">
      <div className="rounded-xl border border-primary/20 bg-card shadow-sm transition-colors focus-within:border-primary/50 focus-within:ring-2 focus-within:ring-primary/5">
        <div className="flex items-center gap-2 px-4 pt-3 text-xs text-muted-foreground">
          <TerminalWindow className="size-3.5" />
          <span className="min-w-0 flex-1 truncate">{contextLabel}</span>
          <span className="text-[10px]">Command draft</span>
        </div>
        <div className="flex items-end gap-3 px-4 pt-2 pb-3">
          <span
            className="self-start pt-1 font-mono text-base text-muted-foreground"
            aria-hidden="true"
          >
            ❯
          </span>
          <textarea
            ref={inputRef}
            aria-label="Command draft"
            value={draft}
            rows={expanded || draft.includes("\n") ? 3 : 1}
            spellCheck={false}
            autoCapitalize="off"
            autoComplete="off"
            onChange={(event) => onDraft(event.target.value)}
            onCompositionStart={() => {
              composing.current = true
            }}
            onCompositionEnd={() => {
              composing.current = false
            }}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.preventDefault()
                onFocusTerminal()
              }
              if (
                event.key === "Enter" &&
                !event.shiftKey &&
                !composing.current &&
                !event.nativeEvent.isComposing
              ) {
                event.preventDefault()
                onSubmit()
              }
            }}
            placeholder="Write a command…"
            className="max-h-28 min-h-8 min-w-0 flex-1 resize-none bg-transparent py-1 font-mono text-sm leading-6 text-foreground outline-none placeholder:text-muted-foreground/70"
          />
          <Button
            type="button"
            aria-label="Send command to terminal"
            size="icon-sm"
            disabled={!connected || !draft.trim()}
            onClick={onSubmit}
            className="size-8 shrink-0 rounded-lg"
          >
            <ArrowUp className="size-4" />
          </Button>
        </div>
        <div className="flex items-center justify-between gap-2 border-t border-hairline px-4 py-2 text-[10px] text-muted-foreground">
          <button
            type="button"
            onClick={onFocusTerminal}
            className="truncate hover:text-foreground"
          >
            {connected ? "Sends to the focused terminal pane" : "Waiting for a connection"}
          </button>
          <button
            type="button"
            onClick={() => setExpanded(!expanded)}
            className={cn("shrink-0 hover:text-foreground", expanded && "text-foreground")}
            aria-pressed={expanded}
            aria-label={expanded ? "Collapse command editor" : "Expand command editor"}
          >
            Shift Enter ↵
          </button>
        </div>
      </div>
    </div>
  )
}
