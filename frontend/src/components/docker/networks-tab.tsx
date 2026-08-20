"use client"

import { Network as NetworkIcon, Trash2 } from "lucide-react"
import { del, get } from "@/lib/api"
import type { DockerNetwork } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { IconAction } from "@/components/icon-action"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import type { ConfirmFn } from "@/components/docker/shared"
import { Badge } from "@/components/ui/badge"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export function NetworksTab({ confirm }: { confirm: ConfirmFn }) {
  const { can } = useAuth()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<DockerNetwork[]>("/docker/networks/", undefined, signal),
    30000,
  )
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  return (
    <Panel>
      <PanelHeader
        icon={NetworkIcon}
        title="Networks"
        description={`${data?.length ?? 0} defined on this daemon`}
      />
      <PanelBody flush>
        <Table containerClassName="max-h-[calc(100svh-24rem)]">
          <TableHeader className={stickyTableHeader}>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Driver</TableHead>
              <TableHead className="w-full">Subnets</TableHead>
              <TableHead className="text-right">Containers</TableHead>
              <TableHead className="w-px" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((network) => (
              <TableRow key={network.id} className="group">
                <TableCell className="text-[13px] font-medium">
                  {network.name}
                  {network.internal && (
                    <Badge variant="outline" className="ml-2 text-[10px] font-normal">
                      internal
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="text-xs">{network.driver}</TableCell>
                <TableCell className="font-mono text-[11px] text-muted-foreground">
                  {network.subnets.join(", ") || "—"}
                </TableCell>
                <TableCell className="numeric text-right text-xs">{network.containers}</TableCell>
                <TableCell>
                  {can("destructive") && !["bridge", "host", "none"].includes(network.name) && (
                    <IconAction
                      label="Remove"
                      className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                      onClick={() =>
                        confirm({
                          title: "Delete network",
                          phrase: network.name,
                          confirmLabel: "Delete",
                          description: (
                            <p>
                              Removes <b>{network.name}</b>.
                            </p>
                          ),
                          action: async (c) => {
                            await del(`/docker/networks/${network.id}`, { confirm: c })
                            refresh()
                          },
                        })
                      }
                    >
                      <Trash2 />
                    </IconAction>
                  )}
                </TableCell>
              </TableRow>
            ))}
            {!data?.length && (
              <TableRow>
                <TableCell colSpan={5} className="p-0">
                  <EmptyState icon={NetworkIcon} title="No networks" />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </PanelBody>
    </Panel>
  )
}
