"use client"

import { useMemo, useState } from "react"
import {
  ArrowDownToLine,
  Box,
  CheckCircle2,
  CircleHelp,
  Download,
  Hammer,
  HardDrive,
  RefreshCw,
  Trash2,
} from "lucide-react"
import { notify } from "@/lib/toast"
import { del, get, post } from "@/lib/api"
import { bytes, relativeTime } from "@/lib/format"
import type { DockerImage, ImageDetail, ImageUpdateStatus } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingPanel, LoadingRows, Notice, Spinner } from "@/components/state"
import { IconAction } from "@/components/icon-action"
import { Panel, PanelBody, PanelHeader, PanelToolbar, Well } from "@/components/panel"
import { SidePanel } from "@/components/side-panel"
import { Detail, DetailList, RowLink, SearchInput } from "@/components/page"
import type { ConfirmFn } from "@/components/docker/shared"
import { Hint, Term } from "@/components/docker/explain"
import { usePullProgress } from "@/components/docker/create-container"
import { BuildDialog } from "@/components/docker/build-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
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

function primaryTag(image: DockerImage): string | undefined {
  const tag = image.repoTags.find((t) => t && t !== "<none>:<none>")
  return tag
}

export function ImagesTab({ confirm }: { confirm: ConfirmFn }) {
  const { can } = useAuth()
  const [filter, setFilter] = useState("")
  const [selected, setSelected] = useState<string | null>(null)
  // null is closed; a string (possibly empty) opens the dialog seeded with it.
  const [pulling, setPulling] = useState<string | null>(null)
  const [building, setBuilding] = useState(false)

  const { data, error, loading, refresh } = usePoll(
    (signal) => get<DockerImage[]>("/docker/images/", undefined, signal),
    30000,
  )

  /**
   * Whether the tags in use still point where they did when they were pulled.
   *
   * The one question a self-hoster actually has about an image, and the one no
   * free tool in this class answers: Watchtower answers it and then restarts
   * things unasked, Portainer's version is a paid feature. It is one registry
   * request per tag, cached for half an hour on the server, and it only covers
   * images containers are running — a stale layer nothing references is not
   * news.
   */
  const updates = usePoll<Record<string, ImageUpdateStatus>>(
    (signal) => get<Record<string, ImageUpdateStatus>>("/docker/images/updates", undefined, signal),
    // Slow: the answer changes when a publisher pushes, not when a page
    // refreshes, and Docker Hub rate-limits by address.
    15 * 60_000,
  )
  const [checking, setChecking] = useState(false)

  const recheck = async () => {
    setChecking(true)
    try {
      await get("/docker/images/updates", { refresh: true })
      updates.refresh()
      notify.success("Checked with the registries")
    } catch (err) {
      notify.error("Could not check for updates", err)
    } finally {
      setChecking(false)
    }
  }

  const visible = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    if (!needle) return data ?? []
    return (data ?? []).filter(
      (i) =>
        i.repoTags.some((t) => t.toLowerCase().includes(needle)) ||
        i.id.toLowerCase().includes(needle),
    )
  }, [data, filter])

  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  const total = data?.reduce((s, i) => s + i.size, 0) ?? 0
  const outdated = Object.values(updates.data ?? {}).filter((u) => u.state === "outdated")

  return (
    <div className="space-y-4">
      {outdated.length > 0 && (
        <Notice
          title={`${outdated.length} running image${outdated.length === 1 ? " has" : "s have"} a newer version`}
          icon={ArrowDownToLine}
        >
          <p>
            {outdated.map((u) => u.ref).join(", ")} — the tag now points somewhere else in the
            registry. Pulling moves this server to it; the containers using it keep running the old
            copy until they are recreated.
          </p>
        </Notice>
      )}

      <Panel>
        <PanelHeader
          icon={Box}
          title="Images"
          description={`${data?.length ?? 0} images · ${bytes(total)} on disk`}
          actions={
            <>
              <IconAction
                label="Check the registries for newer versions"
                onClick={recheck}
                disabled={checking}
              >
                {checking ? <Spinner /> : <RefreshCw />}
              </IconAction>
              {can("service.control") && (
                <>
                  <Button size="sm" variant="outline" onClick={() => setPulling("")}>
                    <Download className="size-4" />
                    Pull
                  </Button>
                  {/* The link to the git panel: a repository the dashboard
                      already pulls is a build context. */}
                  <Button size="sm" variant="outline" onClick={() => setBuilding(true)}>
                    <Hammer className="size-4" />
                    Build
                  </Button>
                </>
              )}
              {can("destructive") && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() =>
                    confirm({
                      title: "Prune images",
                      confirmLabel: "Prune",
                      description: (
                        <p>
                          Removes <Term name="dangling">dangling images</Term> — layers left behind
                          when a tag moved to a newer copy. Nothing references them and no container
                          can need them.
                        </p>
                      ),
                      action: async () => {
                        const rep = await post<{ spaceReclaimed: number }>("/docker/images/prune")
                        notify.success(`Reclaimed ${bytes(rep.spaceReclaimed)}`)
                        refresh()
                      },
                    })
                  }
                >
                  <Trash2 className="size-4" />
                  Prune dangling
                </Button>
              )}
            </>
          }
        />
        <PanelToolbar>
          <SearchInput
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter by name or id"
          />
        </PanelToolbar>
        <PanelBody flush>
          <Table containerClassName="max-h-[calc(100svh-26rem)]">
            <TableHeader className={stickyTableHeader}>
              <TableRow>
                <TableHead className="w-full">Repository</TableHead>
                <TableHead>Version</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className="text-right">Used by</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((image) => {
                const tag = primaryTag(image)
                const update = tag ? updates.data?.[tag] : undefined
                return (
                  <TableRow
                    key={image.id}
                    className="group"
                    onActivate={() => setSelected(image.id)}
                  >
                    <TableCell>
                      <div className="max-w-[26rem] min-w-0">
                        <RowLink mono onClick={() => setSelected(image.id)}>
                          {image.repoTags.length ? image.repoTags.join(", ") : <em>untagged</em>}
                        </RowLink>
                        <p className="font-mono text-[11px] text-muted-foreground">
                          {image.id.replace("sha256:", "").slice(0, 12)}
                        </p>
                      </div>
                    </TableCell>
                    <TableCell>
                      <UpdateBadge status={update} dangling={image.dangling} />
                    </TableCell>
                    <TableCell className="numeric text-right font-mono text-xs">
                      {bytes(image.size)}
                    </TableCell>
                    <TableCell className="numeric text-right text-xs">
                      {image.containers > 0 ? (
                        image.containers
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {relativeTime(image.created)}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
                        {can("service.control") && tag && (
                          <IconAction label={`Pull a fresh ${tag}`} onClick={() => setPulling(tag)}>
                            <Download />
                          </IconAction>
                        )}
                        {can("destructive") && (
                          <IconAction
                            label="Remove"
                            className="text-destructive"
                            onClick={() =>
                              confirm({
                                title: "Delete image",
                                confirmLabel: "Delete",
                                description: (
                                  <>
                                    <p>
                                      Deletes <b>{imagePhrase(image)}</b>.
                                    </p>
                                    <p>
                                      {image.containers > 0
                                        ? `${image.containers} container(s) were created from it — they keep running, but will not start again without pulling it back.`
                                        : "No container is using it. Anything that needs it later will have to pull it again."}
                                    </p>
                                  </>
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
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
              {visible.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="p-0">
                    <EmptyState icon={Box} title={filter ? "Nothing matches" : "No images"} />
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </PanelBody>
      </Panel>

      <ImageDetailPanel imageId={selected} onOpenChange={(o) => !o && setSelected(null)} />
      <BuildDialog open={building} onOpenChange={setBuilding} onBuilt={refresh} />
      <PullDialog
        open={pulling !== null}
        initial={pulling ?? ""}
        onClose={() => setPulling(null)}
        onDone={() => {
          refresh()
          updates.refresh()
        }}
      />
    </div>
  )
}

/** The version column: what the registry says about this tag now. */
function UpdateBadge({
  status,
  dangling,
}: {
  status: ImageUpdateStatus | undefined
  dangling: boolean
}) {
  if (dangling) {
    return (
      <Badge variant="secondary" className="font-normal">
        dangling
      </Badge>
    )
  }
  if (!status) return <span className="text-[11px] text-muted-foreground">—</span>

  if (status.state === "outdated") {
    return (
      <Badge variant="warning" className="font-normal">
        update available
      </Badge>
    )
  }
  if (status.state === "current") {
    return (
      <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <CheckCircle2 className="size-3 text-success" />
        current
      </span>
    )
  }
  // "local" and "unknown" both mean "no answer", and the difference between
  // them is the whole point of saying which.
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="flex cursor-help items-center gap-1.5 text-[11px] text-muted-foreground">
          <CircleHelp className="size-3" />
          {status.state === "local" ? "built here" : "not checked"}
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">{status.reason}</TooltipContent>
    </Tooltip>
  )
}

/* ---------------------------------------------------------------- detail -- */

function ImageDetailPanel({
  imageId,
  onOpenChange,
}: {
  imageId: string | null
  onOpenChange: (open: boolean) => void
}) {
  const { data, error, loading } = usePoll<ImageDetail>(
    (signal) =>
      get<ImageDetail>(`/docker/images/${encodeURIComponent(imageId ?? "")}`, undefined, signal),
    0,
    [imageId],
    { enabled: imageId !== null },
  )

  return (
    <SidePanel
      open={imageId !== null}
      onOpenChange={onOpenChange}
      icon={Box}
      title={data?.repoTags[0] ?? "Image"}
      description={data?.id.replace("sha256:", "").slice(0, 24)}
    >
      {error && <ErrorState error={error} />}
      {loading && !data && <LoadingRows />}
      {data && (
        <div className="space-y-5">
          <DetailList>
            <Detail label="Size">{bytes(data.size)}</Detail>
            <Detail label="Created">{relativeTime(data.created)}</Detail>
            <Detail label="Platform">
              {data.os ?? "?"}/{data.architecture ?? "?"}
            </Detail>
            <Detail label="Runs as">{data.user || "root"}</Detail>
            <Detail label="Working dir">{data.workingDir || "/"}</Detail>
            <Detail label="Entrypoint">
              <span className="font-mono break-all">
                {data.entrypoint.length ? data.entrypoint.join(" ") : "—"}
              </span>
            </Detail>
            <Detail label="Command">
              <span className="font-mono break-all">
                {data.command.length ? data.command.join(" ") : "—"}
              </span>
            </Detail>
          </DetailList>

          {data.exposedPorts.length > 0 && (
            <section className="space-y-1.5">
              <p className="eyebrow">Ports the image expects to serve on</p>
              <div className="flex flex-wrap gap-1.5">
                {data.exposedPorts.map((p) => (
                  <Badge key={p} variant="outline" className="font-mono text-[10px] font-normal">
                    {p}
                  </Badge>
                ))}
              </div>
              <Hint>
                Publishing one of these is what makes the service reachable. Nothing is published
                automatically.
              </Hint>
            </section>
          )}

          {data.volumePaths.length > 0 && (
            <section className="space-y-1.5">
              <p className="eyebrow">Paths the image expects storage at</p>
              <div className="flex flex-wrap gap-1.5">
                {data.volumePaths.map((p) => (
                  <Badge key={p} variant="outline" className="font-mono text-[10px] font-normal">
                    {p}
                  </Badge>
                ))}
              </div>
              <Hint>
                Without a <Term name="volume">volume</Term> mounted here, Docker creates an unnamed
                one — the data survives, under a name nobody will recognise later.
              </Hint>
            </section>
          )}

          <section className="space-y-1.5">
            <p className="eyebrow">Used by</p>
            {data.usedBy.length === 0 ? (
              <Hint>
                No container was created from this image. Safe to delete unless you are keeping it
                deliberately.
              </Hint>
            ) : (
              <div className="space-y-1">
                {data.usedBy.map((c) => (
                  <div key={c.id} className="flex items-center justify-between gap-2 text-xs">
                    <span className="truncate">{c.name}</span>
                    <Badge
                      variant={c.state === "running" ? "success" : "secondary"}
                      className="font-normal"
                    >
                      {c.state}
                    </Badge>
                  </div>
                ))}
              </div>
            )}
          </section>

          <section className="space-y-1.5">
            <p className="eyebrow">
              <Term name="image_layer">Layers</Term>
            </p>
            <Hint>
              Read bottom-up: each line is an instruction from the Dockerfile that built it. The
              large ones are where the size went.
            </Hint>
            <div className="space-y-0.5">
              {data.layers.map((layer, i) => (
                <div
                  key={`${layer.id}-${i}`}
                  className="flex items-start gap-2 rounded-md px-2 py-1 font-mono text-[11px] hover:bg-[var(--row-hover)]"
                >
                  <span className="w-16 shrink-0 text-right text-muted-foreground">
                    {layer.size > 0 ? bytes(layer.size, 0) : "—"}
                  </span>
                  <span className="min-w-0 flex-1 break-all">
                    {layer.createdBy || layer.comment}
                  </span>
                </div>
              ))}
            </div>
          </section>

          {data.env.length > 0 && (
            <section className="space-y-1.5">
              <p className="eyebrow">Environment baked into the image</p>
              <Well className="max-h-48">{data.env.join("\n")}</Well>
            </section>
          )}
        </div>
      )}
    </SidePanel>
  )
}

/* ------------------------------------------------------------------ pull -- */

/**
 * A pull, with the layer progress a pull actually has.
 *
 * Downloading a gigabyte behind a spinner is indistinguishable from a hang,
 * and it is the difference between "this is slow" and "this is broken". The
 * socket has existed on the backend all along; nothing used it.
 */
function PullDialog({
  open,
  initial,
  onClose,
  onDone,
}: {
  open: boolean
  initial: string
  onClose: () => void
  onDone: () => void
}) {
  const [ref, setRef] = useState(initial)
  const pull = usePullProgress()

  // The dialog is keyed on `initial` by its parent, so this runs once per
  // opening rather than fighting the input on every render.
  const [seeded, setSeeded] = useState(initial)
  if (seeded !== initial) {
    setSeeded(initial)
    setRef(initial)
  }

  const start = async () => {
    const ok = await pull.pull(ref)
    if (ok) {
      notify.success(`${ref} pulled`)
      onDone()
    } else {
      notify.error(`Could not pull ${ref}`)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // Never close mid-pull: the socket is what drives it, and unmounting
        // the dialog would abandon a download in progress with no way back to
        // its output.
        if (pull.active || next) return
        pull.reset()
        onClose()
      }}
    >
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Download className="size-4" />
            Pull an image
          </DialogTitle>
          <DialogDescription>
            Downloads it to this server. Containers already running an older copy keep running it
            until they are recreated.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          <Input
            value={ref}
            spellCheck={false}
            placeholder="nginx:alpine"
            className="font-mono"
            onChange={(e) => setRef(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && ref.trim() && !pull.active && start()}
          />
          <Hint>
            Leave the version off and you get <span className="font-mono">latest</span>, which is{" "}
            <Term name="tag">whatever the publisher last pushed</Term>.
          </Hint>
        </div>
        {(pull.active || pull.lines.length > 0) && (
          <Well className="max-h-56">
            {pull.lines.length === 0 ? "Starting…" : pull.lines.join("\n")}
          </Well>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={pull.active}>
            Close
          </Button>
          <Button onClick={start} disabled={!ref.trim() || pull.active}>
            {pull.active ? (
              <Spinner className="size-4" />
            ) : (
              <HardDrive className="size-4" />
            )}
            Pull
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
