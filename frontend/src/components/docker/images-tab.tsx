"use client"

import { Box, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes, relativeTime } from "@/lib/format"
import type { DockerImage } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { IconActionButton, type ConfirmFn } from "@/components/docker/shared"
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

/**
 * What an operator types to confirm removing an image, and what the dialog
 * names it. It has to be the image's own tag rather than a fixed phrase — the
 * server keys the confirmation on the same value, for the reason it keys a
 * container's on its name: a phrase that is the same for every row can be
 * typed from muscle memory into the wrong dialog.
 */
function imagePhrase(image: DockerImage): string {
  const tag = image.repoTags[0]
  if (tag && tag !== "<none>:<none>") return tag
  return image.id.replace(/^sha256:/, "").slice(0, 12)
}

export function ImagesTab({ confirm }: { confirm: ConfirmFn }) {
  const { can } = useAuth()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<DockerImage[]>("/docker/images/", undefined, signal),
    30000,
  )
  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />

  const total = data?.reduce((s, i) => s + i.size, 0) ?? 0

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-base">Images</CardTitle>
          <CardDescription>{bytes(total)} on disk</CardDescription>
        </div>
        {can("destructive") && (
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              confirm({
                title: "Prune images",
                phrase: "prune images",
                confirmLabel: "Prune",
                description: <p>Removes dangling images that no container references.</p>,
                action: async (c) => {
                  const rep = await post<{ spaceReclaimed: number }>(
                    "/docker/images/prune",
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
            Prune dangling
          </Button>
        )}
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Repository</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead className="text-right">Containers</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="w-px" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((image) => (
              <TableRow key={image.id}>
                <TableCell>
                  <div className="font-mono text-xs">
                    {image.repoTags.length ? image.repoTags.join(", ") : <em>untagged</em>}
                  </div>
                  <p className="font-mono text-[11px] text-muted-foreground">
                    {image.id.replace("sha256:", "").slice(0, 12)}
                  </p>
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  {bytes(image.size)}
                </TableCell>
                <TableCell className="text-right text-xs tabular-nums">
                  {image.containers}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {relativeTime(image.created)}
                </TableCell>
                <TableCell>
                  {can("destructive") && (
                    <IconActionButton
                      title="Remove"
                      icon={Trash2}
                      destructive
                      onClick={() =>
                        confirm({
                          title: "Delete image",
                          phrase: imagePhrase(image),
                          confirmLabel: "Delete",
                          description: (
                            <p>
                              Deletes <b>{imagePhrase(image)}</b>. Any
                              container that needs it will have to pull it again.
                            </p>
                          ),
                          action: async (c) => {
                            await del(`/docker/images/${encodeURIComponent(image.id)}`, {
                              confirm: c,
                              query: { force: true },
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
                  <EmptyState icon={Box} title="No images" />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
