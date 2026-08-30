"use client"

import { Connection, Route, Servers } from "@/components/icons"
import { get } from "@/lib/api"
import { bytes } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { NetworkInfo } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { Metric, MetricStrip } from "@/components/page"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { ErrorState, LoadingPanel } from "@/components/state"
import { ReachBadge } from "@/components/security/reach-badge"
import { Status } from "@/components/status-dot"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/**
 * The shape of the machine's network, which is what "exposed" means.
 *
 * An address on tailscale0 and the same address on eth0 are completely
 * different security propositions, and until the interfaces are on screen the
 * operator has to take the dashboard's word for which one they have. The
 * routing table answers the other half: which interface carries the default
 * route is what "the internet reaches this box here" means.
 */
export function NetworkPanel() {
  const { data, error, loading } = usePoll<NetworkInfo>(
    (signal) => get("/network", undefined, signal),
    60000,
  )
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />
  if (!data) return null

  const real = data.interfaces.filter((i) => i.kind !== "virtual" && i.kind !== "bridge")
  const exposed = data.interfaces.filter((i) => i.public && i.up)
  const defaultRoute = data.routes.find((r) => r.destination === "default")

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Panel>
        <PanelHeader
          icon={Connection}
          title="Interfaces"
          description="A host running Docker has a dozen virtual devices; they are grouped so the real ones read first"
          actions={
            <Status
              verdict={exposed.length > 0 ? "warning" : "ok"}
              label={
                exposed.length > 0 ? `${exposed.length} on a public address` : "no public address"
              }
            />
          }
        />
        <PanelToolbar>
          <MetricStrip>
            <Metric
              label="devices"
              value={data.interfaces.length}
              hint={`${real.length} not virtual`}
            />
            <Metric label="up" value={data.interfaces.filter((i) => i.up).length} />
            <Metric
              label="default route"
              value={defaultRoute?.interface ?? "—"}
              hint="where the internet reaches this host"
            />
          </MetricStrip>
        </PanelToolbar>
        <PanelBody flush>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Device</TableHead>
                <TableHead className="w-full">Addresses</TableHead>
                <TableHead>MTU</TableHead>
                <TableHead>In / out</TableHead>
                <TableHead>Reach</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.interfaces.map((ifc) => (
                <TableRow key={ifc.name} className={cn(!ifc.up && "opacity-60")}>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs font-medium">{ifc.name}</span>
                      <Badge variant="secondary" className="font-normal">
                        {ifc.kind}
                      </Badge>
                    </div>
                  </TableCell>
                  <TableCell className="font-mono text-[11px]">
                    {ifc.addresses.join("  ") || "—"}
                  </TableCell>
                  <TableCell className="numeric text-xs text-muted-foreground">{ifc.mtu}</TableCell>
                  <TableCell className="numeric text-xs text-muted-foreground">
                    {bytes(ifc.bytesRecv)} / {bytes(ifc.bytesSent)}
                  </TableCell>
                  <TableCell>
                    <ReachBadge
                      scope={ifc.public ? "internet" : ifc.loopback ? "local" : "private"}
                    />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </PanelBody>
      </Panel>

      <div className="grid gap-4 lg:grid-cols-[2fr_1fr] [&>*]:min-w-0">
        <Panel>
          <PanelHeader
            icon={Route}
            title="Routes"
            description="Which interface carries the default route is what decides where the internet reaches this host"
          />
          <PanelBody flush>
            <Table containerClassName="max-h-[22rem]">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-full">Destination</TableHead>
                  <TableHead>Via</TableHead>
                  <TableHead>Device</TableHead>
                  <TableHead>Metric</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.routes.map((route, i) => (
                  <TableRow key={`${route.family}-${route.destination}-${i}`}>
                    <TableCell className="font-mono text-xs">
                      {route.destination}
                      {route.destination === "default" && (
                        <Badge variant="outline" className="ml-2 font-normal">
                          {route.family}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">
                      {route.gateway || "on-link"}
                    </TableCell>
                    <TableCell className="font-mono text-[11px]">{route.interface || "—"}</TableCell>
                    <TableCell className="numeric text-[11px] text-muted-foreground">
                      {route.metric || "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader
            icon={Servers}
            title="Resolvers"
            description="A resolver you did not choose redirects every name this host looks up"
          />
          <PanelBody className="space-y-3">
            <div className="space-y-1">
              {data.resolvers.map((server) => (
                <div key={server} className="font-mono text-xs">
                  {server}
                </div>
              ))}
              {data.resolvers.length === 0 && (
                <p className="text-[13px] text-muted-foreground">None configured.</p>
              )}
            </div>
            {data.resolvers.some((s) => s.startsWith("127.0.0.53")) && (
              <p className="text-[11px] leading-relaxed text-muted-foreground">
                127.0.0.53 is systemd-resolved answering locally; the real upstream servers are the
                ones it was given.
              </p>
            )}
            {data.search.length > 0 && (
              <div>
                <p className="eyebrow mb-1">search domains</p>
                <p className="font-mono text-[11px]">{data.search.join(" ")}</p>
              </div>
            )}
          </PanelBody>
        </Panel>
      </div>
    </div>
  )
}
