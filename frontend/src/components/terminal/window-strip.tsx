"use client"

import { useRef, useState } from "react"
import {
  AlertTriangle,
  Bell,
  Columns2,
  LayoutGrid,
  Link2,
  Link2Off,
  Maximize2,
  MoreHorizontal,
  Palette,
  Pencil,
  Plus,
  Rows2,
  SquareTerminal,
  Unlink,
  X,
} from "lucide-react"
import { cn } from "@/lib/utils"
import type { TerminalPane, TerminalWindow } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { IconAction } from "@/components/icon-action"
import { ColourMenuItems, TagSwatch, tagStyle, tagVar } from "@/components/terminal/tags"
import { beginDrag, carries, endDrag, readDrop } from "@/components/terminal/dnd"

/**
 * The windows inside the active session — tmux's own tabs, which have always
 * been there and were unreachable without knowing `C-b c`.
 *
 * Selecting one is a write, not a view change: tmux redraws every attached
 * client when the active window changes, so the pane already showing follows
 * along without reconnecting.
 *
 * Three things here are not decoration:
 *
 *   - **The activity and bell flags.** tmux has tracked them since forever and
 *     nothing in this class surfaces them. They answer the only question a tab
 *     strip cannot otherwise answer — which of these five did something while I
 *     was looking at a different one — and a build that finished in a window
 *     you are not watching is exactly the case.
 *   - **The colour**, inherited from the session unless the window overrides,
 *     for the same reason the rail has colours: five tabs called `bash` are
 *     five tabs called `bash`.
 *   - **Dragging.** Along the strip to reorder, or out onto a session in the
 *     rail to move the window there — which is how work opened in the wrong
 *     place gets put right without losing what is scrolled back in it.
 */
export function WindowStrip({
  windows,
  sessionColour,
  onSelect,
  onRename,
  onColour,
  onNew,
  onClose,
  onReorder,
  onSplit,
  onLayout,
  onSynchronize,
  sessionName,
}: {
  windows: TerminalWindow[]
  sessionColour?: string
  sessionName: string
  onSelect: (index: number) => void
  onRename: (index: number, name: string) => void
  onColour: (index: number, colour: string) => void
  onNew: () => void
  onClose: (index: number) => void
  onReorder: (index: number, position: number) => void
  onSplit: (index: number, vertical: boolean) => void
  onLayout: (index: number, layout: string) => void
  onSynchronize: (index: number, on: boolean) => void
}) {
  const [renaming, setRenaming] = useState<number | null>(null)
  const [dropAt, setDropAt] = useState<number | null>(null)
  const active = windows.find((w) => w.active)

  if (windows.length === 0) return null

  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1">
      {windows.map((win, position) =>
        renaming === win.index ? (
          <WindowNameInput
            key={win.index}
            value={win.name}
            onCommit={(value) => {
              if (value) onRename(win.index, value)
              setRenaming(null)
            }}
            onCancel={() => setRenaming(null)}
          />
        ) : (
          <WindowChip
            key={win.index}
            win={win}
            position={position}
            sessionName={sessionName}
            colour={win.colour || sessionColour}
            inserting={dropAt === position}
            closable={windows.length > 1}
            onSelect={() => onSelect(win.index)}
            onStartRename={() => setRenaming(win.index)}
            onColour={(colour) => onColour(win.index, colour)}
            onClose={() => onClose(win.index)}
            onSplit={(vertical) => onSplit(win.index, vertical)}
            onLayout={(layout) => onLayout(win.index, layout)}
            onSynchronize={(on) => onSynchronize(win.index, on)}
            onDragOverChip={() => setDropAt(position)}
            onDropChip={(event) => {
              setDropAt(null)
              const payload = readDrop(event, "window")
              endDrag()
              if (payload?.kind !== "window" || payload.session !== sessionName) return
              if (payload.position !== position) onReorder(payload.index, position)
            }}
            onDragEndChip={() => setDropAt(null)}
          />
        ),
      )}

      <IconAction label="New window" className="size-6" onClick={onNew}>
        <Plus />
      </IconAction>

      {active && (
        <>
          <span className="mx-0.5 h-4 w-px bg-hairline" />
          <IconAction
            label="Split into two panes, side by side"
            className="size-6"
            onClick={() => onSplit(active.index, true)}
          >
            <Columns2 />
          </IconAction>
          <IconAction
            label="Split into two panes, one above the other"
            className="size-6"
            onClick={() => onSplit(active.index, false)}
          >
            <Rows2 />
          </IconAction>
        </>
      )}

      {/*
        Synchronised input is drawn as a standing warning rather than as a
        checkbox somewhere: it is the one setting that turns a typo into the
        same typo on four servers, and the operator has to be able to see that
        it is on without going to look.
      */}
      {active?.synchronized && (
        <button
          className="flex items-center gap-1 rounded-md border border-warning/50 bg-warning/12 px-1.5 py-0.5 text-[10px] text-warning"
          onClick={() => onSynchronize(active.index, false)}
        >
          <AlertTriangle className="size-3" />
          Typing goes to every pane
          <Unlink className="size-3" />
        </button>
      )}
    </div>
  )
}

function WindowChip({
  win,
  position,
  sessionName,
  colour,
  inserting,
  closable,
  onSelect,
  onStartRename,
  onColour,
  onClose,
  onSplit,
  onLayout,
  onSynchronize,
  onDragOverChip,
  onDropChip,
  onDragEndChip,
}: {
  win: TerminalWindow
  position: number
  sessionName: string
  colour?: string
  inserting: boolean
  closable: boolean
  onSelect: () => void
  onStartRename: () => void
  onColour: (colour: string) => void
  onClose: () => void
  onSplit: (vertical: boolean) => void
  onLayout: (layout: string) => void
  onSynchronize: (on: boolean) => void
  onDragOverChip: () => void
  onDropChip: (event: React.DragEvent) => void
  onDragEndChip: () => void
}) {
  const tag = tagVar(colour)
  return (
    <div
      draggable
      data-window={win.index}
      onDragStart={(event) =>
        beginDrag(
          event,
          {
            kind: "window",
            session: sessionName,
            index: win.index,
            position,
            name: win.name,
          },
          tag,
        )
      }
      onDragEnd={() => {
        endDrag()
        onDragEndChip()
      }}
      onDragOver={(event) => {
        if (!carries(event, "window")) return
        event.preventDefault()
        event.dataTransfer.dropEffect = "move"
        onDragOverChip()
      }}
      onDrop={(event) => {
        if (!carries(event, "window")) return
        event.preventDefault()
        onDropChip(event)
      }}
      style={tagStyle(colour)}
      className={cn(
        "group flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] transition-colors",
        win.active
          ? "border-primary/40 bg-primary/10 text-foreground"
          : "border-hairline text-muted-foreground hover:bg-accent",
        inserting && "border-l-2 border-l-primary",
      )}
    >
      <button className="flex items-center gap-1" onClick={onSelect} onDoubleClick={onStartRename}>
        {tag ? (
          <TagSwatch colour={colour} className="size-1.5" />
        ) : (
          <SquareTerminal className="size-3" />
        )}
        <span className="max-w-32 truncate">{win.name}</span>
        {win.panes > 1 && <span className="numeric text-[9px] opacity-70">{win.panes}p</span>}
        {win.zoomed && <Maximize2 className="size-2.5 text-primary" />}
        {win.synchronized && <Link2 className="size-2.5 text-warning" />}
        {/*
          A bell is louder than activity because it is the shell asking for
          attention rather than merely producing output. Neither is shown on
          the window you are looking at: tmux clears the flag on select, and a
          dot that is always lit on the active tab teaches you to ignore it.
        */}
        {!win.active && win.bell && <Bell className="size-2.5 text-warning" />}
        {!win.active && !win.bell && win.activity && (
          <span className="size-1.5 rounded-full bg-primary/70" />
        )}
      </button>

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label={`More for window ${win.name}`}
            className="size-4 opacity-0 transition-opacity group-hover:opacity-100 [&_svg:not([class*='size-'])]:size-3"
          >
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-52">
          <DropdownMenuItem className="gap-2 text-xs" onSelect={onStartRename}>
            <Pencil className="size-3.5" />
            Rename window
          </DropdownMenuItem>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger className="gap-2 text-xs">
              <Palette className="size-3.5" />
              Colour
              <span className="flex-1" />
              <TagSwatch colour={colour} />
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-44">
              <ColourMenuItems colour={win.colour} inherited="session" onPick={onColour} />
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuSeparator />
          <DropdownMenuItem className="gap-2 text-xs" onSelect={() => onSplit(true)}>
            <Columns2 className="size-3.5" />
            Split side by side
          </DropdownMenuItem>
          <DropdownMenuItem className="gap-2 text-xs" onSelect={() => onSplit(false)}>
            <Rows2 className="size-3.5" />
            Split top and bottom
          </DropdownMenuItem>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger className="gap-2 text-xs">
              <LayoutGrid className="size-3.5" />
              Arrange panes
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-44">
              {LAYOUTS.map((layout) => (
                <DropdownMenuItem
                  key={layout.id}
                  className="text-xs"
                  onSelect={() => onLayout(layout.id)}
                >
                  {layout.label}
                </DropdownMenuItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuItem
            className="gap-2 text-xs"
            onSelect={() => onSynchronize(!win.synchronized)}
          >
            {win.synchronized ? (
              <Link2Off className="size-3.5" />
            ) : (
              <Link2 className="size-3.5" />
            )}
            {win.synchronized ? "Stop synchronised typing" : "Type into every pane at once"}
          </DropdownMenuItem>
          {closable && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" className="gap-2 text-xs" onSelect={onClose}>
                <X className="size-3.5" />
                Close window
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      {closable && (
        <button
          aria-label={`Close window ${win.name}`}
          className="opacity-0 transition-opacity group-hover:opacity-100 hover:text-destructive"
          onClick={onClose}
        >
          <X className="size-3" />
        </button>
      )}
    </div>
  )
}

/** tmux's named arrangements, in the order they are worth reaching for. */
const LAYOUTS = [
  { id: "even-horizontal", label: "Even columns" },
  { id: "even-vertical", label: "Even rows" },
  { id: "main-vertical", label: "Main + side column" },
  { id: "main-horizontal", label: "Main + bottom row" },
  { id: "tiled", label: "Tiled" },
] as const

/**
 * The panes of the active window.
 *
 * Shown only once there is more than one, because a single pane is the
 * ordinary case and a bar saying "1 pane" is noise. What it earns its place
 * with is the command in each: "pane 2" tells you nothing, `pg_dump` tells you
 * which half of the screen not to close.
 */
export function PaneBar({
  panes,
  onSelect,
  onZoom,
  onClose,
}: {
  panes: TerminalPane[]
  onSelect: (index: number) => void
  onZoom: (index: number) => void
  onClose: (index: number) => void
}) {
  if (panes.length < 2) return null
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1 rounded-md border border-hairline bg-surface-header px-1.5 py-1">
      <span className="eyebrow pr-1 text-[9px]">Panes</span>
      {panes.map((pane) => (
        <div
          key={pane.index}
          // Named in the DOM for the same reason a session row and a window
          // chip are: which pane has the focus is state the page acts on, and
          // reading it back out of a class name is guesswork.
          data-pane={pane.index}
          data-active={pane.active}
          className={cn(
            "group flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] transition-colors",
            pane.active ? "bg-primary/12 text-foreground" : "text-muted-foreground hover:bg-accent",
          )}
        >
          <button className="flex items-center gap-1" onClick={() => onSelect(pane.index)}>
            <span className="numeric opacity-60">{pane.index}</span>
            <span className="max-w-28 truncate font-mono">
              {pane.dead ? "exited" : (pane.command ?? "shell")}
            </span>
            <span className="numeric text-[9px] opacity-50">
              {pane.width}×{pane.height}
            </span>
          </button>
          <IconAction
            label="Zoom this pane to fill the window"
            className="size-4 opacity-0 transition-opacity group-hover:opacity-100 [&_svg:not([class*='size-'])]:size-3"
            onClick={() => onZoom(pane.index)}
          >
            <Maximize2 />
          </IconAction>
          <IconAction
            label="Close this pane"
            className="size-4 text-destructive opacity-0 transition-opacity group-hover:opacity-100 [&_svg:not([class*='size-'])]:size-3"
            onClick={() => onClose(pane.index)}
          >
            <X />
          </IconAction>
        </div>
      ))}
    </div>
  )
}

function WindowNameInput({
  value,
  onCommit,
  onCancel,
}: {
  value: string
  onCommit: (value: string) => void
  onCancel: () => void
}) {
  const [draft, setDraft] = useState(value)
  const cancelled = useRef(false)
  return (
    <Input
      autoFocus
      value={draft}
      spellCheck={false}
      className="h-6 w-32 text-xs"
      onChange={(e) => setDraft(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter") onCommit(draft.trim())
        if (e.key === "Escape") {
          cancelled.current = true
          onCancel()
        }
      }}
      onBlur={() => {
        if (!cancelled.current) onCommit(draft.trim())
      }}
    />
  )
}
