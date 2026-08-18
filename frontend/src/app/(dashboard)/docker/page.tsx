"use client"

import { useCallback, useMemo, useState } from "react"
import {
  Box,
  Boxes,
  HardDrive,
  Layers,
  Network as NetworkIcon,
  Pause,
  Play,
  RotateCw,
  Square,
  Terminal as TerminalIcon,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes, percent, relativeTime, truncateMiddle } from "@/lib/format"
import type {
  ComposeStack,
  Container,
  ContainerStats,
  DockerImage,
  DockerNetwork,
  DockerVolume,
} from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { StatusBadge } from "@/components/status-dot"
import { ContainerDetailSheet } from "@/components/docker/container-detail"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export default function DockerPage() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [containers, setContainers] = useState<Container[]>([])
  const [stats, setStats] = useState<Record<string, ContainerStats>>({})
  const [socketError, setSocketError] = useState<string>()
  const [selected, setSelected] = useState<string | null>(null)
  const [filter, setFilter] = useState("")

  const ping = usePoll(
    (signal) =>
      get<{ available: boolean; error?: string; serverVersion?: string }>(
        "/docker/ping",
        undefined,
        signal,
      ),
    30000,
  )

  const onMessage = useCallback((envelope: Envelope) => {
    if (envelope.type === "containers") {
      setContainers(envelope.data as Container[])
      setSocketError(undefined)
    } else if (envelope.type === "stats") {
      const rows = envelope.data as ContainerStats[]
      setStats(Object.fromEntries(rows.map((r) => [r.id, r])))
    } else if (envelope.type === "error") {
      setSocketError(envelope.error)
    }
  }, [])

  const available = ping.data?.available ?? false
  useSocket("/docker/containers/stream", { onMessage, enabled: available })

  const act = async (container: Container, action: string, confirmText?: string) => {
    try {
      await post(`/docker/containers/${container.id}/${action}`, undefined, {
        confirm: confirmText,
      })
      toast.success(`${container.name} ${action}ed`)
    } catch (err) {
      toast.error(`Could not ${action} ${container.name}`, { description: String(err) })
      throw err
    }
  }

  const visible = useMemo(() => {
    const needle = filter.toLowerCase()
    if (!needle) return containers
    return containers.filter(
      (c) =>
        c.name.toLowerCase().includes(needle) ||
        c.image.toLowerCase().includes(needle) ||
        c.composeStack?.toLowerCase().includes(needle),
    )
  }, [containers, filter])

  if (ping.loading) return <LoadingRows rows={6} />

  if (!available) {
    return (
      <>
        <PageHeader title="Docker" />
        <EmptyState
          icon={Box}
          title="Docker is not reachable"
          description={
            ping.data?.error ??
            "The dashboard could not connect to the Docker socket. Check that the daemon is running and that this process can read /var/run/docker.sock."
          }
        />
      </>
    )
  }

  const running = containers.filter((c) => c.state === "running").length

  return (
    <>
      <PageHeader
        title="Docker"
        description={`${running} running of ${containers.length} containers · engine ${ping.data?.serverVersion ?? "unknown"}`}
        actions={
          can("destructive") && (
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                confirm({
                  title: "Prune everything",
                  phrase: "prune everything",
                  confirmLabel: "Prune",
                  description: (
                    <p>
                      Removes stopped containers, dangling images and unused networks. Volumes are
                      left alone unless you prune them from the Volumes tab.
                    </p>
                  ),
                  action: async (c) => {
                    const reports = await post<{ kind: string; spaceReclaimed: number }[]>(
                      "/docker/prune",
                      undefined,
                      { confirm: c },
                    )
                    const total = reports.reduce((s, r) => s + r.spaceReclaimed, 0)
                    toast.success(`Reclaimed ${bytes(total)}`)
                  },
                })
              }
            >
              <Trash2 className="size-4" />
              Prune
            </Button>
          )
        }
      />

      {socketError && <ErrorState error={new Error(socketError)} />}

      <Tabs defaultValue="containers">
        <TabsList>
          <TabsTrigger value="containers">Containers</TabsTrigger>
          <TabsTrigger value="stacks">Stacks</TabsTrigger>
          <TabsTrigger value="images">Images</TabsTrigger>
          <TabsTrigger value="volumes">Volumes</TabsTrigger>
          <TabsTrigger value="networks">Networks</TabsTrigger>
        </TabsList>

        <TabsContent value="containers" className="space-y-4">
          <Input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter by name, image or stack"
            className="max-w-sm"
          />
          <Card>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Container</TableHead>
                    <TableHead>Image</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead className="text-right">CPU</TableHead>
                    <TableHead className="text-right">Memory</TableHead>
                    <TableHead>Ports</TableHead>
                    <TableHead className="w-px" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visible.map((container) => {
                    const stat = stats[container.id]
                    return (
                      <TableRow
                        key={container.id}
                        className="group"
                        onActivate={() => setSelected(container.id)}
                      >
                        <TableCell>
                          <button
                            className="text-left font-medium hover:underline"
                            onClick={() => setSelected(container.id)}
                          >
                            {container.name}
                          </button>
                          {container.composeStack && (
                            <p className="text-xs text-muted-foreground">
                              <Layers className="mr-1 inline size-3" />
                              {container.composeStack}/{container.composeService}
                            </p>
                          )}
                        </TableCell>
                        <TableCell className="font-mono text-xs text-muted-foreground">
                          {truncateMiddle(container.image, 36)}
                        </TableCell>
                        <TableCell>
                          <StatusBadge state={container.state} label={container.status} />
                          {container.health && (
                            <p className="mt-1 text-xs text-muted-foreground">{container.health}</p>
                          )}
                        </TableCell>
                        <TableCell className="text-right">
                          {stat ? (
                            <div className="flex items-center justify-end gap-2">
                              <Progress
                                value={Math.min(stat.cpuPercent, 100)}
                                className="h-1 w-12"
                              />
                              <span className="w-12 font-mono text-xs tabular-nums">
                                {percent(stat.cpuPercent)}
                              </span>
                            </div>
                          ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="text-right font-mono text-xs tabular-nums">
                          {stat ? (
                            <>
                              {bytes(stat.memUsage)}
                              <span className="text-muted-foreground">
                                {" "}
                                / {bytes(stat.memLimit)}
                              </span>
                            </>
                          ) : (
                            "—"
                          )}
                        </TableCell>
                        <TableCell className="space-x-1">
                          {container.ports
                            .filter((p) => p.publicPort)
                            .slice(0, 3)
                            .map((p, i) => (
                              <Badge key={i} variant="outline" className="font-mono text-[10px]">
                                {p.publicPort}:{p.privatePort}
                              </Badge>
                            ))}
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
                            {container.state === "running" ? (
                              <>
                                {can("service.control") && (
                                  <IconAction
                                    title="Pause"
                                    icon={Pause}
                                    onClick={() => act(container, "pause")}
                                  />
                                )}
                                {can("destructive") && (
                                  <>
                                    <IconAction
                                      title="Restart"
                                      icon={RotateCw}
                                      onClick={() =>
                                        confirm({
                                          title: "Restart container",
                                          phrase: container.name,
                                          confirmLabel: "Restart",
                                          description: (
                                            <p>
                                              <b>{container.name}</b> will be stopped and started
                                              again. Anything it is serving will be interrupted.
                                            </p>
                                          ),
                                          action: (c) => act(container, "restart", c),
                                        })
                                      }
                                    />
                                    <IconAction
                                      title="Stop"
                                      icon={Square}
                                      onClick={() =>
                                        confirm({
                                          title: "Stop container",
                                          phrase: container.name,
                                          confirmLabel: "Stop",
                                          description: (
                                            <p>
                                              <b>{container.name}</b> will stop serving immediately.
                                            </p>
                                          ),
                                          action: (c) => act(container, "stop", c),
                                        })
                                      }
                                    />
                                  </>
                                )}
                              </>
                            ) : (
                              can("service.control") && (
                                <IconAction
                                  title="Start"
                                  icon={Play}
                                  onClick={() => act(container, "start")}
                                />
                              )
                            )}
                            {can("terminal") && container.state === "running" && (
                              <IconAction
                                title="Shell"
                                icon={TerminalIcon}
                                onClick={() => setSelected(container.id)}
                              />
                            )}
                            {can("destructive") && (
                              <IconAction
                                title="Remove"
                                icon={Trash2}
                                destructive
                                onClick={() =>
                                  confirm({
                                    title: "Remove container",
                                    phrase: container.name,
                                    confirmLabel: "Remove",
                                    description: (
                                      <p>
                                        <b>{container.name}</b> will be deleted. This cannot be
                                        undone; its anonymous volumes are kept.
                                      </p>
                                    ),
                                    action: async (c) => {
                                      await del(`/docker/containers/${container.id}`, {
                                        confirm: c,
                                        query: { force: true },
                                      })
                                    },
                                  })
                                }
                              />
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                  {visible.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={7} className="p-0">
                        <EmptyState
                          icon={Boxes}
                          title={filter ? "No containers match that filter" : "No containers"}
                        />
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="stacks">
          <StacksTab confirm={confirm} />
        </TabsContent>
        <TabsContent value="images">
          <ImagesTab confirm={confirm} />
        </TabsContent>
        <TabsContent value="volumes">
          <VolumesTab confirm={confirm} />
        </TabsContent>
        <TabsContent value="networks">
          <NetworksTab confirm={confirm} />
        </TabsContent>
      </Tabs>

      <ContainerDetailSheet
        containerId={selected}
        onOpenChange={(open) => !open && setSelected(null)}
      />
      {dialog}
    </>
  )
}

function IconAction({
  title,
  icon: Icon,
  onClick,
  destructive,
}: {
  title: string
  icon: React.ComponentType<{ className?: string }>
  onClick: () => void
  destructive?: boolean
}) {
  return (
    <Button
      size="icon"
      variant="ghost"
      title={title}
      aria-label={title}
      className={destructive ? "size-7 text-destructive" : "size-7"}
      onClick={onClick}
    >
      <Icon className="size-3.5" />
    </Button>
  )
}

type ConfirmFn = ReturnType<typeof useConfirm>["confirm"]

function StacksTab({ confirm }: { confirm: ConfirmFn }) {
  const { can } = useAuth()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<ComposeStack[]>("/docker/stacks/", undefined, signal),
    15000,
  )
  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />
  if (!data?.length) return <EmptyState icon={Layers} title="No compose stacks found" />

  const run = async (stack: ComposeStack, action: string, confirmText?: string) => {
    const res = await post<{ exitCode: number; output: string }>(
      `/docker/stacks/${encodeURIComponent(stack.name)}/${action}`,
      undefined,
      { confirm: confirmText },
    )
    if (res.exitCode !== 0) throw new Error(res.output.slice(-400))
    refresh()
  }

  return (
    <div className="grid items-start gap-4 lg:grid-cols-2 [&>*]:min-w-0">
      {data.map((stack) => (
        <Card key={stack.name}>
          <CardHeader className="pb-3">
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0">
                <CardTitle className="truncate text-base">{stack.name}</CardTitle>
                <CardDescription className="truncate font-mono text-xs">
                  {stack.workingDir || "location unknown"}
                </CardDescription>
              </div>
              <Badge
                variant={stack.running === stack.total && stack.total > 0 ? "default" : "secondary"}
              >
                {stack.running}/{stack.total}
              </Badge>
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="space-y-1">
              {stack.services.map((svc) => (
                <div
                  key={svc.container}
                  className="flex items-center justify-between gap-2 text-sm"
                >
                  <span className="truncate">{svc.name}</span>
                  <StatusBadge state={svc.state} />
                </div>
              ))}
            </div>
            {!stack.managed ? (
              <p className="text-xs text-muted-foreground">
                No compose file reachable from this dashboard, so this stack is read-only here.
              </p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {can("service.control") && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => run(stack, "up").catch((e) => toast.error(String(e)))}
                  >
                    <Play className="size-3.5" />
                    Up
                  </Button>
                )}
                {can("destructive") && (
                  <>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        confirm({
                          title: "Restart stack",
                          phrase: stack.name,
                          confirmLabel: "Restart",
                          description: (
                            <p>
                              Every service in <b>{stack.name}</b> restarts.
                            </p>
                          ),
                          action: (c) => run(stack, "restart", c),
                        })
                      }
                    >
                      <RotateCw className="size-3.5" />
                      Restart
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="text-destructive"
                      onClick={() =>
                        confirm({
                          title: "Take stack down",
                          phrase: stack.name,
                          confirmLabel: "Down",
                          description: (
                            <p>
                              Stops and removes every container in <b>{stack.name}</b>. Named
                              volumes survive.
                            </p>
                          ),
                          action: (c) => run(stack, "down", c),
                        })
                      }
                    >
                      <Square className="size-3.5" />
                      Down
                    </Button>
                  </>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function ImagesTab({ confirm }: { confirm: ConfirmFn }) {
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
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
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
                    <IconAction
                      title="Remove"
                      icon={Trash2}
                      destructive
                      onClick={() =>
                        confirm({
                          title: "Delete image",
                          phrase: "delete image",
                          confirmLabel: "Delete",
                          description: (
                            <p>
                              Deletes <b>{image.repoTags[0] ?? image.id.slice(7, 19)}</b>. Any
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

function VolumesTab({ confirm }: { confirm: ConfirmFn }) {
  const { can } = useAuth()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<DockerVolume[]>("/docker/volumes/", undefined, signal),
    30000,
  )
  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
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
                    <IconAction
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

function NetworksTab({ confirm }: { confirm: ConfirmFn }) {
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
                    <IconAction
                      title="Remove"
                      icon={Trash2}
                      destructive
                      onClick={() =>
                        confirm({
                          title: "Delete network",
                          phrase: "delete network",
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
