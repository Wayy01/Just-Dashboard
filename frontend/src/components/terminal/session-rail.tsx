"use client"

import { useMemo, useState } from "react"
import {
  Check,
  ChevronDown,
  Circle,
  Folder,
  FolderOpen,
  Pencil,
  Play,
  Star,
  TerminalSquare,
  X,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { relativeTime, truncateMiddle } from "@/lib/format"
import type { TerminalWorkspace } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { IconAction } from "@/components/icon-action"

/**
 * The list of terminals, as a workspace rail rather than a strip of chips.
 *
 * A chip strip works for three sessions and falls apart at ten: the names
 * truncate, the order is arbitrary, and there is nowhere to say which are
 * running and which are merely alive. Every tool in this class stops before
 * this point — Cockpit has one terminal, ttyd and Wetty have no session
 * concept, Portainer's console dies with the tab — so the references worth
 * copying are outside it: VS Code's terminal tab list, where a tab has a name
 * you set and keeps, and Guacamole's connection groups, where a long list is
 * made short by folding it.
 *
 * Three affordances carry that:
 *
 *   - **Favourites float to the top**, because two or three of these are where
 *     somebody actually lives and the rest are archaeology.
 *   - **Folders**, so eight sessions for one deployment collapse to one line.
 *   - **A name you type**, inline, because a rename that costs a dialog is a
 *     rename nobody does — and then every session is `vpsd-3f2a91c4` again.
 */
export function SessionRail({
  sessions,
  activeId,
  onSelect,
  onRename,
  onToggleFavourite,
  onSetFolder,
  onClose,
  onNew,
}: {
  sessions: TerminalWorkspace[]
  activeId: string | null
  /** Attach to a live session, or reattach one that is merely running. */
  onSelect: (session: TerminalWorkspace) => void
  onRename: (session: TerminalWorkspace, title: string) => void
  onToggleFavourite: (session: TerminalWorkspace) => void
  onSetFolder: (session: TerminalWorkspace, folder: string) => void
  onClose: (session: TerminalWorkspace) => void
  onNew: (folder?: string) => void
}) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})

  // Favourites first as their own group, then folders alphabetically, then
  // everything unfiled. Within a group the newest is first: the session you
  // just opened is the one you are looking for.
  const groups = useMemo(() => {
    const favourites = sessions.filter((s) => s.favourite)
    const rest = sessions.filter((s) => !s.favourite)
    const byFolder = new Map<string, TerminalWorkspace[]>()
    for (const session of rest) {
      const key = session.folder || ""
      byFolder.set(key, [...(byFolder.get(key) ?? []), session])
    }
    const named = [...byFolder.entries()]
      .filter(([folder]) => folder !== "")
      .sort((a, b) => a[0].localeCompare(b[0]))
    const unfiled = byFolder.get("") ?? []

    const out: { key: string; label: string; folder?: string; items: TerminalWorkspace[] }[] = []
    if (favourites.length) out.push({ key: "★", label: "Favourites", items: favourites })
    for (const [folder, items] of named) out.push({ key: folder, label: folder, folder, items })
    if (unfiled.length) {
      out.push({ key: "—", label: named.length || favourites.length ? "Other" : "Sessions", items: unfiled })
    }
    return out
  }, [sessions])

  return (
    <div className="flex min-h-0 w-full shrink-0 flex-col gap-2 lg:w-64">
      {groups.map((group) => {
        const isCollapsed = collapsed[group.key]
        return (
          <div key={group.key} className="min-w-0">
            <div className="flex items-center gap-1">
              <button
                className="flex min-w-0 flex-1 items-center gap-1.5 py-1 text-left"
                onClick={() => setCollapsed((c) => ({ ...c, [group.key]: !c[group.key] }))}
              >
                <ChevronDown
                  className={cn(
                    "size-3 shrink-0 text-muted-foreground transition-transform",
                    isCollapsed && "-rotate-90",
                  )}
                />
                {group.key === "★" ? (
                  <Star className="size-3 shrink-0 fill-warning text-warning" />
                ) : group.folder ? (
                  <FolderOpen className="size-3 shrink-0 text-muted-foreground" />
                ) : null}
                <span className="eyebrow truncate">{group.label}</span>
                <span className="text-[10px] text-muted-foreground">{group.items.length}</span>
              </button>
              {group.folder && (
                <IconAction
                  label={`New session in ${group.folder}`}
                  className="size-6"
                  onClick={() => onNew(group.folder)}
                >
                  <Play />
                </IconAction>
              )}
            </div>

            {!isCollapsed && (
              <div className="space-y-1">
                {group.items.map((session) => (
                  <SessionRow
                    key={session.tmuxName || session.id}
                    session={session}
                    active={activeId !== null && session.id === activeId}
                    onSelect={() => onSelect(session)}
                    onRename={(title) => onRename(session, title)}
                    onToggleFavourite={() => onToggleFavourite(session)}
                    onSetFolder={(folder) => onSetFolder(session, folder)}
                    onClose={() => onClose(session)}
                  />
                ))}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

function SessionRow({
  session,
  active,
  onSelect,
  onRename,
  onToggleFavourite,
  onSetFolder,
  onClose,
}: {
  session: TerminalWorkspace
  active: boolean
  onSelect: () => void
  onRename: (title: string) => void
  onToggleFavourite: () => void
  onSetFolder: (folder: string) => void
  onClose: () => void
}) {
  const [renaming, setRenaming] = useState(false)
  const [filing, setFiling] = useState(false)
  const [draft, setDraft] = useState(session.title)

  if (renaming || filing) {
    const commit = () => {
      const value = draft.trim()
      if (value) (renaming ? onRename : onSetFolder)(value)
      setRenaming(false)
      setFiling(false)
    }
    return (
      <div className="flex items-center gap-1 rounded-lg border border-primary/40 bg-card px-1.5 py-1">
        <Input
          autoFocus
          value={draft}
          spellCheck={false}
          placeholder={renaming ? "Name this session" : "Folder name"}
          className="h-6 border-0 bg-transparent px-1 text-xs shadow-none focus-visible:ring-0"
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") commit()
            if (e.key === "Escape") {
              setRenaming(false)
              setFiling(false)
            }
          }}
          onBlur={commit}
        />
        <Check className="size-3 shrink-0 text-muted-foreground" />
      </div>
    )
  }

  return (
    <div
      className={cn(
        "group flex min-w-0 items-center gap-1.5 rounded-lg border px-2 py-1.5 transition-colors",
        active
          ? "border-primary/40 bg-primary/10"
          : "border-transparent hover:border-hairline hover:bg-[var(--row-hover)]",
      )}
    >
      <button onClick={onSelect} className="flex min-w-0 flex-1 items-center gap-2 text-left">
        {/*
          The one signal that matters at a glance: a filled dot is a session
          this dashboard is holding open, a hollow one is running on the host
          with nothing attached. Both are alive — the difference is only
          whether clicking costs a reattach.
        */}
        <Circle
          className={cn(
            "size-2 shrink-0",
            session.live ? "fill-success text-success" : "text-muted-foreground",
          )}
        />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13px] leading-tight">{session.title}</span>
          <span className="block truncate font-mono text-[10px] leading-tight text-muted-foreground">
            {session.cwd ? truncateMiddle(session.cwd, 28) : relativeTime(session.createdAt)}
            {session.windows > 1 && ` · ${session.windows}w`}
          </span>
        </span>
      </button>

      <span className="flex shrink-0 items-center opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
        <IconAction
          label={session.favourite ? "Remove from favourites" : "Add to favourites"}
          className="size-6"
          onClick={onToggleFavourite}
        >
          <Star className={cn(session.favourite && "fill-warning text-warning")} />
        </IconAction>
        <IconAction
          label="Rename"
          className="size-6"
          onClick={() => {
            setDraft(session.title)
            setRenaming(true)
          }}
        >
          <Pencil />
        </IconAction>
        <IconAction
          label="Put in a folder"
          className="size-6"
          onClick={() => {
            setDraft(session.folder ?? "")
            setFiling(true)
          }}
        >
          <Folder />
        </IconAction>
        <IconAction label="Close session" className="size-6 text-destructive" onClick={onClose}>
          <X />
        </IconAction>
      </span>
      {session.favourite && (
        <Star className="size-3 shrink-0 fill-warning text-warning group-hover:hidden" />
      )}
    </div>
  )
}

/**
 * The windows inside the active session — tmux's own tabs, which have always
 * been there and were unreachable without knowing `C-b c`.
 *
 * Selecting one is a write, not a view change: tmux redraws every attached
 * client when the active window changes, so the pane already showing follows
 * along without reconnecting.
 */
export function WindowStrip({
  windows,
  onSelect,
  onRename,
  onNew,
  onClose,
}: {
  windows: { index: number; name: string; active: boolean; panes: number; cwd?: string }[]
  onSelect: (index: number) => void
  onRename: (index: number, name: string) => void
  onNew: () => void
  onClose: (index: number) => void
}) {
  const [renaming, setRenaming] = useState<number | null>(null)
  const [draft, setDraft] = useState("")

  if (windows.length === 0) return null

  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1">
      {windows.map((win) =>
        renaming === win.index ? (
          <Input
            key={win.index}
            autoFocus
            value={draft}
            spellCheck={false}
            className="h-6 w-32 text-xs"
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                if (draft.trim()) onRename(win.index, draft.trim())
                setRenaming(null)
              }
              if (e.key === "Escape") setRenaming(null)
            }}
            onBlur={() => setRenaming(null)}
          />
        ) : (
          <div
            key={win.index}
            className={cn(
              "group flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] transition-colors",
              win.active
                ? "border-primary/40 bg-primary/10 text-foreground"
                : "border-hairline text-muted-foreground hover:bg-accent",
            )}
          >
            <button
              className="flex items-center gap-1"
              onClick={() => onSelect(win.index)}
              onDoubleClick={() => {
                setDraft(win.name)
                setRenaming(win.index)
              }}
              title={win.cwd}
            >
              <TerminalSquare className="size-3" />
              {win.name}
              {win.panes > 1 && <span className="text-[9px]">({win.panes})</span>}
            </button>
            {windows.length > 1 && (
              <button
                aria-label={`Close window ${win.name}`}
                className="opacity-0 transition-opacity group-hover:opacity-100 hover:text-destructive"
                onClick={() => onClose(win.index)}
              >
                <X className="size-3" />
              </button>
            )}
          </div>
        ),
      )}
      <Button size="xs" variant="ghost" className="h-6 px-1.5" onClick={onNew}>
        + window
      </Button>
    </div>
  )
}
