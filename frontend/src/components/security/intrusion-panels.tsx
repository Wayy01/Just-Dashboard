"use client"

import { Ban, History } from "lucide-react"
import { get, ApiError } from "@/lib/api"
import { timestamp } from "@/lib/format"
import type { BanEvent, Fail2banJail } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { Status } from "@/components/status-dot"
import { AreaFindings } from "@/components/security/posture-panel"
import { JailPanel } from "@/components/security/jail-panel"
import { OffendersPanel } from "@/components/security/offenders-panel"
import { useSecurity } from "@/components/security/security-context"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/**
 * fail2ban: the jails, the addresses they are holding, and — under that — what
 * the tool has actually been doing, because a ban expires and the jail is
 * empty again by morning however busy the night was.
 */
export function IntrusionPanels() {
  const { can } = useAuth()
  const { posture } = useSecurity()
  const { data, error, loading, refresh } = usePoll(
    (signal) =>
      get<{ available: boolean; running: boolean; jails: Fail2banJail[]; error?: string }>(
        "/fail2ban/",
        undefined,
        signal,
      ),
    20000,
  )

  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />
  if (!data?.available) {
    return (
      <EmptyState
        icon={Ban}
        title="fail2ban is not installed"
        description="It turns an endless brute-force against a port that has to stay open into a few attempts and a ban, which is the one thing a firewall cannot do for SSH."
      />
    )
  }
  if (!data.running) {
    return (
      <EmptyState
        icon={Ban}
        title="fail2ban is installed but not responding"
        description={
          data.error ?? "Installed and stopped is the state that looks protected and is not."
        }
      />
    )
  }

  return (
    <div className="space-y-4">
      <AreaFindings posture={posture} area="intrusion" />

      <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        {data.jails.map((jail) => (
          <JailPanel key={jail.name} jail={jail} canManage={can("system.admin")} onChanged={refresh} />
        ))}
        {data.jails.length === 0 && (
          <EmptyState
            icon={Ban}
            title="No jails configured"
            description="A running fail2ban with no jails bans nobody. Enable at least the sshd jail."
          />
        )}
      </div>

      <OffendersPanel onBlocked={refresh} />
      <BanHistoryPanel />
    </div>
  )
}

/**
 * What fail2ban has done recently, read from its own log — not remembered by
 * the dashboard and not inferred by polling the jail: a ban shorter than the
 * interval would never be seen that way, and the events either side of a
 * restart would be invented.
 */
function BanHistoryPanel() {
  const { data, error, loading } = usePoll(
    (signal) => get<BanEvent[]>("/fail2ban/history", { limit: 100 }, signal),
    60000,
  )

  const unavailable = error instanceof ApiError && error.code === "fail2ban_unavailable"

  return (
    <Panel>
      <PanelHeader
        icon={History}
        title="Ban activity"
        description="From fail2ban's own log — including bans that have since expired"
      />
      <PanelBody flush>
        {unavailable ? (
          <Notice tone="default" title="No fail2ban log on this host">
            fail2ban is not installed, or it logs only to the journal. There is no file to read back.
          </Notice>
        ) : error ? (
          <ErrorState error={error} />
        ) : loading ? (
          <LoadingPanel />
        ) : !data?.length ? (
          <EmptyState icon={History} title="No ban activity recorded" />
        ) : (
          <Table containerClassName="max-h-[24rem]">
            <TableHeader>
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>Jail</TableHead>
                <TableHead className="w-full">Address</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((event, i) => (
                <TableRow key={`${event.at}-${event.ip}-${i}`}>
                  <TableCell className="text-xs whitespace-nowrap text-muted-foreground">
                    {timestamp(event.at)}
                  </TableCell>
                  <TableCell>
                    <Status
                      state={event.action === "ban" ? "failed" : "exited"}
                      label={event.action}
                    />
                  </TableCell>
                  <TableCell className="text-[13px]">{event.jail}</TableCell>
                  <TableCell className="font-mono text-xs">{event.ip}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </PanelBody>
    </Panel>
  )
}
