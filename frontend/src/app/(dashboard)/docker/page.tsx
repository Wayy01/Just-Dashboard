"use client"

import { useCallback, useMemo, useState } from "react"
import {
  Box,
  Boxes,
  Layers,
  Pause,
  Play,
  RotateCw,
  Square,
  Terminal as TerminalIcon,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes, percent, truncateMiddle } from "@/lib/format"
import type { Container, ContainerStats } from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { StatusBadge } from "@/components/status-dot"
import { ContainerDetailSheet } from "@/components/docker/container-detail"
import { IconActionButton } from "@/components/docker/shared"
import { ImagesTab } from "@/components/docker/images-tab"
import { NetworksTab } from "@/components/docker/networks-tab"
import { StacksTab } from "@/components/docker/stacks-tab"
import { VolumesTab } from "@/components/docker/volumes-tab"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Card, CardContent } from "@/components/ui/card"
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
        <PageHeader title="Docker" description="Containers, images, volumes and networks" />
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
                                  <IconActionButton
                                    title="Pause"
                                    icon={Pause}
                                    onClick={() => act(container, "pause")}
                                  />
                                )}
                                {can("destructive") && (
                                  <>
                                    <IconActionButton
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
                                    <IconActionButton
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
                                <IconActionButton
                                  title="Start"
                                  icon={Play}
                                  onClick={() => act(container, "start")}
                                />
                              )
                            )}
                            {can("terminal") && container.state === "running" && (
                              <IconActionButton
                                title="Shell"
                                icon={TerminalIcon}
                                onClick={() => setSelected(container.id)}
                              />
                            )}
                            {can("destructive") && (
                              <IconActionButton
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
