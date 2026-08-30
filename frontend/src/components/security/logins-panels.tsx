"use client"

import { useState } from "react"
import { ClockRewind, Logout, Users } from "@/components/icons"
import { get, post, ApiError } from "@/lib/api"
import { timestamp } from "@/lib/format"
import type { LoginRecord, LoginSession } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { Status } from "@/components/status-dot"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/**
 * The two halves of "who has been on this machine": a snapshot of the
 * interactive logins right now, and — the question that actually matters on an
 * exposed host — who got in overnight, which the host has been keeping in wtmp
 * all along.
 */
export function LoginsPanels() {
  return (
    <div className="space-y-4">
      <CurrentSessions />
      <LoginHistoryPanel />
    </div>
  )
}

function CurrentSessions() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<LoginSession[]>("/ssh-sessions", undefined, signal),
    10000,
  )
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />
  if (!data?.length) return <EmptyState icon={Users} title="No interactive logins" />

  return (
    <>
      <Panel>
        <PanelHeader
          icon={Users}
          title="Interactive logins"
          description={`${data.length} session${data.length === 1 ? "" : "s"} on this host right now`}
        />
        <PanelBody flush>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>User</TableHead>
                <TableHead>Terminal</TableHead>
                <TableHead className="w-full">From</TableHead>
                <TableHead>Logged in</TableHead>
                <TableHead>Idle</TableHead>
                <TableHead>Type</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((session, i) => (
                <TableRow key={`${session.user}-${session.tty}-${i}`} className="group">
                  <TableCell className="text-[13px] font-medium">{session.user}</TableCell>
                  <TableCell className="font-mono text-xs">{session.tty}</TableCell>
                  <TableCell className="font-mono text-xs">{session.from || "local"}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {session.loginTime ? timestamp(session.loginTime) : "—"}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {session.idle ?? "—"}
                  </TableCell>
                  <TableCell>
                    <Badge variant={session.isSsh ? "outline" : "secondary"} className="font-normal">
                      {session.isSsh ? "ssh" : "local"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {can("system.admin") && session.pid ? (
                      <Button
                        size="xs"
                        variant="ghost"
                        className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                        onClick={() =>
                          confirm({
                            title: `Disconnect ${session.user}`,
                            confirmLabel: "Disconnect",
                            description: (
                              <p>
                                The session on <b>{session.tty}</b> from{" "}
                                <b>{session.from || "local"}</b> is hung up. Anything it is running
                                in the foreground stops with it — a long job that was not started
                                under tmux or nohup will not survive.
                              </p>
                            ),
                            action: async () => {
                              await post(`/ssh-sessions/${session.pid}/disconnect`, {})
                              refresh()
                            },
                          })
                        }
                      >
                        <Logout className="size-3.5" />
                        Disconnect
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </PanelBody>
      </Panel>
      {dialog}
    </>
  )
}

/**
 * Recent logins, read from the host rather than remembered by the dashboard.
 *
 * Failed attempts are a separate request behind system.admin: btmp records
 * whatever was typed at a login prompt, and what people type at a login prompt
 * is sometimes their password in the username field.
 */
function LoginHistoryPanel() {
  const { can } = useAuth()
  const [failed, setFailed] = useState(false)
  const admin = can("system.admin")
  const showFailed = failed && admin

  const { data, error, loading } = usePoll(
    (signal) =>
      get<LoginRecord[]>(showFailed ? "/logins/failed" : "/logins", { limit: 100 }, signal),
    60000,
    [showFailed],
  )

  const unavailable = error instanceof ApiError && error.code === "login_history_unavailable"

  return (
    <Panel>
      <PanelHeader
        icon={ClockRewind}
        title={showFailed ? "Failed login attempts" : "Recent logins"}
        description={
          showFailed
            ? "From the host's btmp record — every attempt that did not get in"
            : "From the host's wtmp record, including restarts"
        }
        actions={
          admin && (
            <ToggleGroup
              type="single"
              value={showFailed ? "failed" : "ok"}
              onValueChange={(next) => setFailed(next === "failed")}
              variant="outline"
              size="sm"
              aria-label="Which logins to show"
            >
              <ToggleGroupItem value="ok" className="px-2.5 text-[11px]">
                Successful
              </ToggleGroupItem>
              <ToggleGroupItem value="failed" className="px-2.5 text-[11px]">
                Failed
              </ToggleGroupItem>
            </ToggleGroup>
          )
        }
      />
      <PanelBody flush>
        {unavailable ? (
          <Notice tone="default" title="This host cannot read its login record">
            <div className="space-y-1.5">
              <p>
                <code className="font-mono">last</code> and{" "}
                <code className="font-mono">lastb</code> are what read wtmp and btmp, and they come
                from <code className="font-mono">util-linux-extra</code> — which minimal cloud
                images leave out. Install it and this fills in; the records themselves have been
                there all along.
              </p>
              <p>
                Until then this page has no answer, which is not the same as a host nobody has
                tried to log in to.
              </p>
            </div>
          </Notice>
        ) : error ? (
          <ErrorState error={error} />
        ) : loading ? (
          <LoadingPanel />
        ) : !data?.length ? (
          <EmptyState
            icon={ClockRewind}
            title={showFailed ? "No failed attempts recorded" : "No logins recorded"}
          />
        ) : (
          <Table containerClassName="max-h-[28rem]">
            <TableHeader>
              <TableRow>
                <TableHead>User</TableHead>
                <TableHead>Terminal</TableHead>
                <TableHead className="w-full">From</TableHead>
                <TableHead>When</TableHead>
                <TableHead>Lasted</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((record, i) => (
                <TableRow key={`${record.user}-${record.loginTime ?? i}-${i}`}>
                  <TableCell className="text-[13px] font-medium">
                    <span className="flex items-center gap-2">
                      {record.user}
                      {record.kind !== "login" && (
                        <Badge variant="secondary" className="font-normal">
                          {record.kind === "boot" ? "boot" : "shutdown"}
                        </Badge>
                      )}
                    </span>
                  </TableCell>
                  <TableCell className="font-mono text-xs">{record.tty || "—"}</TableCell>
                  <TableCell className="font-mono text-xs">{record.from || "local"}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {record.loginTime ? timestamp(record.loginTime) : "—"}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {record.active ? (
                      <Status state="active" label="still open" />
                    ) : (
                      (record.duration ?? record.ended ?? "—")
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </PanelBody>
    </Panel>
  )
}
