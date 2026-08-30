"use client"

import { NetworkDevice, ShieldCheck } from "@/components/icons"
import { notify } from "@/lib/toast"
import { get, post } from "@/lib/api"
import type { Connections } from "@/lib/types"
import { useViewState } from "@/lib/view-state"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { Metric, MetricStrip } from "@/components/page"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { ReachBadge } from "@/components/security/reach-badge"
import { Status } from "@/components/status-dot"
import { Button } from "@/components/ui/button"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/**
 * Who is talking to this machine right now.
 *
 * The ports view answers what is listening; this answers who took it up on the
 * offer, which is the question during an incident. Folded by remote address
 * rather than listed one socket per row: a busy host holds thousands, and
 * forty of them are one client — a raw table buries the single address with
 * two hundred connections underneath four hundred rows of noise.
 */
export function ConnectionsPanel() {
  const { can } = useAuth()
  const [scope, setScope] = useViewState<"all" | "public">("security.connections.scope", "all")
  const { data, error, loading, refresh } = usePoll<Connections>(
    (signal) => get("/connections", undefined, signal),
    10000,
  )

  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  const peers = (data?.peers ?? []).filter((p) => scope === "all" || !p.private)
  const fromInternet = (data?.peers ?? []).filter((p) => !p.private).length

  const block = async (ip: string) => {
    try {
      await post("/firewall/rules", {
        action: "deny",
        direction: "in",
        from: ip,
        comment: "blocked from connections",
      })
      notify.success(`${ip} blocked`)
      refresh()
    } catch (err) {
      notify.error("Could not add the rule", err)
    }
  }

  return (
    <Panel>
      <PanelHeader
        icon={NetworkDevice}
        title="Live connections"
        description="Who is talking to this machine right now"
        actions={
          <Status
            verdict={fromInternet > 0 ? "notice" : "ok"}
            label={
              fromInternet > 0
                ? `${fromInternet} from the internet`
                : "none from the internet"
            }
          />
        }
      />
      <PanelToolbar>
        <MetricStrip>
          <Metric label="remote addresses" value={data?.peers.length ?? 0} />
          <Metric label="sockets" value={data?.total ?? 0} />
          <Metric label="listening" value={data?.listening ?? 0} />
          <Metric label="loopback" value={data?.loopback ?? 0} hint="never left the machine" />
        </MetricStrip>
      </PanelToolbar>
      <PanelToolbar>
        <ToggleGroup
          type="single"
          value={scope}
          onValueChange={(next) => next && setScope(next as "all" | "public")}
          variant="outline"
          size="sm"
          aria-label="Which peers to show"
        >
          <ToggleGroupItem value="all" className="px-2.5 text-[11px]">
            Everything
          </ToggleGroupItem>
          <ToggleGroupItem value="public" className="px-2.5 text-[11px]">
            From the internet
          </ToggleGroupItem>
        </ToggleGroup>
        <span className="text-[11px] text-muted-foreground">
          Most of a healthy host&rsquo;s connections are private, which is what makes the public
          ones worth looking at.
        </span>
      </PanelToolbar>
      <PanelBody flush>
        {peers.length === 0 ? (
          <EmptyState
            icon={NetworkDevice}
            title={scope === "public" ? "Nothing connected from the internet" : "No connections"}
          />
        ) : (
          <Table containerClassName="max-h-[calc(100svh-26rem)]">
            <TableHeader className={stickyTableHeader}>
              <TableRow>
                <TableHead className="w-full">Remote address</TableHead>
                <TableHead>Sockets</TableHead>
                <TableHead>Reaching</TableHead>
                <TableHead>Process</TableHead>
                <TableHead>Origin</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {peers.map((peer) => (
                <TableRow key={peer.address} className="group">
                  <TableCell className="font-mono text-xs">{peer.address}</TableCell>
                  <TableCell className="numeric text-xs">
                    {peer.established}
                    {peer.count !== peer.established && (
                      <span className="text-muted-foreground"> / {peer.count}</span>
                    )}
                  </TableCell>
                  <TableCell className="text-xs">
                    <span className="font-mono">{peer.ports.slice(0, 4).join(", ")}</span>
                    {peer.service && (
                      <span className="ml-1.5 text-muted-foreground">{peer.service}</span>
                    )}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {peer.processes.join(", ") || "—"}
                  </TableCell>
                  <TableCell>
                    <ReachBadge scope={peer.private ? "private" : "internet"} />
                  </TableCell>
                  <TableCell>
                    {can("system.admin") && !peer.private && (
                      <Button
                        size="xs"
                        variant="ghost"
                        className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                        onClick={() => block(peer.address)}
                      >
                        <ShieldCheck className="size-3.5" />
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
