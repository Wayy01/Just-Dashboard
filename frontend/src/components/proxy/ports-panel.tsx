"use client"

import { Router } from "@/components/icons"
import { get } from "@/lib/api"
import type { Listener } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { ErrorState, LoadingPanel } from "@/components/state"
import { Status } from "@/components/status-dot"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/** Every listening socket on the host, and whether it faces off the machine. */
export function PortsPanel() {
  const { data, error, loading } = usePoll(
    (signal) => get<Listener[]>("/ports", undefined, signal),
    15000,
  )
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  const exposed = data?.filter((l) => l.exposed).length ?? 0

  return (
    <Panel>
      <PanelHeader
        icon={Router}
        title="Listening ports"
        description={`${exposed} of ${data?.length ?? 0} bound to a wildcard address and therefore reachable from off the machine`}
      />
      <PanelBody flush>
        <Table containerClassName="max-h-[calc(100svh-20rem)]">
          <TableHeader className={stickyTableHeader}>
            <TableRow>
              <TableHead className="w-20">Port</TableHead>
              <TableHead className="w-20">Proto</TableHead>
              <TableHead>Bound to</TableHead>
              <TableHead className="w-full">Process</TableHead>
              <TableHead>User</TableHead>
              <TableHead>Reach</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((listener, i) => (
              <TableRow key={`${listener.protocol}-${listener.address}-${listener.port}-${i}`}>
                <TableCell className="numeric font-mono text-[13px]">{listener.port}</TableCell>
                <TableCell className="text-xs uppercase text-muted-foreground">
                  {listener.protocol}
                </TableCell>
                <TableCell className="font-mono text-xs">{listener.address || "*"}</TableCell>
                <TableCell>
                  <div className="max-w-[22rem] min-w-0">
                    <div className="truncate text-[13px]">{listener.process || "unknown"}</div>
                    <p className="truncate font-mono text-[11px] text-muted-foreground">
                      {listener.cmdline}
                    </p>
                  </div>
                </TableCell>
                <TableCell className="text-xs">{listener.user ?? "—"}</TableCell>
                <TableCell>
                  {listener.exposed ? (
                    <Status verdict="warning" label="exposed" icon={Router} />
                  ) : (
                    <span className="text-xs text-muted-foreground">loopback</span>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </PanelBody>
    </Panel>
  )
}
