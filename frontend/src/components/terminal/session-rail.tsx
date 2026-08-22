"use client"

import { useMemo, useRef, useState } from "react"
import {
  ChevronDown,
  Folder,
  FolderOpen,
  FolderPlus,
  GripVertical,
  MoreHorizontal,
  Palette,
  Pencil,
  Pin,
  PinOff,
  Plus,
  Search,
  Terminal,
  Trash2,
  X,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { relativeTime, truncateMiddle } from "@/lib/format"
import type { TerminalFolder, TerminalWorkspace } from "@/lib/types"
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
import { beginDrag, carries, endDrag, readDrop, useDragPayload } from "@/components/terminal/dnd"

/**
 * The list of terminals, as a workspace tree.
 *
 * A chip strip works for three sessions and falls apart at ten: the names
 * truncate, the order is arbitrary, and there is nowhere to say which are
 * running and which are merely alive. Every tool in this class stops before
 * this point — Cockpit has one terminal, ttyd and Wetty have no session
 * concept, Portainer's console dies with the tab — so the references worth
 * copying are outside it: VS Code's explorer, where a folder is visibly a
 * different kind of thing from a file, and Guacamole's connection groups,
 * where a long list is made short by folding it.
 *
 * The first version of this had folders and sessions drawn as the same row at
 * the same weight, which meant the hierarchy existed in the data and nowhere
 * on screen. Four things carry it now, and they are worth keeping:
 *
 *   - **A folder is chrome, a session is content.** The folder header has the
 *     panel-header tint, an icon in a tinted tile and an uppercase label; a
 *     session is a plain row on the card. That is the same "chrome, then
 *     content" contrast every Panel in this app is built on, applied one level
 *     down.
 *   - **Children are indented behind a rule** in the folder's own colour, so a
 *     group reads as a group at a glance rather than by counting.
 *   - **Colour is inherited.** Paint the folder and everything in it is
 *     painted, because colouring eight sessions by hand is work nobody does
 *     twice.
 *   - **Everything is draggable** — a session into a folder, a folder up the
 *     list, a window onto another session. Filing by dragging is the only kind
 *     of filing people actually do.
 *
 * Pinning sorts a session to the top *of its folder* rather than lifting it
 * into a separate group. The separate group was the earlier design and it
 * quietly broke the hierarchy: a starred session vanished from the folder it
 * was in, which is exactly the thing the operator had filed it there to avoid.
 */
export function SessionRail({
  sessions,
  folders,
  activeId,
  onSelect,
  onRename,
  onTogglePinned,
  onSetFolder,
  onSetColour,
  onClose,
  onNew,
  onCreateFolder,
  onUpdateFolder,
  onDeleteFolder,
  onReorderFolders,
  onMoveWindow,
}: {
  sessions: TerminalWorkspace[]
  folders: TerminalFolder[]
  activeId: string | null
  /** Attach to a live session, or reattach one that is merely running. */
  onSelect: (session: TerminalWorkspace) => void
  onRename: (session: TerminalWorkspace, title: string) => void
  onTogglePinned: (session: TerminalWorkspace) => void
  /** By tmux name: a drop knows the name and nothing else about the row. */
  onSetFolder: (tmuxName: string, folder: string) => void
  onSetColour: (session: TerminalWorkspace, colour: string) => void
  onClose: (session: TerminalWorkspace) => void
  onNew: (folder?: string) => void
  onCreateFolder: (name: string) => void
  onUpdateFolder: (name: string, next: { name?: string; colour?: string }) => void
  onDeleteFolder: (folder: TerminalFolder) => void
  onReorderFolders: (names: string[]) => void
  /** A window dragged out of its session and dropped onto another one. */
  onMoveWindow: (from: string, index: number, to: string) => void
}) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [creatingFolder, setCreatingFolder] = useState(false)
  const [filter, setFilter] = useState("")
  const drag = useDragPayload()

  const matches = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    if (!needle) return sessions
    return sessions.filter(
      (s) =>
        s.title.toLowerCase().includes(needle) ||
        (s.cwd ?? "").toLowerCase().includes(needle) ||
        (s.folder ?? "").toLowerCase().includes(needle),
    )
  }, [sessions, filter])

  // Folders in their stored order, then whatever is unfiled. Pinned sessions
  // sort first inside each group and the newest is next, because the session
  // you just opened is the one you are looking for.
  const groups = useMemo(() => {
    const byFolder = new Map<string, TerminalWorkspace[]>()
    for (const session of matches) {
      const key = session.folder || ""
      byFolder.set(key, [...(byFolder.get(key) ?? []), session])
    }
    const order = (items: TerminalWorkspace[]) =>
      [...items].sort((a, b) => {
        if (a.favourite !== b.favourite) return a.favourite ? -1 : 1
        return b.createdAt.localeCompare(a.createdAt)
      })
    return {
      folders: folders.map((folder) => ({
        folder,
        items: order(byFolder.get(folder.name) ?? []),
      })),
      unfiled: order(byFolder.get("") ?? []),
    }
  }, [matches, folders])

  const draggingFolder = drag?.kind === "folder" ? drag.name : null

  /** Dropping a folder onto another one puts it there and shifts the rest. */
  const dropFolder = (event: React.DragEvent, before: string) => {
    const payload = readDrop(event, "folder")
    endDrag()
    if (!payload || payload.kind !== "folder" || payload.name === before) return
    const names = folders.map((f) => f.name).filter((n) => n !== payload.name)
    const at = names.indexOf(before)
    names.splice(at < 0 ? names.length : at, 0, payload.name)
    onReorderFolders(names)
  }

  return (
    <div className="flex min-h-0 w-full shrink-0 flex-col gap-2 lg:w-72">
      <div className="flex items-center gap-1">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={filter}
            spellCheck={false}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter sessions"
            className="h-7 pl-7 text-xs"
          />
        </div>
        <IconAction label="New folder" className="size-7" onClick={() => setCreatingFolder(true)}>
          <FolderPlus />
        </IconAction>
      </div>

      <div className="min-h-0 flex-1 space-y-1.5 overflow-y-auto pr-0.5">
        {creatingFolder && (
          <InlineEdit
            placeholder="Folder name"
            value=""
            onCommit={(value) => {
              if (value) onCreateFolder(value)
              setCreatingFolder(false)
            }}
            onCancel={() => setCreatingFolder(false)}
          />
        )}

        {groups.folders.map(({ folder, items }) => (
          <FolderGroup
            key={folder.name}
            folder={folder}
            items={items}
            activeId={activeId}
            collapsed={Boolean(collapsed[folder.name])}
            dimmed={Boolean(draggingFolder) && draggingFolder !== folder.name}
            onToggle={() =>
              setCollapsed((c) => ({ ...c, [folder.name]: !c[folder.name] }))
            }
            onDropFolder={dropFolder}
            onSelect={onSelect}
            onRename={onRename}
            onTogglePinned={onTogglePinned}
            onSetFolder={onSetFolder}
            onSetColour={onSetColour}
            onClose={onClose}
            onNew={onNew}
            onUpdateFolder={onUpdateFolder}
            onDeleteFolder={onDeleteFolder}
            onMoveWindow={onMoveWindow}
            folders={folders}
          />
        ))}

        <UnfiledGroup
          items={groups.unfiled}
          hasFolders={folders.length > 0}
          activeId={activeId}
          onSelect={onSelect}
          onRename={onRename}
          onTogglePinned={onTogglePinned}
          onSetFolder={onSetFolder}
          onSetColour={onSetColour}
          onClose={onClose}
          onNew={onNew}
          onMoveWindow={onMoveWindow}
          folders={folders}
        />

        {matches.length === 0 && filter && (
          <p className="px-2 py-4 text-center text-xs text-muted-foreground">
            No session matches <span className="font-medium text-foreground">{filter}</span>.
          </p>
        )}
      </div>
    </div>
  )
}

type RowHandlers = {
  activeId: string | null
  folders: TerminalFolder[]
  onSelect: (session: TerminalWorkspace) => void
  onRename: (session: TerminalWorkspace, title: string) => void
  onTogglePinned: (session: TerminalWorkspace) => void
  /** By tmux name: a drop knows the name and nothing else about the row. */
  onSetFolder: (tmuxName: string, folder: string) => void
  onSetColour: (session: TerminalWorkspace, colour: string) => void
  onClose: (session: TerminalWorkspace) => void
  onNew: (folder?: string) => void
  onMoveWindow: (from: string, index: number, to: string) => void
}

function FolderGroup({
  folder,
  items,
  collapsed,
  dimmed,
  onToggle,
  onDropFolder,
  onUpdateFolder,
  onDeleteFolder,
  ...rows
}: RowHandlers & {
  folder: TerminalFolder
  items: TerminalWorkspace[]
  collapsed: boolean
  dimmed: boolean
  onToggle: () => void
  onDropFolder: (event: React.DragEvent, before: string) => void
  onUpdateFolder: (name: string, next: { name?: string; colour?: string }) => void
  onDeleteFolder: (folder: TerminalFolder) => void
}) {
  const [renaming, setRenaming] = useState(false)
  const [over, setOver] = useState<null | "session" | "folder">(null)
  // A drag crossing a child fires dragleave on the parent, so a single boolean
  // flickers the highlight off and on the whole way across the folder. Counting
  // enter and leave is the standard answer and the only one that is stable.
  const depth = useRef(0)

  const accepts = (event: React.DragEvent): null | "session" | "folder" => {
    if (carries(event, "session")) return "session"
    if (carries(event, "folder")) return "folder"
    return null
  }

  const onDrop = (event: React.DragEvent) => {
    const kind = accepts(event)
    event.preventDefault()
    depth.current = 0
    setOver(null)
    if (kind === "folder") {
      onDropFolder(event, folder.name)
      return
    }
    const payload = readDrop(event, "session")
    endDrag()
    if (payload?.kind === "session" && payload.folder !== folder.name) {
      rows.onSetFolder(payload.tmuxName, folder.name)
    }
  }

  if (renaming) {
    return (
      <InlineEdit
        placeholder="Folder name"
        value={folder.name}
        onCommit={(value) => {
          if (value && value !== folder.name) onUpdateFolder(folder.name, { name: value })
          setRenaming(false)
        }}
        onCancel={() => setRenaming(false)}
      />
    )
  }

  return (
    <div
      // Named in the DOM so the group a row belongs to is readable from
      // outside it — which is what a drop target has to know, and what an
      // end-to-end test asserting "this session is in that folder" needs in
      // place of guessing from indentation.
      data-folder={folder.name}
      className={cn("min-w-0 transition-opacity", dimmed && "opacity-45")}
      style={tagStyle(folder.colour)}
      onDragEnter={(event) => {
        if (!accepts(event)) return
        depth.current += 1
        setOver(accepts(event))
      }}
      onDragLeave={() => {
        depth.current -= 1
        if (depth.current <= 0) {
          depth.current = 0
          setOver(null)
        }
      }}
      onDragOver={(event) => {
        if (!accepts(event)) return
        event.preventDefault()
        event.dataTransfer.dropEffect = "move"
      }}
      onDrop={onDrop}
    >
      {/*
        The header is drawn as chrome — the same tint and hairline a Panel
        header gets — because that is what makes a folder read as a container
        rather than as a slightly bolder session.
      */}
      <div
        draggable
        onDragStart={(event) => beginDrag(event, { kind: "folder", name: folder.name })}
        onDragEnd={endDrag}
        className={cn(
          "group/folder flex items-center gap-1 rounded-lg border border-hairline bg-surface-header px-1.5 py-1 transition-colors",
          over === "session" && "border-primary/50 bg-primary/10",
          over === "folder" && "border-t-2 border-t-primary",
        )}
      >
        <GripVertical className="size-3 shrink-0 cursor-grab text-muted-foreground/50 opacity-0 transition-opacity group-hover/folder:opacity-100" />
        <button
          className="flex min-w-0 flex-1 items-center gap-1.5 py-0.5 text-left"
          onClick={onToggle}
        >
          <ChevronDown
            className={cn(
              "size-3 shrink-0 text-muted-foreground transition-transform",
              collapsed && "-rotate-90",
            )}
          />
          <span
            className="flex size-5 shrink-0 items-center justify-center rounded-[5px]"
            style={{
              backgroundColor: tagVar(folder.colour)
                ? "color-mix(in oklab, var(--tag) 22%, transparent)"
                : "var(--row-hover)",
              color: tagVar(folder.colour) ?? "var(--color-muted-foreground)",
            }}
          >
            {collapsed ? <Folder className="size-3" /> : <FolderOpen className="size-3" />}
          </span>
          <span className="eyebrow truncate">{folder.name}</span>
          <span className="numeric rounded-full bg-muted px-1.5 text-[10px] leading-4 text-muted-foreground">
            {items.length}
          </span>
        </button>

        <span className="flex shrink-0 items-center opacity-0 transition-opacity group-hover/folder:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
          <IconAction
            label={`New session in ${folder.name}`}
            className="size-6"
            onClick={() => rows.onNew(folder.name)}
          >
            <Plus />
          </IconAction>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={`More for ${folder.name}`}
                className="size-6 [&_svg:not([class*='size-'])]:size-3.5"
              >
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-44">
              <DropdownMenuItem className="gap-2 text-xs" onSelect={() => setRenaming(true)}>
                <Pencil className="size-3.5" />
                Rename folder
              </DropdownMenuItem>
              <ColourSubmenu
                colour={folder.colour}
                onPick={(colour) => onUpdateFolder(folder.name, { colour })}
              />
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                className="gap-2 text-xs"
                onSelect={() => onDeleteFolder(folder)}
              >
                <Trash2 className="size-3.5" />
                Delete folder
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </span>
      </div>

      {!collapsed && (
        // The rule down the left is the folder's colour at low strength: it is
        // what makes four rows read as one group without repeating the colour
        // on every one of them.
        <div
          className="mt-1 ml-2.5 space-y-1 border-l pl-2"
          style={{
            borderColor: tagVar(folder.colour)
              ? "color-mix(in oklab, var(--tag) 45%, transparent)"
              : "var(--hairline)",
          }}
        >
          {items.map((session) => (
            <SessionRow
              key={session.tmuxName || session.id}
              session={session}
              inheritedColour={folder.colour}
              {...rows}
            />
          ))}
          {items.length === 0 && (
            <button
              className="w-full rounded-lg border border-dashed border-hairline px-2 py-1.5 text-left text-[11px] text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground"
              onClick={() => rows.onNew(folder.name)}
            >
              Empty — open a session here
            </button>
          )}
        </div>
      )}
    </div>
  )
}

/**
 * Everything not in a folder. It is a drop target in its own right, because
 * dragging a session *out* of a folder needs somewhere to go — without this
 * the only way back out is the menu, and a filing system you can only put
 * things into is a trap.
 */
function UnfiledGroup({
  items,
  hasFolders,
  ...rows
}: RowHandlers & { items: TerminalWorkspace[]; hasFolders: boolean }) {
  const [over, setOver] = useState(false)
  const depth = useRef(0)

  if (items.length === 0 && !hasFolders) return null

  return (
    <div
      className="min-w-0"
      data-folder=""
      onDragEnter={(event) => {
        if (!carries(event, "session")) return
        depth.current += 1
        setOver(true)
      }}
      onDragLeave={() => {
        depth.current -= 1
        if (depth.current <= 0) {
          depth.current = 0
          setOver(false)
        }
      }}
      onDragOver={(event) => {
        if (!carries(event, "session")) return
        event.preventDefault()
        event.dataTransfer.dropEffect = "move"
      }}
      onDrop={(event) => {
        event.preventDefault()
        depth.current = 0
        setOver(false)
        const payload = readDrop(event, "session")
        endDrag()
        if (payload?.kind === "session" && payload.folder) {
          rows.onSetFolder(payload.tmuxName, "")
        }
      }}
    >
      {hasFolders && (
        <div
          className={cn(
            "mt-1 flex items-center gap-1.5 rounded-lg border border-transparent px-1.5 py-1 transition-colors",
            over && "border-dashed border-primary/50 bg-primary/5",
          )}
        >
          <span className="eyebrow truncate text-muted-foreground/70">Unfiled</span>
          <span className="numeric rounded-full bg-muted px-1.5 text-[10px] leading-4 text-muted-foreground">
            {items.length}
          </span>
          <span className="flex-1" />
          <IconAction
            label="New unfiled session"
            className="size-6"
            onClick={() => rows.onNew()}
          >
            <Plus />
          </IconAction>
        </div>
      )}
      <div className={cn("space-y-1", hasFolders && "mt-1")}>
        {items.map((session) => (
          <SessionRow key={session.tmuxName || session.id} session={session} {...rows} />
        ))}
      </div>
    </div>
  )
}

function SessionRow({
  session,
  inheritedColour,
  activeId,
  folders,
  onSelect,
  onRename,
  onTogglePinned,
  onSetFolder,
  onSetColour,
  onClose,
  onMoveWindow,
}: RowHandlers & { session: TerminalWorkspace; inheritedColour?: string }) {
  const [renaming, setRenaming] = useState(false)
  const [windowOver, setWindowOver] = useState(false)
  const active = activeId !== null && session.id === activeId
  const colour = session.colour || inheritedColour

  if (renaming) {
    return (
      <InlineEdit
        placeholder="Name this session"
        value={session.title}
        onCommit={(value) => {
          if (value) onRename(session, value)
          setRenaming(false)
        }}
        onCancel={() => setRenaming(false)}
      />
    )
  }

  return (
    <div
      draggable
      data-session={session.tmuxName ?? session.id}
      onDragStart={(event) =>
        beginDrag(event, {
          kind: "session",
          tmuxName: session.tmuxName ?? "",
          folder: session.folder ?? "",
          title: session.title,
        })
      }
      onDragEnd={endDrag}
      // A window dropped onto a session moves it there, which is how work
      // opened in the wrong place gets put right without losing its scrollback.
      onDragOver={(event) => {
        if (!carries(event, "window")) return
        event.preventDefault()
        event.dataTransfer.dropEffect = "move"
        setWindowOver(true)
      }}
      onDragLeave={() => setWindowOver(false)}
      onDrop={(event) => {
        if (!carries(event, "window")) return
        event.preventDefault()
        setWindowOver(false)
        const payload = readDrop(event, "window")
        endDrag()
        if (payload?.kind === "window" && session.tmuxName && payload.session !== session.tmuxName) {
          onMoveWindow(payload.session, payload.index, session.tmuxName)
        }
      }}
      className={cn(
        "group relative flex min-w-0 items-center gap-1.5 rounded-lg border py-1.5 pr-1 pl-2 transition-colors",
        active
          ? "border-primary/40 bg-primary/10"
          : "border-transparent hover:border-hairline hover:bg-[var(--row-hover)]",
        windowOver && "border-dashed border-primary bg-primary/10",
      )}
      style={tagStyle(colour)}
    >
      {/* The tag as a rule down the row's own edge, so a coloured session is
          identifiable inside an uncoloured folder and vice versa. */}
      {colour && (
        <span
          className="absolute inset-y-1 left-0 w-0.5 rounded-full"
          style={{ backgroundColor: "var(--tag)" }}
        />
      )}
      <button
        onClick={() => onSelect(session)}
        className="flex min-w-0 flex-1 items-center gap-2 text-left"
      >
        {/*
          The one signal that matters at a glance: a filled dot is a session
          this dashboard is holding open, a hollow one is running on the host
          with nothing attached. Both are alive — the difference is only
          whether clicking costs a reattach.
        */}
        <span
          className={cn(
            "size-2 shrink-0 rounded-full",
            session.live ? "bg-success" : "ring-1 ring-muted-foreground/60 ring-inset",
          )}
        />
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1">
            {session.favourite && <Pin className="size-2.5 shrink-0 text-warning" />}
            <span className="truncate text-[13px] leading-tight">{session.title}</span>
          </span>
          <span className="flex items-center gap-1 truncate font-mono text-[10px] leading-tight text-muted-foreground">
            {session.cwd ? truncateMiddle(session.cwd, 26) : relativeTime(session.createdAt)}
            {session.windows > 1 && (
              <span className="inline-flex items-center gap-0.5">
                · <Terminal className="size-2.5" />
                {session.windows}
              </span>
            )}
          </span>
        </span>
      </button>

      <span className="flex shrink-0 items-center opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
        <IconAction
          label={session.favourite ? "Unpin" : "Pin to the top of its folder"}
          className="size-6"
          onClick={() => onTogglePinned(session)}
        >
          {session.favourite ? <PinOff /> : <Pin />}
        </IconAction>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label={`More for ${session.title}`}
              className="size-6 [&_svg:not([class*='size-'])]:size-3.5"
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuItem className="gap-2 text-xs" onSelect={() => setRenaming(true)}>
              <Pencil className="size-3.5" />
              Rename
            </DropdownMenuItem>
            <ColourSubmenu
              colour={session.colour}
              inherited={inheritedColour ? "folder" : undefined}
              onPick={(next) => onSetColour(session, next)}
            />
            <DropdownMenuSub>
              <DropdownMenuSubTrigger className="gap-2 text-xs">
                <Folder className="size-3.5" />
                Move to
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-44">
                <DropdownMenuItem
                  className="gap-2 text-xs"
                  onSelect={() => onSetFolder(session.tmuxName ?? "", "")}
                >
                  Unfiled
                </DropdownMenuItem>
                {folders.map((f) => (
                  <DropdownMenuItem
                    key={f.name}
                    className="gap-2 text-xs"
                    disabled={f.name === session.folder}
                    onSelect={() => onSetFolder(session.tmuxName ?? "", f.name)}
                  >
                    <TagSwatch colour={f.colour} />
                    <span className="truncate">{f.name}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant="destructive"
              className="gap-2 text-xs"
              onSelect={() => onClose(session)}
            >
              <X className="size-3.5" />
              Close session
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </span>
    </div>
  )
}

/** The colour picker as a submenu, so a row needs one menu rather than two. */
function ColourSubmenu({
  colour,
  inherited,
  onPick,
}: {
  colour?: string
  inherited?: string
  onPick: (colour: string) => void
}) {
  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger className="gap-2 text-xs">
        <Palette className="size-3.5" />
        Colour
        <span className="flex-1" />
        <TagSwatch colour={colour} />
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="w-44">
        <ColourMenuItems colour={colour} inherited={inherited} onPick={onPick} />
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  )
}

/**
 * The one editable row. A rename that costs a dialog is a rename nobody does,
 * and then every session is `vpsd-3f2a91c4` again.
 */
function InlineEdit({
  value,
  placeholder,
  onCommit,
  onCancel,
}: {
  value: string
  placeholder: string
  onCommit: (value: string) => void
  onCancel: () => void
}) {
  const [draft, setDraft] = useState(value)
  // Blur commits, and Escape has to be able to beat it: the key handler runs
  // first and this flag is what stops the blur it causes from writing the
  // draft the operator just abandoned.
  const cancelled = useRef(false)

  return (
    <div className="flex items-center gap-1 rounded-lg border border-primary/50 bg-card px-1.5 py-1">
      <Input
        autoFocus
        value={draft}
        spellCheck={false}
        placeholder={placeholder}
        className="h-6 border-0 bg-transparent px-1 text-xs shadow-none focus-visible:ring-0"
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") onCommit(draft.trim())
          if (e.key === "Escape") {
            cancelled.current = true
            onCancel()
          }
        }}
        onBlur={() => {
          if (cancelled.current) return
          onCommit(draft.trim())
        }}
      />
    </div>
  )
}
