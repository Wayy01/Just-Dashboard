"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import {
  Maximize2,
  Minimize2,
  PanelLeft,
  PanelRight,
  Plus,
  ShieldAlert,
  TerminalSquare,
} from "lucide-react"
import { toast } from "sonner"
import { del, get, patch, post, put } from "@/lib/api"
import type {
  TerminalFolder,
  TerminalPane,
  TerminalWindow,
  TerminalWorkspace,
} from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { actionFor, keymap, type ShortcutAction } from "@/lib/terminal-keymap"
import { cn } from "@/lib/utils"
import { useConfirm } from "@/components/confirm-dialog"
import { Page } from "@/components/page"
import { XtermPane } from "@/components/xterm-pane"
import { SessionRail } from "@/components/terminal/session-rail"
import { PaneBar, WindowStrip } from "@/components/terminal/window-strip"
import { WorkspaceTools } from "@/components/terminal/workspace-tools"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { Button } from "@/components/ui/button"

type TerminalList = {
  enabled: boolean
  tmux: boolean
  /** Who a new session logs in as, and where it lands. */
  login: { user: string; home: string; shell: string; error?: string }
  folders: TerminalFolder[]
  sessions: TerminalWorkspace[]
}

/** Every persistent-session route is addressed by tmux name. */
const persistent = (name: string) => `/terminal/persistent/${encodeURIComponent(name)}`

export default function TerminalPage() {
  const { confirm, dialog } = useConfirm()
  const router = useRouter()
  const [picked, setPicked] = useState<string | null>(null)
  // The session rail and the Files+Git companion can each be toggled, and the
  // whole workspace can go fullscreen. These are page state, not the pane's, so
  // that fullscreen keeps the rail, the strips *and* the tools — the previous
  // fullscreen zoomed only the terminal, which is exactly what put the file
  // tree and git out of reach the moment you maximised. Toggling a panel is
  // pure React state and never touches the Fullscreen API, so it works without
  // dropping out of fullscreen.
  const [showRail, setShowRail] = useState(true)
  const [showTools, setShowTools] = useState(true)
  const [immersive, setImmersive] = useState(false)
  const workspaceRef = useRef<HTMLDivElement>(null)
  // Deep link: a compose stack, a repository or a build context can hand the
  // terminal a directory to start in, so "I need a shell here" does not mean
  // opening one and retyping the path. Read above the early returns below,
  // because a hook cannot be called conditionally.
  const params = useSearchParams()
  const requestedCwd = params.get("cwd")
  const requestedFolder = params.get("folder") ?? undefined
  const launched = useRef(false)
  // The navigation shortcut handlers, rebuilt each render and read by a
  // listener that is installed once. See where it is filled in below.
  const navigationRef = useRef<Partial<Record<ShortcutAction, () => void>>>({})

  const { data, error, loading, refresh } = usePoll(
    (signal) => get<TerminalList>("/terminal/", undefined, signal),
    10000,
  )

  const openSession = useCallback(
    async (cwd?: string, folder?: string) => {
      try {
        const session = await post<{ id: string }>("/terminal/", {
          rows: 30,
          cols: 110,
          persist: true,
          cwd,
          folder,
          // Named after the directory, because a shell opened from a stack is
          // one of several a minute later and "Terminal 3" says nothing about
          // which one it was.
          title: cwd ? cwd.split("/").filter(Boolean).pop() : undefined,
        })
        await refresh()
        setPicked(session.id)
      } catch (err) {
        toast.error("Could not open a terminal", { description: String(err) })
      }
    },
    [refresh],
  )

  // Fullscreen is the browser's real thing, requested on the whole workspace
  // rather than on the pane, so the rail, the strips and the tools go with it.
  // The CSS overlay classes are applied whenever immersive regardless, so a
  // browser that refuses the request still gives a viewport-filling workspace.
  const toggleImmersive = useCallback(() => {
    if (immersive) {
      setImmersive(false)
      if (document.fullscreenElement) void document.exitFullscreen().catch(() => {})
    } else {
      setImmersive(true)
      void workspaceRef.current?.requestFullscreen?.().catch(() => {})
    }
  }, [immersive])

  // Esc leaves native fullscreen through the browser; drop the overlay with it
  // so the two can never disagree.
  useEffect(() => {
    const onChange = () => {
      if (!document.fullscreenElement) setImmersive(false)
    }
    document.addEventListener("fullscreenchange", onChange)
    return () => document.removeEventListener("fullscreenchange", onChange)
  }, [])

  // Opening a shell starts a process on the server, so this fires at most once
  // per mount: the ref is set before the call, which is what stops React's
  // development double-invoke from spawning two.
  //
  // It deliberately does not skip when sessions already exist. It used to, on
  // the reasoning that a shell is expensive and one may as well be reused —
  // which quietly broke the feature for everybody who keeps a terminal open,
  // i.e. everybody: "Shell here" landed them in whatever directory their
  // existing session happened to be in.
  useEffect(() => {
    if (launched.current || !requestedCwd || !data) return
    launched.current = true
    void openSession(requestedCwd, requestedFolder)
  }, [requestedCwd, requestedFolder, data, openSession])

  // The live session showing in the pane. Derived rather than stored, so a
  // session that was closed elsewhere falls back on its own instead of
  // leaving a dead pane behind.
  const live = useMemo(() => (data?.sessions ?? []).filter((s) => s.live && s.id), [data])
  const active = live.some((s) => s.id === picked) ? picked : (live[0]?.id ?? null)
  const activeSession = live.find((s) => s.id === active)
  const tmuxName = activeSession?.tmuxName

  // The windows of the active session — tmux's own tabs. Polled only while
  // something is attached, and slowly: a window appears when somebody makes
  // one, which is not a thing that happens between two heartbeats. The one
  // exception is the activity and bell flags, which change on their own — and
  // 5s is soon enough for "something happened in that tab".
  const windows = usePoll<TerminalWindow[]>(
    (signal) => get<TerminalWindow[]>(`${persistent(tmuxName ?? "")}/windows`, undefined, signal),
    5000,
    [tmuxName],
    { enabled: Boolean(tmuxName) },
  )

  const activeWindow = (windows.data ?? []).find((w) => w.active)

  // Where the tools panel looks: the active window's current directory, which
  // tmux updates as the shell cds around, falling back to where the session was
  // opened. This is what the file tree roots at and what git detection runs on.
  const currentDir = activeWindow?.cwd || activeSession?.cwd || undefined

  // Panes, only for the window on screen and only once it has been split.
  // Asking for them unconditionally would be a subprocess on the host every
  // five seconds for the overwhelmingly common case of one pane.
  //
  // The index is required rather than defaulted. Falling back to window 0 read
  // as an occasional wrong answer instead of no answer: between a window being
  // selected and the next windows poll no window reports itself active, and
  // during that gap the bar listed window 0's panes and clicking one selected
  // a pane in a window nobody was looking at.
  const paneWindow = activeWindow?.index
  const panes = usePoll<TerminalPane[]>(
    (signal) =>
      get<TerminalPane[]>(`${persistent(tmuxName ?? "")}/windows/${paneWindow}/panes`, undefined, signal),
    5000,
    [tmuxName, paneWindow],
    {
      enabled: Boolean(tmuxName) && paneWindow !== undefined && (activeWindow?.panes ?? 1) > 1,
    },
  )

  // Everything the shell inside the pane does not own: moving between
  // sessions, windows and panes. It is one listener in the capture phase for
  // a reason — the terminal has focus, and a bubbling listener would run after
  // xterm had already forwarded the keystroke to the PTY, so Ctrl+Alt+→ would
  // switch the window *and* type an escape sequence at the prompt.
  //
  // The pane's own shortcuts (copy, paste, search) live in XtermPane against
  // the same keymap under a different scope, so a chord belonging to the other
  // listener falls through here rather than being swallowed.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      // Not while somebody is typing into the rail's filter or renaming a row
      // — but the terminal itself does not count, and that exclusion is the
      // whole point. xterm takes keystrokes through a hidden
      // `.xterm-helper-textarea`, so a plain "is the target a text field"
      // guard suppresses every one of these shortcuts exactly when the
      // terminal has focus, which is the only time anybody presses them.
      const target = event.target as HTMLElement | null
      if (target?.closest("input, textarea, [contenteditable=true]") && !target.closest(".xterm")) {
        return
      }
      const action = actionFor(event, "navigation", keymap())
      if (!action) return
      const handler = navigationRef.current[action]
      if (!handler) return
      event.preventDefault()
      event.stopPropagation()
      handler()
    }
    window.addEventListener("keydown", onKey, { capture: true })
    return () => window.removeEventListener("keydown", onKey, { capture: true })
  }, [])

  /** Every mutation refreshes; the failure names what did not happen. */
  const act = useCallback(
    async (fn: () => Promise<unknown>, failure: string, alsoWindows = false) => {
      try {
        await fn()
      } catch (err) {
        toast.error(failure, { description: String(err) })
        return
      }
      if (alsoWindows) {
        windows.refresh()
        // And the panes with them. Without this a click on a pane chip did the
        // right thing on the server and changed nothing on screen until the
        // next five-second tick, which reads as a control that works about one
        // time in three.
        panes.refresh()
      }
      refresh()
    },
    [refresh, windows, panes],
  )

  /** Attaching to something that is running but has no PTY costs a reattach. */
  const select = async (session: TerminalWorkspace) => {
    if (session.id) {
      setPicked(session.id)
      return
    }
    if (!session.tmuxName) return
    try {
      const res = await post<{ id: string }>("/terminal/reattach", {
        tmuxName: session.tmuxName,
        rows: 30,
        cols: 110,
      })
      await refresh()
      setPicked(res.id)
    } catch (err) {
      toast.error("Could not pick that session up", { description: String(err) })
    }
  }

  /**
   * Naming, filing, pinning and colouring all write to the same place: tmux
   * user options on the session itself, so they outlive this process exactly
   * as the work does. A session without a tmux name has nowhere to keep them.
   *
   * Only the changed field is sent. The server merges it onto what the session
   * already has, which is what makes a drag into a folder — a request carrying
   * one field — safe: the earlier shape, where the client echoed all four,
   * erased whatever the client had not looked at recently.
   */
  const setMeta = (tmuxName: string | undefined, next: Partial<TerminalWorkspace>) => {
    if (!tmuxName) {
      toast.error("This session is not persistent, so it cannot be named")
      return
    }
    return act(
      () =>
        patch(persistent(tmuxName), {
          ...(next.title !== undefined && { title: next.title }),
          ...(next.folder !== undefined && { folder: next.folder }),
          ...(next.favourite !== undefined && { favourite: next.favourite }),
          ...(next.colour !== undefined && { colour: next.colour }),
        }),
      "Could not save that",
    )
  }

  /**
   * Closing a session, straight through.
   *
   * It used to sit behind a typed phrase. That guard is right for the things
   * somebody deletes a handful of times a year, and wrong here: closing a
   * shell is an everyday act, and a phrase in front of an everyday act does
   * not get read — it gets typed. The server dropped the phrase for the same
   * reason and still records the close in the audit log.
   */
  const closeSession = async (session: TerminalWorkspace) => {
    try {
      if (session.id) {
        await del(`/terminal/${session.id}`)
      } else if (session.tmuxName) {
        // Nothing is attached, so there is no session id to address. Picking
        // it up first is the only route the API offers to killing it.
        const res = await post<{ id: string }>("/terminal/reattach", {
          tmuxName: session.tmuxName,
          rows: 24,
          cols: 80,
        })
        await del(`/terminal/${res.id}`)
      }
    } catch (err) {
      toast.error("Could not close that session", { description: String(err) })
      return
    }
    if (active === session.id) setPicked(null)
    refresh()
  }

  const deleteFolder = (folder: TerminalFolder) =>
    confirm({
      title: `Delete ${folder.name}`,
      confirmLabel: "Delete folder",
      description: (
        <p>
          The folder goes and the sessions in it move to <b>Unfiled</b>. Nothing running is
          touched — this is filing, not closing.
        </p>
      ),
      action: () =>
        act(() => del(`/terminal/folders/${encodeURIComponent(folder.name)}`), "Could not delete that folder"),
    })

  const windowPatch = (index: number, body: Record<string, unknown>, failure: string) =>
    act(() => patch(`${persistent(tmuxName!)}/windows/${index}`, body), failure, true)

  const splitActive = (vertical: boolean) => {
    if (!activeWindow || !tmuxName) return
    void act(
      () => post(`${persistent(tmuxName)}/windows/${activeWindow.index}/panes`, { vertical }),
      "Could not split that window",
      true,
    )
  }

  const selectPane = (pane: number) =>
    activeWindow &&
    tmuxName &&
    act(
      () => patch(`${persistent(tmuxName)}/windows/${activeWindow.index}/panes/${pane}`, { select: true }),
      "Could not focus that pane",
      true,
    )

  const closeWindow = (index: number) =>
    act(
      () => del(`${persistent(tmuxName!)}/windows/${index}`),
      "Could not close that window",
      true,
    )

  const closePane = (pane: number) =>
    activeWindow &&
    act(
      () => del(`${persistent(tmuxName!)}/windows/${activeWindow.index}/panes/${pane}`),
      "Could not close that pane",
      true,
    )

  /**
   * What every navigation shortcut does, rebuilt each render and handed to the
   * listener through a ref.
   *
   * The listener is installed once — re-registering a window-level capture
   * handler on every render is a real cost while a build is scrolling past —
   * so it cannot close over this directly without going stale on the first
   * new session.
   *
   * `step` wraps in both directions on purpose: cycling is what `C-b n` does
   * in tmux and what Ctrl+Tab does in a browser, and stopping at the end is
   * only ever noticed as the shortcut having failed.
   */
  const step = <T,>(items: T[], current: number, by: number): T | undefined =>
    items.length === 0 ? undefined : items[(((current + by) % items.length) + items.length) % items.length]

  const windowList = windows.data ?? []
  const paneList = panes.data ?? []
  const navigation: Partial<Record<ShortcutAction, () => void>> = {
    "session.new": () => void openSession(),
    "session.next": () => {
      const at = live.findIndex((sess) => sess.id === active)
      const next = step(live, at < 0 ? -1 : at, 1)
      if (next?.id) setPicked(next.id)
    },
    "session.prev": () => {
      const at = live.findIndex((sess) => sess.id === active)
      const prev = step(live, at < 0 ? 0 : at, -1)
      if (prev?.id) setPicked(prev.id)
    },
    "window.new": () =>
      tmuxName &&
      void act(() => post(`${persistent(tmuxName)}/windows`, {}), "Could not open a window", true),
    "window.next": () => {
      const at = windowList.findIndex((w) => w.active)
      const next = step(windowList, at < 0 ? -1 : at, 1)
      if (next) windowPatch(next.index, { select: true }, "Could not switch window")
    },
    "window.prev": () => {
      const at = windowList.findIndex((w) => w.active)
      const prev = step(windowList, at < 0 ? 0 : at, -1)
      if (prev) windowPatch(prev.index, { select: true }, "Could not switch window")
    },
    "window.close": () => activeWindow && windowList.length > 1 && closeWindow(activeWindow.index),
    "pane.next": () => {
      const at = paneList.findIndex((pane) => pane.active)
      const next = step(paneList, at < 0 ? -1 : at, 1)
      if (next) void selectPane(next.index)
    },
    "pane.prev": () => {
      const at = paneList.findIndex((pane) => pane.active)
      const prev = step(paneList, at < 0 ? 0 : at, -1)
      if (prev) void selectPane(prev.index)
    },
    "pane.zoom": () => {
      const current = paneList.find((pane) => pane.active) ?? paneList[0]
      if (!current || !activeWindow || !tmuxName) return
      void act(
        () =>
          patch(`${persistent(tmuxName)}/windows/${activeWindow.index}/panes/${current.index}`, {
            zoom: true,
          }),
        "Could not zoom that pane",
        true,
      )
    },
    "pane.splitRight": () => splitActive(true),
    "pane.splitDown": () => splitActive(false),
    "pane.close": () => {
      const current = paneList.find((pane) => pane.active)
      if (current && paneList.length > 1) closePane(current.index)
    },
  }
  for (const n of [1, 2, 3, 4, 5, 6, 7, 8, 9] as const) {
    navigation[`session.${n}`] = () => {
      const target = live[n - 1]
      if (target?.id) setPicked(target.id)
    }
    navigation[`window.${n}`] = () => {
      const target = windowList[n - 1]
      if (target) windowPatch(target.index, { select: true }, "Could not switch window")
    }
  }
  // Written in an effect rather than during render: a ref assigned mid-render
  // makes the component's output depend on when it ran.
  useEffect(() => {
    navigationRef.current = navigation
  })

  if (loading) {
    return (
      <Page fill className="px-2 py-2 md:px-3 md:py-3">
        <LoadingPanel rows={4} />
      </Page>
    )
  }
  if (error) {
    return (
      <Page className="px-2 py-2 md:px-3 md:py-3">
        <ErrorState error={error} />
      </Page>
    )
  }
  if (!data?.enabled) {
    return (
      <Page className="px-2 py-2 md:px-3 md:py-3">
        <EmptyState
          icon={TerminalSquare}
          title="The web terminal is disabled"
          description="Set JD_TERMINAL_ENABLED=true on the backend to turn it on. It grants a shell with this process's privileges, so leaving it off is a reasonable default."
        />
      </Page>
    )
  }

  return (
    // No page header, and a tighter gutter than every other page.
    //
    // A terminal is the one screen whose content *is* the viewport: every row
    // the chrome takes is a row of output the operator cannot see, and a title
    // band plus an explanatory notice was costing about a fifth of the pane on
    // a laptop. What they said is not lost — the breadcrumb above already says
    // where you are, "New session" moved into the rail beside "New folder",
    // and which account a shell runs as is on the pane's own header, where it
    // is next to the shell rather than three inches above it.
    <Page fill className="gap-2 px-2 py-2 md:gap-2 md:px-3 md:py-3">
      {/*
        The one banner that stays. A missing account is not information, it is
        a broken feature: no shell can open at all until it is fixed, so it
        cannot be a line in a tooltip.
      */}
      {data.login.error && (
        <Notice icon={ShieldAlert} tone="danger" title="No account to log in as">
          {data.login.error} Set <code className="font-mono">JD_TERMINAL_USER</code> to an account
          that exists on this server.
        </Notice>
      )}

      {/*
        The rail is always here, even with nothing in it.

        It used to be replaced wholesale by an empty state, which read well and
        made the feature unreachable in one specific way: the only control that
        creates a folder lives in the rail, so a fresh install could open a
        session but could not make anywhere to put it. An empty rail with its
        filter and its "New folder" button is the more honest picture anyway —
        it shows what this page is.
      */}
        <div
          ref={workspaceRef}
          className={cn(
            "flex min-h-0 flex-1 flex-col gap-3 lg:flex-row",
            // The overlay is CSS so the panel toggles never touch the
            // Fullscreen API; the real fullscreen request rides on top of it.
            immersive && "fixed inset-0 z-50 gap-2 overflow-hidden bg-background p-2",
          )}
        >
          {showRail && (
          <SessionRail
            sessions={data.sessions}
            folders={data.folders}
            activeId={active}
            onSelect={select}
            onRename={(s, title) => setMeta(s.tmuxName, { title })}
            onTogglePinned={(s) => setMeta(s.tmuxName, { favourite: !s.favourite })}
            onSetFolder={(tmuxName, folder) => setMeta(tmuxName, { folder })}
            onSetColour={(s, colour) => setMeta(s.tmuxName, { colour })}
            onClose={closeSession}
            onNew={(folder) => openSession(undefined, folder)}
            onCreateFolder={(name) =>
              act(() => post("/terminal/folders", { name }), "Could not create that folder")
            }
            onUpdateFolder={(name, next) =>
              act(
                () => patch(`/terminal/folders/${encodeURIComponent(name)}`, next),
                "Could not update that folder",
              )
            }
            onDeleteFolder={deleteFolder}
            onReorderFolders={(names) =>
              act(
                () =>
                  put("/terminal/folders", {
                    // The colours travel with the order: the endpoint replaces
                    // the record, so sending names alone would repaint every
                    // folder grey.
                    folders: names.map((name) => ({
                      name,
                      colour: data.folders.find((f) => f.name === name)?.colour ?? "",
                    })),
                  }),
                "Could not reorder the folders",
              )
            }
            onMoveWindow={(from, index, to) =>
              act(
                () => patch(`${persistent(from)}/windows/${index}`, { session: to }),
                "Could not move that window",
                true,
              )
            }
          />
          )}

          <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
            {/*
              The workspace controls. Slim on purpose — every row here is a row
              of shell output nobody can see — but this is the one place the rail
              and the tools panel can be shown or hidden, and the whole workspace
              taken fullscreen. In fullscreen these are the only way back to the
              sessions list and the file tree, so the bar stays put there too.
            */}
            <div className="flex shrink-0 items-center gap-1 rounded-lg border bg-card px-1.5 py-1">
              <WorkspaceToggle
                active={showRail}
                onClick={() => setShowRail((v) => !v)}
                label={showRail ? "Hide the sessions rail" : "Show the sessions rail"}
                icon={PanelLeft}
              />
              {currentDir && (
                <span
                  className="min-w-0 truncate px-1 font-mono text-[11px] text-muted-foreground"
                  title={currentDir}
                >
                  {currentDir}
                </span>
              )}
              <span className="flex-1" />
              <WorkspaceToggle
                active={showTools}
                onClick={() => setShowTools((v) => !v)}
                label={showTools ? "Hide files & git" : "Show files & git"}
                icon={PanelRight}
              />
              <WorkspaceToggle
                active={immersive}
                onClick={toggleImmersive}
                label={immersive ? "Leave fullscreen" : "Fullscreen workspace"}
                icon={immersive ? Minimize2 : Maximize2}
              />
            </div>

            {tmuxName && (
              <WindowStrip
                windows={windows.data ?? []}
                sessionName={tmuxName}
                sessionColour={activeSession?.colour}
                onSelect={(index) => windowPatch(index, { select: true }, "Could not switch window")}
                onRename={(index, name) =>
                  windowPatch(index, { name }, "Could not rename that window")
                }
                onColour={(index, colour) =>
                  windowPatch(index, { colour }, "Could not colour that window")
                }
                onReorder={(index, position) =>
                  windowPatch(index, { position }, "Could not move that window")
                }
                onSplit={(_index, vertical) => splitActive(vertical)}
                onLayout={(index, layout) =>
                  windowPatch(index, { layout }, "Could not rearrange the panes")
                }
                onSynchronize={(index, on) =>
                  windowPatch(index, { synchronize: on }, "Could not change synchronised typing")
                }
                onNew={() =>
                  act(
                    () => post(`${persistent(tmuxName)}/windows`, {}),
                    "Could not open a window",
                    true,
                  )
                }
                onClose={(index) => closeWindow(index)}
              />
            )}

            {tmuxName && activeWindow && (
              <PaneBar
                panes={panes.data ?? []}
                onSelect={(pane) =>
                  act(
                    () =>
                      patch(
                        `${persistent(tmuxName)}/windows/${activeWindow.index}/panes/${pane}`,
                        { select: true },
                      ),
                    "Could not focus that pane",
                    true,
                  )
                }
                onZoom={(pane) =>
                  act(
                    () =>
                      patch(`${persistent(tmuxName)}/windows/${activeWindow.index}/panes/${pane}`, {
                        zoom: true,
                      }),
                    "Could not zoom that pane",
                    true,
                  )
                }
                onClose={(pane) => void closePane(pane)}
              />
            )}

            {active ? (
              <XtermPane
                key={active}
                path={`/terminal/${active}/attach`}
                // The prompt inside already says where you are; the header says
                // who, which is the fact a root-equivalent shell should never
                // make you go and check.
                subtitle={`${activeSession?.title ?? ""} · ${
                  activeSession?.user ?? data.login.user
                }`}
                cwd={activeSession?.cwd}
                onOpenFiles={(path) => router.push(`/files?path=${encodeURIComponent(path)}`)}
                // Clicking in a pane focuses it, which is what clicking in
                // anything does — and until now the only way to move the focus
                // was the chip in the bar above, which is a long way to go to
                // type into the half of the screen you are already looking at.
                //
                // Nothing happens for a single pane, and nothing happens when
                // the click was already inside the focused one: every one of
                // those would be a tmux subprocess per click.
                onCellClick={({ col, row }) => {
                  if (paneList.length < 2) return
                  const hit = paneList.find(
                    (pane) =>
                      col >= pane.left && col <= pane.right && row >= pane.top && row <= pane.bottom,
                  )
                  if (hit && !hit.active) void selectPane(hit.index)
                }}
                // No minimum height: the pane is whatever is left after the
                // header and the strips, and a floor taller than that would
                // push the page past the window — which is the one thing a
                // terminal must never do, because xterm sizes itself from the
                // box and would latch at the larger size for good.
                className="min-h-0 flex-1"
                onExit={refresh}
                // The pane's fullscreen button and shortcut take the whole
                // workspace fullscreen instead of the pane, so the rail and the
                // tools stay reachable while maximised.
                onToggleFullscreen={toggleImmersive}
                fullscreenActive={immersive}
              />
            ) : (
              <EmptyState
                className="flex-1"
                icon={TerminalSquare}
                title={data.sessions.length === 0 ? "No sessions yet" : "Pick a session"}
                description={
                  data.sessions.length === 0
                    ? "A session is a shell that keeps running — name it, file it in a folder, colour the folder, and come back to it tomorrow exactly as you left it."
                    : "A hollow dot means it is running on the server with nothing attached — clicking picks it up where you left it."
                }
                action={
                  data.sessions.length === 0 ? (
                    <Button size="sm" onClick={() => openSession()}>
                      <Plus className="size-4" />
                      Open one
                    </Button>
                  ) : undefined
                }
              />
            )}
          </div>

          {showTools && (
            <div className="flex min-h-[16rem] shrink-0 flex-col lg:h-auto lg:min-h-0 lg:w-[21rem] xl:w-[23rem]">
              <WorkspaceTools
                dir={currentDir}
                onOpenInFiles={(path) => router.push(`/files?path=${encodeURIComponent(path)}`)}
                onClose={() => setShowTools(false)}
              />
            </div>
          )}
        </div>
      {dialog}
    </Page>
  )
}

/** A toggle in the workspace control bar: pressed-in when its panel is shown. */
function WorkspaceToggle({
  active,
  onClick,
  label,
  icon: Icon,
}: {
  active: boolean
  onClick: () => void
  label: string
  icon: React.ComponentType<{ className?: string }>
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      title={label}
      aria-label={label}
      aria-pressed={active}
      className={cn(
        "size-7 shrink-0 p-0",
        active ? "bg-primary/12 text-primary" : "text-muted-foreground hover:text-foreground",
      )}
      onClick={onClick}
    >
      <Icon className="size-3.5" />
    </Button>
  )
}
