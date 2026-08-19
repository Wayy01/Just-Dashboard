"use client"

import { Network as NetworkIcon, Trash2 } from "lucide-react"
import { del, get } from "@/lib/api"
import type { DockerNetwork } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { IconActionButton, type ConfirmFn } from "@/components/docker/shared"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
import {
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
  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />

  return (
    <Card>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Driver</TableHead>
              <TableHead>Subnets</TableHead>
              <TableHead className="text-right">Containers</TableHead>
              <TableHead className="w-px" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((network) => (
              <TableRow key={network.id}>
                <TableCell className="font-medium">
                  {network.name}
                  {network.internal && (
                    <Badge variant="outline" className="ml-2 text-[10px]">
                      internal
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="text-xs">{network.driver}</TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">
                  {network.subnets.join(", ") || "—"}
                </TableCell>
                <TableCell className="text-right text-xs tabular-nums">
                  {network.containers}
                </TableCell>
                <TableCell>
                  {can("destructive") && !["bridge", "host", "none"].includes(network.name) && (
                    <IconActionButton
                      title="Remove"
                      icon={Trash2}
                      destructive
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
                    />
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
      </CardContent>
    </Card>
  )
}
