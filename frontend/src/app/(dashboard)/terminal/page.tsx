"use client"

import { useState } from "react"
import { Link2, Plus, TerminalSquare, X } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { TerminalSession } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { XtermPane } from "@/components/xterm-pane"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

type TerminalList = {
  enabled: boolean
  tmux: boolean
  sessions: TerminalSession[]
  detached: string[]
}

export default function TerminalPage() {
  const { confirm, dialog } = useConfirm()
  const [picked, setPicked] = useState<string | null>(null)
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<TerminalList>("/terminal/", undefined, signal),
    10000,
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

  const open = async () => {
    try {
      const session = await post<{ id: string }>("/terminal/", {
        rows: 30,
        cols: 110,
        persist: data.tmux,
      })
      await refresh()
      setPicked(session.id)
    } catch (err) {
      toast.error("Could not open a terminal", { description: String(err) })
    }
  }

  const reattach = async (tmuxName: string) => {
    try {
      const session = await post<{ id: string }>("/terminal/reattach", {
        tmuxName,
        rows: 30,
        cols: 110,
      })
      await refresh()
      setPicked(session.id)
    } catch (err) {
      toast.error("Could not reattach", { description: String(err) })
    }
  }

  // Opening the page with sessions already running and showing none of them is
  // a wasted click; the first is as good a default as any and the strip above
  // makes switching obvious. Derived, so a closed session falls back on its
  // own rather than leaving a dead pane.
  const active = data.sessions.some((s) => s.id === picked)
    ? picked
    : (data.sessions[0]?.id ?? null)

  // Sessions the dashboard is not currently holding a PTY for — typically
  // left behind by a restart, and worth offering back rather than orphaning.
  const attachedTmux = new Set(data.sessions.map((s) => s.tmuxName).filter(Boolean))
  const orphans = data.detached.filter((name) => !attachedTmux.has(name))

  return (
    <Page fill>
      <PageHeader
        eyebrow="Access"
        title="Terminal"
        description={
          data.tmux
            ? "Sessions are tmux-backed, so they survive a closed tab or a dashboard restart."
            : "tmux is not installed, so sessions end when they are closed."
        }
        actions={
          <Button size="sm" onClick={open}>
            <Plus className="size-4" />
            New session
          </Button>
        }
      />

      <Notice icon={TerminalSquare} title="This is a real shell on the host">
        Everything typed here runs with the dashboard process&apos;s privileges. Opening and closing
        a session is recorded in the audit log.
      </Notice>

      {orphans.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="eyebrow">Detached</span>
          {orphans.map((name) => (
            <Button key={name} size="sm" variant="outline" onClick={() => reattach(name)}>
              <Link2 className="size-3.5" />
              {name}
            </Button>
          ))}
        </div>
      )}

      {data.sessions.length === 0 ? (
        <EmptyState
          icon={TerminalSquare}
          title="No open sessions"
          action={
            <Button size="sm" onClick={open}>
              <Plus className="size-4" />
              Open one
            </Button>
          }
        />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          {/* Session chips read as a tab strip: the active one is filled, the
              rest are outlines, and the close control sits inside each chip
              rather than in a menu two clicks away. */}
          <div className="flex flex-wrap gap-1.5">
            {data.sessions.map((session) => (
              <div
                key={session.id}
                className={cn(
                  "flex items-center gap-1.5 rounded-lg border px-2 py-1 text-[13px] transition-colors",
                  active === session.id
                    ? "border-primary/40 bg-primary/10 text-foreground"
                    : "border-border hover:bg-accent",
                )}
              >
                <button onClick={() => setPicked(session.id)} className="flex items-center gap-1.5">
                  <TerminalSquare className="size-3.5" />
                  {session.title}
                  {session.persisted && (
                    <Badge variant="outline" className="text-[10px] font-normal">
                      tmux
                    </Badge>
                  )}
                </button>
                <button
                  className="text-muted-foreground transition-colors hover:text-destructive"
                  aria-label="Close session"
                  onClick={() =>
                    confirm({
                      title: "Close terminal",
                      phrase: "close terminal",
                      confirmLabel: "Close",
                      description: (
                        <p>
                          Ends the session and anything running inside it. A tmux-backed session is
                          destroyed too, not just detached.
                        </p>
                      ),
                      action: async (c) => {
                        await del(`/terminal/${session.id}`, { confirm: c })
                        if (active === session.id) setPicked(null)
                        refresh()
                      },
                    })
                  }
                >
                  <X className="size-3.5" />
                </button>
              </div>
            ))}
          </div>

          {active ? (
            <XtermPane
              key={active}
              path={`/terminal/${active}/attach`}
              // No minimum height: the pane is whatever is left after the
              // header and the session strip, and a floor taller than that
              // would push the page past the window again — which is the one
              // thing a terminal must never do, because xterm sizes itself
              // from the box and would latch at the larger size for good.
              className="min-h-0 flex-1"
              onExit={refresh}
            />
          ) : (
            <EmptyState
              className="flex-1"
              icon={TerminalSquare}
              title="Select a session above"
              description="Or open a new one — each is an independent shell."
            />
          )}
        </div>
      )}
      {dialog}
    </Page>
  )
}
