"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { useSearchParams } from "next/navigation"
import { Plus, ShieldAlert, TerminalSquare } from "lucide-react"
import { toast } from "sonner"
import { del, get, patch, post } from "@/lib/api"
import type { TerminalWindow, TerminalWorkspace } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { XtermPane } from "@/components/xterm-pane"
import { SessionRail, WindowStrip } from "@/components/terminal/session-rail"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { Button } from "@/components/ui/button"

type TerminalList = {
  enabled: boolean
  tmux: boolean
  /** Who a new session logs in as, and where it lands. */
  login: { user: string; home: string; shell: string; error?: string }
  sessions: TerminalWorkspace[]
}

export default function TerminalPage() {
  const { confirm, dialog } = useConfirm()
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
  const live = (data?.sessions ?? []).filter((s) => s.live && s.id)
  const active = live.some((s) => s.id === picked) ? picked : (live[0]?.id ?? null)
  const activeSession = live.find((s) => s.id === active)

  // The windows of the active session — tmux's own tabs. Polled only while
  // something is attached, and slowly: a window appears when somebody makes
  // one, which is not a thing that happens between two heartbeats.
  const windows = usePoll<TerminalWindow[]>(
    (signal) =>
      get<TerminalWindow[]>(
        `/terminal/persistent/${encodeURIComponent(activeSession?.tmuxName ?? "")}/windows`,
        undefined,
        signal,
      ),
    5000,
    [activeSession?.tmuxName],
    { enabled: Boolean(activeSession?.tmuxName) },
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
   * Naming, filing and starring all write to the same place: tmux user options
   * on the session itself, so they outlive this process exactly as the work
   * does. A session without a tmux name has nowhere to keep them.
   */
  const setMeta = async (session: TerminalWorkspace, next: Partial<TerminalWorkspace>) => {
    if (!session.tmuxName) {
      toast.error("This session is not persistent, so it cannot be named")
      return
    }
    try {
      await patch(`/terminal/persistent/${encodeURIComponent(session.tmuxName)}`, {
        title: next.title ?? session.title,
        folder: next.folder ?? session.folder ?? "",
        favourite: next.favourite ?? session.favourite,
      })
      refresh()
    } catch (err) {
      toast.error("Could not save that", { description: String(err) })
    }
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

  const windowAction = async (fn: () => Promise<unknown>, failure: string) => {
    try {
      await fn()
      windows.refresh()
      refresh()
    } catch (err) {
      toast.error(failure, { description: String(err) })
    }
  }

  const tmuxName = activeSession?.tmuxName

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

      {data.sessions.length === 0 ? (
        <EmptyState
          icon={TerminalSquare}
          title="No sessions yet"
          description="A session is a shell that keeps running — name it, file it in a folder, and come back to it tomorrow exactly as you left it."
          action={
            <Button size="sm" onClick={() => openSession()}>
              <Plus className="size-4" />
              Open one
            </Button>
          }
        />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-3 lg:flex-row">
          <SessionRail
            sessions={data.sessions}
            activeId={active}
            onSelect={select}
            onRename={(s, title) => setMeta(s, { title })}
            onToggleFavourite={(s) => setMeta(s, { favourite: !s.favourite })}
            onSetFolder={(s, folder) => setMeta(s, { folder })}
            onClose={closeSession}
            onNew={(folder) => openSession(undefined, folder)}
          />

          <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
            {tmuxName && (
              <WindowStrip
                windows={windows.data ?? []}
                onSelect={(index) =>
                  windowAction(
                    () =>
                      patch(
                        `/terminal/persistent/${encodeURIComponent(tmuxName)}/windows/${index}`,
                        { select: true },
                      ),
                    "Could not switch window",
                  )
                }
                onRename={(index, name) =>
                  windowAction(
                    () =>
                      patch(
                        `/terminal/persistent/${encodeURIComponent(tmuxName)}/windows/${index}`,
                        { name },
                      ),
                    "Could not rename that window",
                  )
                }
                onNew={() =>
                  windowAction(
                    () => post(`/terminal/persistent/${encodeURIComponent(tmuxName)}/windows`, {}),
                    "Could not open a window",
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
                      windowAction(
                        () =>
                          del(
                            `/terminal/persistent/${encodeURIComponent(tmuxName)}/windows/${index}`,
                            { confirm: c },
                          ),
                        "Could not close that window",
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
                title="Pick a session"
                description="A hollow dot means it is running on the server with nothing attached — clicking picks it up where you left it."
              />
            )}
          </div>
        </div>
      )}
      {dialog}
    </Page>
  )
}
