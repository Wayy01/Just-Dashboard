"use client"

import { useState } from "react"
import { Link2, Plus, TerminalSquare, X } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import type { TerminalSession } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { XtermPane } from "@/components/xterm-pane"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { cn } from "@/lib/utils"

type TerminalList = {
  enabled: boolean
  tmux: boolean
  sessions: TerminalSession[]
  detached: string[]
}

export default function TerminalPage() {
  const { confirm, dialog } = useConfirm()
  const [active, setActive] = useState<string | null>(null)
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<TerminalList>("/terminal/", undefined, signal),
    10000,
  )

  if (loading) return <LoadingRows rows={4} />
  if (error) return <ErrorState error={error} />
  if (!data?.enabled) {
    return (
      <>
        <PageHeader title="Terminal" />
        <EmptyState
          icon={TerminalSquare}
          title="The web terminal is disabled"
          description="Set VPSD_TERMINAL_ENABLED=true on the backend to turn it on. It grants a shell with this process's privileges, so leaving it off is a reasonable default."
        />
      </>
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
      setActive(session.id)
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
      setActive(session.id)
    } catch (err) {
      toast.error("Could not reattach", { description: String(err) })
    }
  }

  // Sessions the dashboard is not currently holding a PTY for — typically
  // left behind by a restart, and worth offering back rather than orphaning.
  const attachedTmux = new Set(data.sessions.map((s) => s.tmuxName).filter(Boolean))
  const orphans = data.detached.filter((name) => !attachedTmux.has(name))

  return (
    <>
      <PageHeader
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

      <Alert>
        <TerminalSquare className="size-4" />
        <AlertTitle>This is a real shell on the host</AlertTitle>
        <AlertDescription>
          Everything typed here runs with the dashboard process&apos;s privileges. Opening and
          closing a session is recorded in the audit log.
        </AlertDescription>
      </Alert>

      {orphans.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm text-muted-foreground">Detached sessions:</span>
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
          <div className="flex flex-wrap gap-1.5">
            {data.sessions.map((session) => (
              <div
                key={session.id}
                className={cn(
                  "flex items-center gap-1.5 rounded-md border px-2 py-1 text-sm",
                  active === session.id ? "border-primary bg-accent" : "hover:bg-accent/50",
                )}
              >
                <button onClick={() => setActive(session.id)} className="flex items-center gap-1.5">
                  <TerminalSquare className="size-3.5" />
                  {session.title}
                  {session.persisted && (
                    <Badge variant="outline" className="text-[10px]">
                      tmux
                    </Badge>
                  )}
                </button>
                <button
                  className="text-muted-foreground hover:text-destructive"
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
                        if (active === session.id) setActive(null)
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
              className="min-h-[32rem] flex-1"
              onExit={refresh}
            />
          ) : (
            <EmptyState icon={TerminalSquare} title="Select a session above" />
          )}
        </div>
      )}
      {dialog}
    </>
  )
}
