"use client"

import { useMemo, useRef, useState } from "react"
import {
  BlendMode,
  ChevronDown,
  Cross,
  FolderClosed,
  FolderOpen,
  FolderPlus,
  MagnifyingGlass,
  MoreHorizontal,
  Pencil,
  Pin,
  Plus,
  Terminal,
  Trash,
} from "@/components/icons"
import { cn } from "@/lib/utils"
import { relativeTime, truncateMiddle } from "@/lib/format"
import type { TerminalFolder, TerminalWorkspace } from "@/lib/types"
import { useViewState } from "@/lib/view-state"
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
import { carries, endDrag, readDrop } from "@/components/terminal/dnd"

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
 * on screen. Three things carry it now, and they are worth keeping:
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
 *
 * Filing is through each row's menu — "Move to" for a session, the colour
 * submenu for either. Pinning sorts a session to the top *of its folder*
 * rather than lifting it into a separate group; the separate group was the
 * earlier design and it quietly broke the hierarchy: a starred session
 * vanished from the folder it was in, which is exactly the thing the operator
 * had filed it there to avoid.
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
  onMoveWindow,
  className,
}: {
  sessions: TerminalWorkspace[]
  folders: TerminalFolder[]
  activeId: string | null
  /** Attach to a live session, or reattach one that is merely running. */
  onSelect: (session: TerminalWorkspace) => void
  onRename: (session: TerminalWorkspace, title: string) => void
  onTogglePinned: (session: TerminalWorkspace) => void
  /** By tmux name: the menu knows the name and nothing else about the row. */
  onSetFolder: (tmuxName: string, folder: string) => void
  onSetColour: (session: TerminalWorkspace, colour: string) => void
  onClose: (session: TerminalWorkspace) => void
  onNew: (folder?: string) => void
  onCreateFolder: (name: string) => void
  onUpdateFolder: (name: string, next: { name?: string; colour?: string }) => void
  onDeleteFolder: (folder: TerminalFolder) => void
  /** A window dragged out of its session and dropped onto another one. */
  onMoveWindow: (from: string, index: number, to: string) => void
  /** The page owns the rail's width — it is the operator's to drag. */
  className?: string
}) {
  // Which folders are folded away. Remembered, because a rail with eight
  // folders is organised precisely so that seven of them can be shut, and
  // reopening them on every visit undoes the organising.
  const [collapsed, setCollapsed] = useViewState<Record<string, boolean>>(
    "terminal.folders.collapsed",
    {},
  )
  const [creatingFolder, setCreatingFolder] = useState(false)
  const [filter, setFilter] = useState("")

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

  return (
    <div className={cn("flex min-h-0 w-full shrink-0 flex-col gap-2 lg:w-72", className)}>
      <div className="flex items-center gap-1">
        <div className="relative min-w-0 flex-1">
          <MagnifyingGlass className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground" />
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
        <IconAction label="New session" className="size-7" onClick={() => onNew()}>
          <Plus />
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
            onToggle={() =>
              setCollapsed((c) => ({ ...c, [folder.name]: !c[folder.name] }))
            }
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
  /** By tmux name: the menu knows the name and nothing else about the row. */
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
  onToggle,
  onUpdateFolder,
  onDeleteFolder,
  ...rows
}: RowHandlers & {
  folder: TerminalFolder
  items: TerminalWorkspace[]
  collapsed: boolean
  onToggle: () => void
  onUpdateFolder: (name: string, next: { name?: string; colour?: string }) => void
  onDeleteFolder: (folder: TerminalFolder) => void
}) {
  const [renaming, setRenaming] = useState(false)

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
      // outside it — which is what an end-to-end test asserting "this session
      // is in that folder" needs in place of guessing from indentation.
      data-folder={folder.name}
      className="min-w-0"
      style={tagStyle(folder.colour)}
    >
      {/*
        The header is drawn as chrome — the same tint and hairline a Panel
        header gets — because that is what makes a folder read as a container
        rather than as a slightly bolder session.
      */}
      <div
        className={cn(
          "group/folder flex items-center gap-1 rounded-lg border px-1.5 py-1 transition-colors",
          !tagVar(folder.colour) && "border-hairline bg-surface-header",
        )}
        // The whole header takes the colour, not just the icon. A folder
        // painted only on a 20px tile is a folder nobody can pick out of a
        // list of six at a glance, which is the entire job the colour has.
        // Mixed against the card rather than set flat, so one hue works on a
        // near-black surface and a near-white one.
        style={
          tagVar(folder.colour)
            ? {
                backgroundColor: "color-mix(in oklab, var(--tag) 14%, var(--card))",
                borderColor: "color-mix(in oklab, var(--tag) 40%, transparent)",
              }
            : undefined
        }
      >
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
                ? "color-mix(in oklab, var(--tag) 30%, transparent)"
                : "var(--row-hover)",
              color: tagVar(folder.colour) ?? "var(--color-muted-foreground)",
            }}
          >
            {collapsed ? <FolderClosed className="size-3" /> : <FolderOpen className="size-3" />}
          </span>
          <span
            className="eyebrow truncate"
            style={
              tagVar(folder.colour)
                ? { color: "color-mix(in oklab, var(--tag) 75%, var(--foreground))" }
                : undefined
            }
          >
            {folder.name}
          </span>
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
                <Trash className="size-3.5" />
                Delete folder
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </span>
      </div>

      {!collapsed && (
        // The indent rule is structure, not colour: it says these rows are
        // children of the row above. It stays neutral now that each child
        // carries the folder's colour as its own fill — drawing the hue a
        // third time added nothing except something else to look at.
        <div className="mt-1 ml-2.5 space-y-1 border-l border-hairline pl-2">
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

/** Everything not in a folder. */
function UnfiledGroup({
  items,
  hasFolders,
  ...rows
}: RowHandlers & { items: TerminalWorkspace[]; hasFolders: boolean }) {
  if (items.length === 0 && !hasFolders) return null

  return (
    <div className="min-w-0" data-folder="">
      {hasFolders && (
        <div className="mt-1 flex items-center gap-1.5 px-1.5 py-1">
          <span className="eyebrow truncate text-muted-foreground/70">Unfiled</span>
          <span className="numeric rounded-full bg-muted px-1.5 text-[10px] leading-4 text-muted-foreground">
            {items.length}
          </span>
          <span className="flex-1" />
          <IconAction label="New unfiled session" className="size-6" onClick={() => rows.onNew()}>
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
  const accent = tagVar(colour) ?? "var(--primary)"

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
      data-session={session.tmuxName ?? session.id}
      data-active={active || undefined}
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
        !active && !windowOver && "border-transparent",
        !active && !colour && "hover:border-hairline hover:bg-[var(--row-hover)]",
        !active && colour && "hover:brightness-110",
        active && "shadow-xs",
        active && !colour && !windowOver && "raised bg-card",
        windowOver && "border-dashed border-primary bg-primary/10",
      )}
      // The selected row is lifted into a card — the design system's own
      // sheen: a border, a hairline gradient off the top edge, a soft shadow.
      // A flat fill on its own read as a grey wash in the light theme, since
      // the app's ink *is* grey there. A coloured session keeps its hue: the
      // border and a gentle top-to-bottom tint carry the same "raised" reading
      // in the tag colour.
      style={{
        ...tagStyle(colour),
        ...(active && colour
          ? {
              backgroundImage:
                "linear-gradient(180deg, color-mix(in oklab, var(--tag) 22%, var(--card)), color-mix(in oklab, var(--tag) 13%, var(--card)) 60%)",
              borderColor: "color-mix(in oklab, var(--tag) 45%, transparent)",
            }
          : !active && colour
            ? { backgroundColor: "color-mix(in oklab, var(--tag) 11%, var(--card))" }
            : undefined),
      }}
    >
      <button
        onClick={() => onSelect(session)}
        className="flex min-w-0 flex-1 items-center gap-2 text-left"
      >
        {/*
          Three states, and it was one before: everything the dashboard was
          holding drew the same green dot, which on a page where that is nearly
          every session is a light that is always on and therefore says
          nothing.

            - **Filled, in the row's accent, with a halo** — the session on
              screen. The accent bar carries this too; the dot keeps it legible
              when the row is scrolled to the edge.
            - **Filled green** — running and attached, ready without a wait.
            - **Hollow** — running on the host with no PTY. Still alive; the
              difference is that clicking costs a reattach.
        */}
        <span
          data-status={active ? "active" : session.live ? "live" : "detached"}
          className={cn(
            "size-2 shrink-0 rounded-full transition-colors",
            !active && session.live && "bg-success",
            !active && !session.live && "ring-1 ring-muted-foreground/60 ring-inset",
          )}
          style={
            active
              ? {
                  backgroundColor: accent,
                  boxShadow: `0 0 0 2px color-mix(in oklab, ${accent} 30%, transparent)`,
                }
              : undefined
          }
        />
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1">
            {session.favourite && <Pin className="size-2.5 shrink-0 text-warning" />}
            <span className={cn("truncate text-[13px] leading-tight", active && "font-medium")}>
              {session.title}
            </span>
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
          {session.favourite ? <Pin /> : <Pin />}
        </IconAction>
        {/* Closing is the third thing anybody does to a session and it was two
            clicks down a menu. It is still in the menu for anybody who goes
            looking there. */}
        <IconAction
          label={`Close ${session.title}`}
          className="size-6 text-muted-foreground hover:text-destructive"
          onClick={() => onClose(session)}
        >
          <Cross />
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
                <FolderClosed className="size-3.5" />
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
              <Cross className="size-3.5" />
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
        <BlendMode className="size-3.5" />
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
