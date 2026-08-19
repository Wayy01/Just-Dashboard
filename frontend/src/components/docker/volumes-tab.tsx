"use client"

import { HardDrive, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes, truncateMiddle } from "@/lib/format"
import type { DockerVolume } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { IconActionButton, type ConfirmFn } from "@/components/docker/shared"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
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
  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-base">Volumes</CardTitle>
          <CardDescription>Volumes not in use can be reclaimed</CardDescription>
        </div>
        {can("destructive") && (
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
        )}
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Driver</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead>In use</TableHead>
              <TableHead className="w-px" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((volume) => (
              <TableRow key={volume.name}>
                <TableCell>
                  <div className="font-mono text-xs">{truncateMiddle(volume.name, 40)}</div>
                  <p className="truncate font-mono text-[11px] text-muted-foreground">
                    {volume.mountpoint}
                  </p>
                </TableCell>
                <TableCell className="text-xs">{volume.driver}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  {volume.size ? bytes(volume.size) : "—"}
                </TableCell>
                <TableCell>
                  {volume.refCount < 0 ? (
                    <span className="text-xs text-muted-foreground">unknown</span>
                  ) : (
                    <Badge variant={volume.inUse ? "default" : "secondary"}>
                      {volume.inUse ? `${volume.refCount} container(s)` : "unused"}
                    </Badge>
                  )}
                </TableCell>
                <TableCell>
                  {can("destructive") && (
                    <IconActionButton
                      title="Remove"
                      icon={Trash2}
                      destructive
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
                    />
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
      </CardContent>
    </Card>
  )
}
