"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import { Plus, ShieldAlert, TerminalSquare } from "lucide-react"
import { toast } from "sonner"
import { del, get, patch, post, put } from "@/lib/api"
import type {
  TerminalFolder,
  TerminalPane,
  TerminalWindow,
  TerminalWorkspace,
} from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { XtermPane } from "@/components/xterm-pane"
import { SessionRail } from "@/components/terminal/session-rail"
import { PaneBar, WindowStrip } from "@/components/terminal/window-strip"
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
  // Deep link: a compose stack, a repository or a build context can hand the
  // terminal a directory to start in, so "I need a shell here" does not mean
  // opening one and retyping the path. Read above the early returns below,
  // because a hook cannot be called conditionally.
  const params = useSearchParams()
  const requestedCwd = params.get("cwd")
  const requestedFolder = params.get("folder") ?? undefined
  const launched = useRef(false)

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

  // Panes, only for the window on screen and only once it has been split.
  // Asking for them unconditionally would be a subprocess on the host every
  // five seconds for the overwhelmingly common case of one pane.
  const panes = usePoll<TerminalPane[]>(
    (signal) =>
      get<TerminalPane[]>(
        `${persistent(tmuxName ?? "")}/windows/${activeWindow?.index ?? 0}/panes`,
        undefined,
        signal,
      ),
    5000,
    [tmuxName, activeWindow?.index],
    { enabled: Boolean(tmuxName) && (activeWindow?.panes ?? 1) > 1 },
  )

  // Ctrl+Alt+1..9 switches session, the way a tabbed terminal does. Alt alone
  // is claimed by the shell inside the pane, so the chord is deliberately one
  // the terminal will never want.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.ctrlKey || !e.altKey || e.metaKey) return
      const n = Number(e.key)
      if (!n || n < 1 || n > 9) return
      const target = live[n - 1]
      if (target?.id) {
        e.preventDefault()
        setPicked(target.id)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [live])

  /** Every mutation refreshes; the failure names what did not happen. */
  const act = useCallback(
    async (fn: () => Promise<unknown>, failure: string, alsoWindows = false) => {
      try {
        await fn()
      } catch (err) {
        toast.error(failure, { description: String(err) })
        return
      }
      if (alsoWindows) windows.refresh()
      refresh()
    },
    [refresh, windows],
  )

  if (loading) {
    return (
      <Page>
        <PageHeader eyebrow="Access" title="Terminal" />
        <LoadingPanel rows={4} />
      </Page>
    )
  }
  if (error) {
    return (
      <Page>
        <PageHeader eyebrow="Access" title="Terminal" />
        <ErrorState error={error} />
      </Page>
    )
  }
  if (!data?.enabled) {
    return (
      <Page>
        <PageHeader
          eyebrow="Access"
          title="Terminal"
          description="A shell on the host, in the browser"
        />
        <EmptyState
          icon={TerminalSquare}
          title="The web terminal is disabled"
          description="Set JD_TERMINAL_ENABLED=true on the backend to turn it on. It grants a shell with this process's privileges, so leaving it off is a reasonable default."
        />
      </Page>
    )
  }

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

  const closeSession = (session: TerminalWorkspace) =>
    confirm({
      title: "Close terminal",
      phrase: "close terminal",
      confirmLabel: "Close",
      description: (
        <>
          <p>
            Ends <b>{session.title}</b> and everything running inside it, including any other
            windows it has.
          </p>
          <p>
            This is the only thing that stops a session — leaving the page, closing the tab and
            restarting the dashboard all leave it running.
          </p>
        </>
      ),
      action: async (c) => {
        if (session.id) {
          await del(`/terminal/${session.id}`, { confirm: c })
        } else if (session.tmuxName) {
          // Nothing is attached, so there is no session id to address. Picking
          // it up first is the only route the API offers to killing it.
          const res = await post<{ id: string }>("/terminal/reattach", {
            tmuxName: session.tmuxName,
            rows: 24,
            cols: 80,
          })
          await del(`/terminal/${res.id}`, { confirm: c })
        }
        if (active === session.id) setPicked(null)
        refresh()
      },
    })

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

  return (
    <Page fill>
      <PageHeader
        eyebrow="Access"
        title="Terminal"
        description={
          data.tmux
            ? "Sessions keep running: closing the tab, leaving the page and restarting the dashboard all leave them alone. Only closing one stops it."
            : "tmux is not installed, so sessions end when they are closed."
        }
        actions={
          <Button size="sm" onClick={() => openSession()}>
            <Plus className="size-4" />
            New session
          </Button>
        }
      />

      {data.login.error ? (
        <Notice icon={ShieldAlert} tone="danger" title="No account to log in as">
          {data.login.error} Set <code className="font-mono">JD_TERMINAL_USER</code> to an account
          that exists on this server.
        </Notice>
      ) : (
        <Notice icon={TerminalSquare} title="A login shell on the host, not in the container">
          Sessions log in as{" "}
          <code className="font-mono font-medium text-foreground">{data.login.user}</code> and start
          in <code className="font-mono">{data.login.home}</code>, running{" "}
          <code className="font-mono">{data.login.shell}</code> — the same as an SSH session, so
          your dotfiles, PATH and installed tools are all here. Opening and closing a session is
          recorded in the audit log.
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
        <div className="flex min-h-0 flex-1 flex-col gap-3 lg:flex-row">
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

          <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
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
                onSplit={(index, vertical) =>
                  act(
                    () => post(`${persistent(tmuxName)}/windows/${index}/panes`, { vertical }),
                    "Could not split that window",
                    true,
                  )
                }
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
                onClose={(index) =>
                  confirm({
                    title: "Close window",
                    phrase: "close window",
                    confirmLabel: "Close",
                    description: (
                      <p>
                        Ends this window and whatever is running in it. The session and its other
                        windows are untouched.
                      </p>
                    ),
                    action: (c) =>
                      act(
                        () => del(`${persistent(tmuxName)}/windows/${index}`, { confirm: c }),
                        "Could not close that window",
                        true,
                      ),
                  })
                }
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
                onClose={(pane) =>
                  confirm({
                    title: "Close pane",
                    phrase: "close pane",
                    confirmLabel: "Close",
                    description: (
                      <p>
                        Ends this pane and whatever is running in it. The window&apos;s other panes
                        are untouched.
                      </p>
                    ),
                    action: (c) =>
                      act(
                        () =>
                          del(
                            `${persistent(tmuxName)}/windows/${activeWindow.index}/panes/${pane}`,
                            { confirm: c },
                          ),
                        "Could not close that pane",
                        true,
                      ),
                  })
                }
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
                // No minimum height: the pane is whatever is left after the
                // header and the strips, and a floor taller than that would
                // push the page past the window — which is the one thing a
                // terminal must never do, because xterm sizes itself from the
                // box and would latch at the larger size for good.
                className="min-h-0 flex-1"
                onExit={refresh}
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
        </div>
      {dialog}
    </Page>
  )
}
