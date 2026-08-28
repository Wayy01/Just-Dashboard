"use client"

import { useState } from "react"
import Link from "next/link"
import { FolderTree, HardDrive, Plus, Trash2 } from "lucide-react"
import { notify } from "@/lib/toast"
import { del, get, post } from "@/lib/api"
import { bytes, truncateMiddle } from "@/lib/format"
import type { VolumeDetail } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingPanel, LoadingRows, Spinner } from "@/components/state"
import { IconAction } from "@/components/icon-action"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { SidePanel } from "@/components/side-panel"
import { Detail, DetailList, RowLink } from "@/components/page"
import type { ConfirmFn } from "@/components/docker/shared"
import { Hint, Term } from "@/components/docker/explain"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
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
 * Volumes, with the one fact that decides every action on them: what is using
 * this, and will deleting it destroy something.
 *
 * Docker's own reference count answers for *running* containers only, so a
 * volume belonging to a stopped stack reads as unused — and that is precisely
 * the volume somebody prunes by accident, along with the only copy of whatever
 * was in it. The list joins against every container, running or not, on the
 * server rather than in the browser.
 */
export function VolumesTab({ confirm }: { confirm: ConfirmFn }) {
  const { can } = useAuth()
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  const { data, error, loading, refresh } = usePoll(
    (signal) => get<VolumeDetail[]>("/docker/volumes/", undefined, signal),
    30000,
  )
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  const unused = (data ?? []).filter((v) => !v.inUse)
  const reclaimable = unused.reduce((s, v) => s + v.size, 0)

  return (
    <div className="space-y-4">
      <Panel>
        <PanelHeader
          icon={HardDrive}
          title="Volumes"
          description={
            unused.length > 0
              ? `${data?.length ?? 0} volumes · ${unused.length} attached to nothing, holding ${bytes(reclaimable)}`
              : `${data?.length ?? 0} volumes, all in use`
          }
          actions={
            <>
              {can("service.control") && (
                <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
                  <Plus className="size-4" />
                  New volume
                </Button>
              )}
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
                        <>
                          <p className="text-destructive">
                            Deletes every volume no <em>running</em> container is using, including
                            named ones. The data in them is gone permanently.
                          </p>
                          <p>
                            That is Docker&apos;s definition, not this dashboard&apos;s: a volume
                            belonging to a stack that is merely stopped counts as unused and will be
                            destroyed. Check the list first — the ones at risk are the rows marked
                            &ldquo;unused&rdquo;.
                          </p>
                        </>
                      ),
                      action: async (c) => {
                        const rep = await post<{ spaceReclaimed: number }>(
                          "/docker/volumes/prune",
                          undefined,
                          { confirm: c },
                        )
                        notify.success(`Reclaimed ${bytes(rep.spaceReclaimed)}`)
                        refresh()
                      },
                    })
                  }
                >
                  <Trash2 className="size-4" />
                  Prune unused
                </Button>
              )}
            </>
          }
        />
        <PanelBody flush>
          <Table containerClassName="max-h-[calc(100svh-24rem)]">
            <TableHeader className={stickyTableHeader}>
              <TableRow>
                <TableHead className="w-full">Name</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead>Used by</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {data?.map((volume) => (
                <TableRow
                  key={volume.name}
                  className="group"
                  onActivate={() => setSelected(volume.name)}
                >
                  <TableCell>
                    <div className="max-w-[24rem] min-w-0">
                      <RowLink mono onClick={() => setSelected(volume.name)}>
                        {truncateMiddle(volume.name, 40)}
                      </RowLink>
                      <p className="truncate font-mono text-[11px] text-muted-foreground">
                        {volume.mountpoint}
                      </p>
                    </div>
                  </TableCell>
                  <TableCell className="numeric text-right font-mono text-xs">
                    {volume.size ? bytes(volume.size) : "—"}
                  </TableCell>
                  <TableCell>
                    <UsedByCell volume={volume} />
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
                              <>
                                <p className="text-destructive">
                                  Everything stored in <b>{volume.name}</b> is destroyed
                                  permanently.
                                </p>
                                {volume.usedBy.length > 0 && (
                                  <p>
                                    {volume.usedBy.length} container(s) mount it:{" "}
                                    {volume.usedBy.map((u) => u.name).join(", ")}.
                                  </p>
                                )}
                              </>
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
                  <TableCell colSpan={4} className="p-0">
                    <EmptyState
                      icon={HardDrive}
                      title="No volumes"
                      description={
                        <>
                          A <Term name="volume">volume</Term> is storage Docker manages, kept
                          outside a container so it survives being recreated.
                        </>
                      }
                    />
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </PanelBody>
      </Panel>

      <VolumeDetailPanel name={selected} onOpenChange={(o) => !o && setSelected(null)} />
      <NewVolumeDialog open={creating} onOpenChange={setCreating} onCreated={refresh} />
    </div>
  )
}

function UsedByCell({ volume }: { volume: VolumeDetail }) {
  if (volume.usedBy.length === 0) {
    return (
      <Badge variant="secondary" className="font-normal">
        unused
      </Badge>
    )
  }
  const running = volume.usedBy.filter((u) => u.state === "running").length
  return (
    <span className="flex flex-wrap items-center gap-1">
      <Badge variant={running > 0 ? "success" : "warning"} className="font-normal">
        {volume.usedBy.length} container{volume.usedBy.length === 1 ? "" : "s"}
      </Badge>
      {running === 0 && (
        // The row Docker's own prune would delete while calling it unused.
        <span className="text-[11px] text-muted-foreground">stopped — prune would delete this</span>
      )}
    </span>
  )
}

function VolumeDetailPanel({
  name,
  onOpenChange,
}: {
  name: string | null
  onOpenChange: (open: boolean) => void
}) {
  const { data, error, loading } = usePoll<VolumeDetail>(
    (signal) =>
      get<VolumeDetail>(`/docker/volumes/${encodeURIComponent(name ?? "")}`, undefined, signal),
    0,
    [name],
    { enabled: name !== null },
  )

  return (
    <SidePanel
      open={name !== null}
      onOpenChange={onOpenChange}
      icon={HardDrive}
      title={name ?? "Volume"}
      description={data?.mountpoint}
    >
      {error && <ErrorState error={error} />}
      {loading && !data && <LoadingRows />}
      {data && (
        <div className="space-y-5">
          <DetailList>
            <Detail label="Size">{data.size ? bytes(data.size) : "not measured"}</Detail>
            <Detail label="Driver">{data.driver}</Detail>
            <Detail label="Created">{data.createdAt || "—"}</Detail>
            <Detail label="On disk at">
              <span className="font-mono break-all">{data.mountpoint}</span>
            </Detail>
          </DetailList>

          {/*
            A volume is a directory on this server, and this dashboard has a
            file manager. Being able to look inside one — to check a backup
            landed, to read a config a container wrote — is the difference
            between a volume being an opaque handle and being storage.
          */}
          {data.mountpoint && (
            <Button size="sm" variant="outline" asChild>
              <Link href={`/files?path=${encodeURIComponent(data.mountpoint)}`}>
                <FolderTree className="size-3.5" />
                Browse its contents
              </Link>
            </Button>
          )}

          <section className="space-y-1.5">
            <p className="eyebrow">Used by</p>
            {data.usedBy.length === 0 ? (
              <Hint>
                Nothing mounts this volume. It is safe to delete only if you know what was in it — a
                volume outlives the container that created it, so this is often the data from
                something that was removed and rebuilt.
              </Hint>
            ) : (
              <div className="space-y-1">
                {data.usedBy.map((u) => (
                  <div
                    key={`${u.id}-${u.destination}`}
                    className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-hairline px-2.5 py-1.5 text-xs"
                  >
                    <span className="min-w-0">
                      <span className="truncate font-medium">{u.name}</span>
                      <span className="ml-2 font-mono text-[11px] text-muted-foreground">
                        at {u.destination}
                      </span>
                    </span>
                    <span className="flex shrink-0 gap-1">
                      {u.readOnly && (
                        <Badge variant="outline" className="font-normal">
                          read-only
                        </Badge>
                      )}
                      <Badge
                        variant={u.state === "running" ? "success" : "secondary"}
                        className="font-normal"
                      >
                        {u.state}
                      </Badge>
                    </span>
                  </div>
                ))}
              </div>
            )}
          </section>
        </div>
      )}
    </SidePanel>
  )
}

function NewVolumeDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: () => void
}) {
  const [name, setName] = useState("")
  const [busy, setBusy] = useState(false)

  const create = async () => {
    setBusy(true)
    try {
      await post("/docker/volumes/", { name })
      notify.success(`${name} created`)
      onCreated()
      onOpenChange(false)
      setName("")
    } catch (err) {
      notify.error("Could not create the volume", err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Plus className="size-4" />
            New volume
          </DialogTitle>
          <DialogDescription>
            Storage Docker manages, ready to mount into a container. Empty until something writes to
            it.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-1.5">
          <Label htmlFor="volume-name" className="text-xs">
            Name
          </Label>
          <Input
            id="volume-name"
            value={name}
            spellCheck={false}
            className="font-mono"
            placeholder="my-app-data"
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && name.trim() && create()}
          />
          <Hint>
            Name it after what will be in it. Volumes outlive the containers that use them, and in
            six months the name is all you will have to go on.
          </Hint>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={create} disabled={busy || !name.trim()}>
            {busy && <Spinner className="size-4" />}
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
