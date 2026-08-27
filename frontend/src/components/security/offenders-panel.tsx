"use client"

import { Crosshair, ShieldPlus } from "lucide-react"
import { notify } from "@/lib/toast"
import { get, post, ApiError } from "@/lib/api"
import { relativeTime } from "@/lib/format"
import type { BanSummary } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { Sparkline } from "@/components/metrics/sparkline"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/**
 * Who keeps coming back.
 *
 * A jail lists the bans in force this instant, and a ban expires — so the
 * address banned eleven times this week is invisible the moment its eleventh
 * ban lapses, and the page reads as a quiet night. Folding the log the other
 * way round turns it into the one question worth asking of it: this is not a
 * passing scanner, it is somebody working through your host, and a permanent
 * firewall rule is a better answer than another ten-minute ban.
 */
export function OffendersPanel({ onBlocked }: { onBlocked?: () => void }) {
  const { can } = useAuth()
  const { data, error, loading, refresh } = usePoll<BanSummary>(
    (signal) => get("/fail2ban/offenders", { top: 15 }, signal),
    120000,
  )
  const unavailable = error instanceof ApiError && error.code === "fail2ban_unavailable"

  const block = async (ip: string) => {
    try {
      await post("/firewall/rules", {
        action: "deny",
        direction: "in",
        from: ip,
        comment: "repeat offender",
      })
      notify.success(`${ip} blocked at the firewall`, {
        description: "A firewall rule outlives a ban, which expires.",
      })
      onBlocked?.()
      refresh()
    } catch (err) {
      notify.error("Could not add the rule", err)
    }
  }

  return (
    <Panel>
      <PanelHeader
        icon={Crosshair}
        title="Repeat offenders"
        description={
          data
            ? `${data.bans} bans across ${data.offenders.length} of the addresses in the log${
                data.since ? `, since ${relativeTime(data.since)}` : ""
              }`
            : "Folded from fail2ban's own log"
        }
        actions={
          data && data.perDay.length > 1 ? (
            <span className="flex items-center gap-2">
              <span className="eyebrow">bans per day</span>
              <Sparkline
                values={data.perDay.map((d) => d.count)}
                color="var(--destructive)"
                label={`${data.bans} bans over ${data.perDay.length} days`}
              />
            </span>
          ) : undefined
        }
      />
      <PanelBody flush>
        {unavailable ? (
          <Notice tone="default" title="No fail2ban log on this host">
            fail2ban is not installed, or it logs only to the journal. There is no file to fold.
          </Notice>
        ) : error ? (
          <ErrorState error={error} />
        ) : loading ? (
          <LoadingPanel rows={4} />
        ) : !data?.offenders.length ? (
          <EmptyState icon={Crosshair} title="Nothing has been banned yet" />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-full">Address</TableHead>
                <TableHead className="w-20">Bans</TableHead>
                <TableHead>Jails</TableHead>
                <TableHead>First seen</TableHead>
                <TableHead>Last seen</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.offenders.map((offender) => (
                <TableRow key={offender.ip} className="group">
                  <TableCell className="font-mono text-xs">{offender.ip}</TableCell>
                  <TableCell>
                    <Badge
                      variant={offender.bans >= 5 ? "destructive" : "secondary"}
                      className="numeric font-normal"
                    >
                      {offender.bans}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {offender.jails.join(", ")}
                  </TableCell>
                  <TableCell className="text-xs whitespace-nowrap text-muted-foreground">
                    {relativeTime(offender.first)}
                  </TableCell>
                  <TableCell className="text-xs whitespace-nowrap text-muted-foreground">
                    {relativeTime(offender.last)}
                  </TableCell>
                  <TableCell>
                    {can("system.admin") && (
                      <Button
                        size="xs"
                        variant="ghost"
                        className="opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                        onClick={() => block(offender.ip)}
                        title="Add a permanent firewall deny for this address"
                      >
                        <ShieldPlus className="size-3.5" />
                        Block
                      </Button>
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
