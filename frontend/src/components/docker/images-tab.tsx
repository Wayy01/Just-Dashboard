"use client"

import { Box, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes, relativeTime } from "@/lib/format"
import type { DockerImage } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { IconAction } from "@/components/icon-action"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import type { ConfirmFn } from "@/components/docker/shared"
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
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  const total = data?.reduce((s, i) => s + i.size, 0) ?? 0

  return (
    <Panel>
      <PanelHeader
        icon={Box}
        title="Images"
        description={`${data?.length ?? 0} images · ${bytes(total)} on disk`}
        actions={
          can("destructive") && (
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
          )
        }
      />
      <PanelBody flush>
        <Table containerClassName="max-h-[calc(100svh-24rem)]">
          <TableHeader className={stickyTableHeader}>
            <TableRow>
              <TableHead className="w-full">Repository</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead className="text-right">Containers</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="w-px" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((image) => (
              <TableRow key={image.id} className="group">
                <TableCell>
                  <div className="max-w-[26rem] min-w-0">
                    <div className="truncate font-mono text-xs">
                      {image.repoTags.length ? image.repoTags.join(", ") : <em>untagged</em>}
                    </div>
                    <p className="font-mono text-[11px] text-muted-foreground">
                      {image.id.replace("sha256:", "").slice(0, 12)}
                    </p>
                  </div>
                </TableCell>
                <TableCell className="numeric text-right font-mono text-xs">
                  {bytes(image.size)}
                </TableCell>
                <TableCell className="numeric text-right text-xs">{image.containers}</TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {relativeTime(image.created)}
                </TableCell>
                <TableCell>
                  {can("destructive") && (
                    <IconAction
                      label="Remove"
                      className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                      onClick={() =>
                        confirm({
                          title: "Delete image",
                          phrase: imagePhrase(image),
                          confirmLabel: "Delete",
                          description: (
                            <p>
                              Deletes <b>{imagePhrase(image)}</b>. Any container that needs it will
                              have to pull it again.
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
                  <EmptyState icon={Box} title="No images" />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </PanelBody>
    </Panel>
  )
}
