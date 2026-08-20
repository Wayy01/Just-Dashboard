"use client"

import { HardDrive, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes, truncateMiddle } from "@/lib/format"
import type { DockerVolume } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { IconAction } from "@/components/icon-action"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import type { ConfirmFn } from "@/components/docker/shared"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export function VolumesTab({ confirm }: { confirm: ConfirmFn }) {
  const { can } = useAuth()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<DockerVolume[]>("/docker/volumes/", undefined, signal),
    30000,
  )
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  return (
    <Panel>
      <PanelHeader
        icon={HardDrive}
        title="Volumes"
        description="Volumes not in use can be reclaimed"
        actions={
          can("destructive") && (
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                confirm({
                  title: "Prune volumes",
                  phrase: "prune volumes",
                  confirmLabel: "Prune",
                  description: (
                    <p className="text-destructive">
                      Deletes every volume no container is using, including named ones. The data in
                      them is gone permanently.
                    </p>
                  ),
                  action: async (c) => {
                    const rep = await post<{ spaceReclaimed: number }>(
                      "/docker/volumes/prune",
                      undefined,
                      { confirm: c },
                    )
                    toast.success(`Reclaimed ${bytes(rep.spaceReclaimed)}`)
                    refresh()
                  },
                })
              }
            >
              <Trash2 className="size-4" />
              Prune unused
            </Button>
          )
        }
      />
      <PanelBody flush>
        <Table containerClassName="max-h-[calc(100svh-24rem)]">
          <TableHeader className={stickyTableHeader}>
            <TableRow>
              <TableHead className="w-full">Name</TableHead>
              <TableHead>Driver</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead>In use</TableHead>
              <TableHead className="w-px" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((volume) => (
              <TableRow key={volume.name} className="group">
                <TableCell>
                  <div className="max-w-[24rem] min-w-0">
                    <div className="font-mono text-xs">{truncateMiddle(volume.name, 40)}</div>
                    <p className="truncate font-mono text-[11px] text-muted-foreground">
                      {volume.mountpoint}
                    </p>
                  </div>
                </TableCell>
                <TableCell className="text-xs">{volume.driver}</TableCell>
                <TableCell className="numeric text-right font-mono text-xs">
                  {volume.size ? bytes(volume.size) : "—"}
                </TableCell>
                <TableCell>
                  {volume.refCount < 0 ? (
                    <span className="text-xs text-muted-foreground">unknown</span>
                  ) : (
                    <Badge variant={volume.inUse ? "success" : "secondary"} className="font-normal">
                      {volume.inUse ? `${volume.refCount} container(s)` : "unused"}
                    </Badge>
                  )}
                </TableCell>
                <TableCell>
                  {can("destructive") && (
                    <IconAction
                      label="Remove"
                      className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                      onClick={() =>
                        confirm({
                          title: "Delete volume",
                          phrase: volume.name,
                          confirmLabel: "Delete",
                          description: (
                            <p className="text-destructive">
                              Everything stored in <b>{volume.name}</b> is destroyed permanently.
                            </p>
                          ),
                          action: async (c) => {
                            await del(`/docker/volumes/${encodeURIComponent(volume.name)}`, {
                              confirm: c,
                            })
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
                  <EmptyState icon={HardDrive} title="No volumes" />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </PanelBody>
    </Panel>
  )
}
