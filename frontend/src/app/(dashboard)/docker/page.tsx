"use client"

import { useCallback, useMemo, useState } from "react"
import {
  Activity,
  ArrowUpCircle,
  Box,
  Boxes,
  CircleSlash,
  HeartPulse,
  Layers,
  Pause,
  Play,
  Plus,
  RotateCw,
  Square,
  Terminal as TerminalIcon,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes, percent, truncateMiddle } from "@/lib/format"
import type {
  Container,
  ContainerSparkline,
  ContainerSpec,
  ContainerStats,
  DockerDiagnosis,
  DockerFinding,
} from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader, SearchInput } from "@/components/page"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { StatTile } from "@/components/stat-tile"
import { Sparkline } from "@/components/metrics/sparkline"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { StatusBadge } from "@/components/status-dot"
import { IconAction } from "@/components/icon-action"
import { ContainerDetailSheet } from "@/components/docker/container-detail"
import { CreateContainerPanel } from "@/components/docker/create-container"
import { DiagnosisPanel, healthLabel } from "@/components/docker/diagnosis-panel"
import { EventsTab } from "@/components/docker/events-tab"
import { ImagesTab } from "@/components/docker/images-tab"
import { NetworksTab } from "@/components/docker/networks-tab"
import { StacksTab } from "@/components/docker/stacks-tab"
import { VolumesTab } from "@/components/docker/volumes-tab"
import { PortLink } from "@/components/docker/shared"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  stickyTableHeader,
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
  const [creating, setCreating] = useState<ContainerSpec | true | null>(null)
  const [filter, setFilter] = useState("")
  const [tab, setTab] = useState("containers")

  /**
   * An hour of shape per container, in one request.
   *
   * The live socket above shows what every container is doing this second,
   * which is the wrong question when something already went wrong: a container
   * that pinned a core for ten minutes and then settled reads as idle. One
   * bulk query gives every row a trend, so the one that spiked is visible in
   * the table rather than only after opening it.
   */
  const trends = usePoll<ContainerSparkline[]>(
    (signal) =>
      get<ContainerSparkline[]>(
        "/docker/containers/stats/history",
        { range: "1h", points: 40 },
        signal,
      ),
    // Slow on purpose: an hour-wide thumbnail does not change meaningfully in
    // less than a minute, and this shares a page with a 2-second stats socket.
    120_000,
    [],
  )
  const trendByName = useMemo(() => {
    const map = new Map<string, ContainerSparkline>()
    // History is keyed by container name, which is what lets a series survive
    // a compose redeploy giving the container a new id.
    for (const line of trends.data ?? []) map.set(line.name, line)
    return map
  }, [trends.data])

  /**
   * The verdict on Docker as a whole.
   *
   * One pass over every container on the server, so a finding for any one of
   * them is available in its own panel without a second request. Polled slowly
   * — the checks read history to tell a spike from a trend, and the answer
   * does not change between two clicks.
   */
  const health = usePoll<DockerDiagnosis>(
    (signal) => get<DockerDiagnosis>("/docker/health", undefined, signal),
    60_000,
  )

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

  /**
   * A finding's remedy, carried out.
   *
   * This is what separates a diagnosis from a warning list: the server names
   * an action it knows how to do, and pressing the button here does it. The
   * ones that are not fixes — "show me the evidence" — open the panel at the
   * tab that holds it, which is the same thing an operator would have done
   * next anyway.
   */
  const runFix = useCallback(
    (finding: DockerFinding) => {
      switch (finding.action) {
        case "logs":
        case "usage":
          setSelected(finding.targetId ?? null)
          break
        case "unpause":
          if (finding.targetId) {
            post(`/docker/containers/${finding.targetId}/unpause`)
              .then(() => {
                toast.success(`${finding.target} resumed`)
                health.refresh()
              })
              .catch((err) => toast.error(String(err)))
          }
          break
        case "set-restart":
          // Docker cannot change a restart policy in place, so this is a
          // recreate with one field edited. It confirms like any other
          // recreate — a pause, not a phrase to type: the old container is
          // renamed aside and put back if the replacement fails to start.
          if (finding.targetId && finding.target) {
            confirm({
              title: "Set a restart policy",
              confirmLabel: "Apply",
              description: (
                <>
                  <p>
                    <b>{finding.target}</b> will be replaced by an identical container that comes
                    back after a reboot. Docker cannot change this on a container that already
                    exists, so the only way to set it is to rebuild it.
                  </p>
                  <p>
                    Its volumes and settings come with it; the service is interrupted for as long
                    as it takes to start.
                  </p>
                </>
              ),
              action: async (phrase) => {
                const spec = await get<ContainerSpec>(
                  `/docker/containers/${finding.targetId}/spec`,
                )
                await post(
                  `/docker/containers/${finding.targetId}/recreate`,
                  { spec: { ...spec, restartPolicy: "unless-stopped" } },
                  { confirm: phrase },
                )
                health.refresh()
              },
            })
          }
          break
        case "stack.up":
          setTab("stacks")
          break
        case "prune":
          setTab("images")
          break
        default:
          if (finding.targetId) setSelected(finding.targetId)
      }
    },
    [health, confirm],
  )

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

  if (ping.loading) {
    return (
      <Page>
        <PageHeader eyebrow="Server" title="Docker" />
        <LoadingPanel />
      </Page>
    )
  }

  if (!available) {
    return (
      <Page>
        <PageHeader
          eyebrow="Server"
          title="Docker"
          description="Containers, images, volumes and networks"
        />
        <EmptyState
          icon={Box}
          title="Docker is not reachable"
          description={
            ping.data?.error ??
            "The dashboard could not connect to the Docker socket. Check that the daemon is running and that this process can read /var/run/docker.sock."
          }
        />
      </Page>
    )
  }

  const running = containers.filter((c) => c.state === "running").length
  const stopped = containers.length - running
  const stacks = new Set(containers.map((c) => c.composeStack).filter(Boolean)).size
  const cpuTotal = Object.values(stats).reduce((s, r) => s + r.cpuPercent, 0)
  const memTotal = Object.values(stats).reduce((s, r) => s + r.memUsage, 0)
  const status = health.data?.status ?? "ok"

  return (
    <Page>
      <PageHeader
        eyebrow="Server"
        title="Docker"
        description={`Engine ${ping.data?.serverVersion ?? "unknown"}`}
        actions={
          <>
            {can("service.control") && (
              <Button size="sm" onClick={() => setCreating(true)}>
                <Plus className="size-4" />
                Run a container
              </Button>
            )}
            {can("destructive") && (
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
                      health.refresh()
                    },
                  })
                }
              >
                <Trash2 className="size-4" />
                Prune
              </Button>
            )}
          </>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4 [&>*]:min-w-0">
        <StatTile
          label="Running"
          icon={Play}
          value={running}
          tone={running > 0 ? "success" : "default"}
          hint={`${containers.length} containers in total`}
        />
        <StatTile
          label="Stopped"
          icon={CircleSlash}
          value={stopped}
          hint={stopped ? "not currently serving" : "everything is up"}
        />
        <StatTile label="Compose stacks" icon={Layers} value={stacks} hint="labelled projects" />
        {/*
          The verdict replaces the fourth "how busy" tile. Utilisation is
          already two tiles away and on every chart; whether anything is
          broken was nowhere.
        */}
        <StatTile
          label="Health"
          icon={HeartPulse}
          value={healthLabel(status)}
          tone={status === "critical" ? "danger" : status === "warning" ? "warning" : "success"}
          hint={
            health.data
              ? `${health.data.findings.length || "no"} finding${health.data.findings.length === 1 ? "" : "s"} · ${percent(cpuTotal, 0)} CPU, ${bytes(memTotal)} resident`
              : "checking…"
          }
        />
      </div>

      {/* Only when there is something to say; an always-present "all good"
          panel is a row of chrome that trains the eye to skip it. */}
      {(health.data?.findings.length ?? 0) > 0 && (
        <DiagnosisPanel diagnosis={health.data} onAction={runFix} />
      )}

      {socketError && <ErrorState error={new Error(socketError)} />}

      <Tabs value={tab} onValueChange={setTab} className="min-w-0 gap-4">
        <TabsList>
          <TabsTrigger value="containers">Containers</TabsTrigger>
          <TabsTrigger value="stacks">Stacks</TabsTrigger>
          <TabsTrigger value="images">Images</TabsTrigger>
          <TabsTrigger value="volumes">Volumes</TabsTrigger>
          <TabsTrigger value="networks">Networks</TabsTrigger>
          <TabsTrigger value="events">
            <Activity className="size-3.5" />
            Events
          </TabsTrigger>
        </TabsList>

        <TabsContent value="containers" className="min-w-0">
          <Panel>
            <PanelHeader
              icon={Box}
              title="Containers"
              description={`${running} running of ${containers.length}`}
            />
            <PanelToolbar>
              <SearchInput
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="Filter by name, image or stack"
              />
            </PanelToolbar>
            <PanelBody flush>
              <Table containerClassName="max-h-[calc(100svh-27rem)]">
                <TableHeader className={stickyTableHeader}>
                  <TableRow>
                    <TableHead className="w-full">Container</TableHead>
                    <TableHead>Image</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead className="text-right">CPU</TableHead>
                    <TableHead className="text-right">Memory</TableHead>
                    <TableHead className="text-right">Last hour</TableHead>
                    <TableHead>Ports</TableHead>
                    <TableHead className="w-px" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visible.map((container) => {
                    const stat = stats[container.id]
                    const worst = worstFinding(health.data, container.id)
                    return (
                      <TableRow
                        key={container.id}
                        className="group"
                        onActivate={() => setSelected(container.id)}
                      >
                        <TableCell>
                          <div className="max-w-[18rem] min-w-0">
                            <button
                              className="truncate text-left text-[13px] font-medium hover:underline"
                              onClick={() => setSelected(container.id)}
                            >
                              {container.name}
                            </button>
                            {container.composeStack && (
                              <p className="truncate text-[11px] text-muted-foreground">
                                <Layers className="mr-1 inline size-3" />
                                {container.composeStack}/{container.composeService}
                              </p>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="font-mono text-[11px] text-muted-foreground">
                          {truncateMiddle(container.image, 34)}
                        </TableCell>
                        <TableCell>
                          <StatusBadge state={container.state} label={container.status} />
                          {/*
                            The worst finding for this row, in its own words.
                            "Exited (137)" is already in the status; what it
                            means was not anywhere.
                          */}
                          {worst ? (
                            <p
                              className={`mt-1 line-clamp-1 max-w-[16rem] text-[11px] ${
                                worst.level === "critical" ? "text-destructive" : "text-warning"
                              }`}
                              title={worst.detail}
                            >
                              {worst.title}
                            </p>
                          ) : (
                            container.health && (
                              <p className="mt-1 text-[11px] text-muted-foreground">
                                {container.health}
                              </p>
                            )
                          )}
                        </TableCell>
                        <TableCell className="text-right">
                          {stat ? (
                            <div className="flex items-center justify-end gap-2">
                              <Progress
                                value={Math.min(stat.cpuPercent, 100)}
                                className="h-1 w-10"
                              />
                              <span className="numeric w-10 font-mono text-[11px]">
                                {percent(stat.cpuPercent)}
                              </span>
                            </div>
                          ) : (
                            <span className="text-[11px] text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="numeric text-right font-mono text-[11px]">
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
                        <TableCell className="text-right">
                          <ContainerTrend trend={trendByName.get(container.name)} />
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1">
                            {container.ports
                              .filter((p) => p.publicPort)
                              .slice(0, 3)
                              .map((p, i) => (
                                <PortLink
                                  key={i}
                                  ip={p.ip}
                                  port={p.publicPort ?? 0}
                                  target={p.privatePort}
                                />
                              ))}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
                            {container.state === "running" ? (
                              <>
                                {can("service.control") && (
                                  <IconAction
                                    label="Pause"
                                    onClick={() => act(container, "pause").catch(() => undefined)}
                                  >
                                    <Pause />
                                  </IconAction>
                                )}
                                {can("destructive") && (
                                  <>
                                    <IconAction
                                      label="Restart"
                                      onClick={() =>
                                        confirm({
                                          title: "Restart container",
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
                                    >
                                      <RotateCw />
                                    </IconAction>
                                    <IconAction
                                      label="Stop"
                                      onClick={() =>
                                        confirm({
                                          title: "Stop container",
                                          confirmLabel: "Stop",
                                          description: (
                                            <p>
                                              <b>{container.name}</b> will stop serving immediately.
                                            </p>
                                          ),
                                          action: (c) => act(container, "stop", c),
                                        })
                                      }
                                    >
                                      <Square />
                                    </IconAction>
                                  </>
                                )}
                              </>
                            ) : (
                              can("service.control") && (
                                <IconAction
                                  label="Start"
                                  onClick={() => act(container, "start").catch(() => undefined)}
                                >
                                  <Play />
                                </IconAction>
                              )
                            )}
                            {/*
                              Update: pull a newer image and rebuild the
                              container from the settings it already has. The
                              action every self-hoster wants and the one that
                              took a shell and four commands.
                            */}
                            {can("destructive") && !container.composeStack && (
                              <IconAction
                                label="Update to a newer image"
                                onClick={() =>
                                  confirm({
                                    title: "Update container",
                                    confirmLabel: "Update",
                                    description: (
                                      <>
                                        <p>
                                          Pulls a newer <b>{container.image}</b> and replaces{" "}
                                          <b>{container.name}</b> with a container built from it,
                                          keeping every setting it has now.
                                        </p>
                                        <p>
                                          Its volumes come with it. Anything written inside the
                                          container rather than into a volume does not.
                                        </p>
                                      </>
                                    ),
                                    action: async (c) => {
                                      await post(
                                        `/docker/containers/${container.id}/recreate`,
                                        { pullLatest: true },
                                        { confirm: c },
                                      )
                                      health.refresh()
                                    },
                                  })
                                }
                              >
                                <ArrowUpCircle />
                              </IconAction>
                            )}
                            {can("terminal") && container.state === "running" && (
                              <IconAction label="Shell" onClick={() => setSelected(container.id)}>
                                <TerminalIcon />
                              </IconAction>
                            )}
                            {can("destructive") && (
                              <IconAction
                                label="Remove"
                                className="text-destructive"
                                onClick={() =>
                                  confirm({
                                    title: "Remove container",
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
                      <TableCell colSpan={8} className="p-0">
                        <EmptyState
                          icon={Boxes}
                          title={filter ? "No containers match that filter" : "Nothing running yet"}
                          description={
                            filter
                              ? undefined
                              : "Start from a common image, paste a docker run command you found, or fill in the form yourself."
                          }
                          action={
                            !filter &&
                            can("service.control") && (
                              <Button size="sm" onClick={() => setCreating(true)}>
                                <Plus className="size-4" />
                                Run a container
                              </Button>
                            )
                          }
                        />
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </PanelBody>
          </Panel>
        </TabsContent>

        <TabsContent value="stacks" className="min-w-0">
          <StacksTab confirm={confirm} />
        </TabsContent>
        <TabsContent value="images" className="min-w-0">
          <ImagesTab confirm={confirm} />
        </TabsContent>
        <TabsContent value="volumes" className="min-w-0">
          <VolumesTab confirm={confirm} />
        </TabsContent>
        <TabsContent value="networks" className="min-w-0">
          <NetworksTab confirm={confirm} />
        </TabsContent>
        <TabsContent value="events" className="min-w-0">
          <EventsTab />
        </TabsContent>
      </Tabs>

      <ContainerDetailSheet
        containerId={selected}
        onOpenChange={(open) => !open && setSelected(null)}
        diagnosis={health.data}
        confirm={confirm}
        onChanged={() => health.refresh()}
        onDuplicate={(spec) => {
          setSelected(null)
          setCreating(spec)
        }}
      />
      <CreateContainerPanel
        open={creating !== null}
        initialSpec={creating === true ? undefined : (creating ?? undefined)}
        onOpenChange={(open) => !open && setCreating(null)}
        onCreated={() => {
          trends.refresh()
          health.refresh()
        }}
      />
      {dialog}
    </Page>
  )
}

/** The most severe thing the diagnosis has to say about one container. */
function worstFinding(diagnosis: DockerDiagnosis | undefined, id: string): DockerFinding | undefined {
  return (diagnosis?.findings ?? []).find(
    (f) => f.targetId === id && (f.level === "critical" || f.level === "warning"),
  )
}

/**
 * One container's last hour, in a table cell.
 *
 * The peak is spelled out beside the line because a sparkline cannot carry a
 * scale: two rows whose lines look identical may be a container that touched
 * 4% and one that pinned two cores, and the number is what tells them apart.
 * The line answers "when", the number answers "how much".
 */
function ContainerTrend({ trend }: { trend?: ContainerSparkline }) {
  if (!trend || trend.cpu.length === 0) {
    return <span className="text-[11px] text-muted-foreground">—</span>
  }
  return (
    <span className="flex items-center justify-end gap-2">
      <Sparkline
        values={trend.cpu}
        label={`CPU over the last hour, peaking at ${percent(trend.cpuPeak)}`}
        color="var(--chart-1)"
      />
      <span className="numeric w-11 shrink-0 text-right font-mono text-[11px] text-muted-foreground">
        {percent(trend.cpuPeak, 0)}
      </span>
    </span>
  )
}
