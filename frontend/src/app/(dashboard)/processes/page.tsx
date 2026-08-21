"use client"

import { useMemo, useState } from "react"
import { Activity, Clock, Cpu, ListChecks, Play, RotateCw, Square, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { del, get, post, put } from "@/lib/api"
import { bytes, duration, percent, relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { Crontab, PM2Process, ProcessRow, SystemdUnit } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useMetrics } from "@/hooks/use-metrics"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader, Metric, MetricStrip, SearchInput } from "@/components/page"
import { Panel, PanelBody, PanelHeader, PanelToolbar, Well } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { StatusBadge } from "@/components/status-dot"
import { UnitJournalSheet } from "@/components/procs/unit-journal"
import { PM2LogSheet } from "@/components/procs/pm2-logs"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { IconAction } from "@/components/icon-action"
import { Textarea } from "@/components/ui/textarea"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export default function ProcessesPage() {
  return (
    <Page>
      <PageHeader
        eyebrow="Server"
        title="Processes"
        description="PM2 applications, systemd units, the raw process table and cron"
      />
      <Tabs defaultValue="pm2" className="min-w-0 gap-4">
        <TabsList>
          <TabsTrigger value="pm2">PM2</TabsTrigger>
          <TabsTrigger value="systemd">systemd</TabsTrigger>
          <TabsTrigger value="table">Process table</TabsTrigger>
          <TabsTrigger value="cron">Cron</TabsTrigger>
        </TabsList>
        <TabsContent value="pm2" className="min-w-0">
          <PM2Tab />
        </TabsContent>
        <TabsContent value="systemd" className="min-w-0">
          <SystemdTab />
        </TabsContent>
        <TabsContent value="table" className="min-w-0">
          <ProcessTableTab />
        </TabsContent>
        <TabsContent value="cron" className="min-w-0">
          <CronTab />
        </TabsContent>
      </Tabs>
    </Page>
  )
}

/** The hover-revealed action cluster every row in this page uses. */
const ROW_ACTIONS =
  "flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100"

function PM2Tab() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [logsFor, setLogsFor] = useState<string | null>(null)
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<{ available: boolean; processes: PM2Process[] }>("/pm2/", undefined, signal),
    5000,
  )

  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />
  if (!data?.available) {
    return (
      <EmptyState
        icon={Activity}
        title="PM2 is not installed"
        description="Install PM2 on this host to manage node applications from here."
      />
    )
  }
  if (data.processes.length === 0) {
    return <EmptyState icon={Activity} title="PM2 is running but manages no processes" />
  }

  const act = async (proc: PM2Process, action: string, confirmText?: string) => {
    await post(`/pm2/${encodeURIComponent(proc.name)}/${action}`, undefined, {
      confirm: confirmText,
    })
    toast.success(`${proc.name} ${action}ed`)
    refresh()
  }

  const online = data.processes.filter((p) => p.status === "online").length

  return (
    <>
      <Panel>
        <PanelHeader
          icon={Activity}
          title="PM2 applications"
          description={`${online} online of ${data.processes.length}`}
        />
        <PanelBody flush>
          <Table containerClassName="max-h-[calc(100svh-20rem)]">
            <TableHeader className={stickyTableHeader}>
              <TableRow>
                <TableHead className="w-full">Application</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">CPU</TableHead>
                <TableHead className="text-right">Memory</TableHead>
                <TableHead className="text-right">Restarts</TableHead>
                <TableHead>Uptime</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.processes.map((proc) => (
                <TableRow key={proc.id} className="group" onActivate={() => setLogsFor(proc.name)}>
                  <TableCell>
                    <div className="max-w-[22rem] min-w-0">
                      <button
                        className="truncate text-[13px] font-medium hover:underline"
                        onClick={() => setLogsFor(proc.name)}
                      >
                        {proc.name}
                      </button>
                      <p className="truncate font-mono text-[11px] text-muted-foreground">
                        {proc.scriptPath}
                      </p>
                    </div>
                  </TableCell>
                  <TableCell>
                    <StatusBadge state={proc.status} />
                  </TableCell>
                  <TableCell className="numeric text-right font-mono text-xs">
                    {percent(proc.cpu)}
                  </TableCell>
                  <TableCell className="numeric text-right font-mono text-xs">
                    {bytes(proc.memory)}
                  </TableCell>
                  <TableCell className="numeric text-right font-mono text-xs">
                    {proc.restarts}
                    {proc.unstableRestarts > 0 && (
                      <span className="ml-1 text-destructive">
                        ({proc.unstableRestarts} unstable)
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {proc.uptimeMs > 0 ? duration(proc.uptimeMs / 1000) : "—"}
                  </TableCell>
                  <TableCell>
                    <div className={ROW_ACTIONS}>
                      {proc.status !== "online" && can("service.control") && (
                        <IconAction
                          label="Start"
                          onClick={() => act(proc, "start").catch((e) => toast.error(String(e)))}
                        >
                          <Play />
                        </IconAction>
                      )}
                      {can("destructive") && (
                        <>
                          <IconAction
                            label="Restart"
                            onClick={() =>
                              confirm({
                                title: "Restart application",
                                phrase: proc.name,
                                confirmLabel: "Restart",
                                description: (
                                  <p>
                                    <b>{proc.name}</b> restarts and briefly stops serving.
                                  </p>
                                ),
                                action: (c) => act(proc, "restart", c),
                              })
                            }
                          >
                            <RotateCw />
                          </IconAction>
                          <IconAction
                            label="Stop"
                            onClick={() =>
                              confirm({
                                title: "Stop application",
                                phrase: proc.name,
                                confirmLabel: "Stop",
                                description: (
                                  <p>
                                    <b>{proc.name}</b> stops until it is started again.
                                  </p>
                                ),
                                action: (c) => act(proc, "stop", c),
                              })
                            }
                          >
                            <Square />
                          </IconAction>
                          <IconAction
                            label="Delete"
                            className="text-destructive"
                            onClick={() =>
                              confirm({
                                title: "Delete from PM2",
                                phrase: proc.name,
                                confirmLabel: "Delete",
                                description: (
                                  <p>
                                    Removes <b>{proc.name}</b> from PM2 entirely. Files on disk are
                                    untouched, but the process definition is gone.
                                  </p>
                                ),
                                action: async (c) => {
                                  await del(`/pm2/${encodeURIComponent(proc.name)}`, { confirm: c })
                                  refresh()
                                },
                              })
                            }
                          >
                            <Trash2 />
                          </IconAction>
                        </>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </PanelBody>
      </Panel>
      <PM2LogSheet name={logsFor} onOpenChange={(o) => !o && setLogsFor(null)} />
      {dialog}
    </>
  )
}

function SystemdTab() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [filter, setFilter] = useState("")
  const [stateFilter, setStateFilter] = useState("all")
  const [journalFor, setJournalFor] = useState<string | null>(null)
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<{ available: boolean; units: SystemdUnit[] }>("/systemd/", undefined, signal),
    10000,
  )

  const visible = useMemo(() => {
    let units = data?.units ?? []
    if (stateFilter !== "all") units = units.filter((u) => u.activeState === stateFilter)
    const needle = filter.toLowerCase()
    if (needle) {
      units = units.filter(
        (u) =>
          u.name.toLowerCase().includes(needle) || u.description.toLowerCase().includes(needle),
      )
    }
    return units
  }, [data, filter, stateFilter])

  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />
  if (!data?.available)
    return <EmptyState icon={ListChecks} title="systemd is not available on this host" />

  const act = async (unit: SystemdUnit, action: string, confirmText?: string) => {
    await post(`/systemd/${encodeURIComponent(unit.name)}/${action}`, undefined, {
      confirm: confirmText,
    })
    toast.success(`${unit.name} ${action}`)
    refresh()
  }

  const failed = data.units.filter((u) => u.activeState === "failed").length

  return (
    <>
      <Panel>
        <PanelHeader
          icon={ListChecks}
          title="systemd units"
          description={`${visible.length} shown of ${data.units.length}`}
          actions={
            failed > 0 && (
              <Badge variant="destructive" className="font-normal">
                {failed} failed
              </Badge>
            )
          }
        />
        <PanelToolbar>
          <SearchInput
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter units"
          />
          <Select value={stateFilter} onValueChange={setStateFilter}>
            <SelectTrigger size="sm" className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All states</SelectItem>
              <SelectItem value="active">Active</SelectItem>
              <SelectItem value="inactive">Inactive</SelectItem>
              <SelectItem value="failed">Failed</SelectItem>
            </SelectContent>
          </Select>
        </PanelToolbar>
        <PanelBody flush>
          <Table containerClassName="max-h-[calc(100svh-23rem)]">
            <TableHeader className={stickyTableHeader}>
              <TableRow>
                <TableHead className="w-full">Unit</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Startup</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.slice(0, 200).map((unit) => (
                <TableRow
                  key={unit.name}
                  className="group"
                  onActivate={() => setJournalFor(unit.name)}
                >
                  <TableCell>
                    <div className="max-w-[26rem] min-w-0">
                      <button
                        className="truncate text-[13px] font-medium hover:underline"
                        onClick={() => setJournalFor(unit.name)}
                      >
                        {unit.name}
                      </button>
                      <p className="truncate text-[11px] text-muted-foreground">
                        {unit.description}
                      </p>
                    </div>
                  </TableCell>
                  <TableCell>
                    <StatusBadge
                      state={unit.activeState}
                      label={`${unit.activeState} (${unit.subState})`}
                    />
                  </TableCell>
                  <TableCell>
                    <Badge variant={unit.enabled ? "outline" : "secondary"} className="font-normal">
                      {unit.unitFileState || "unknown"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <div className={ROW_ACTIONS}>
                      {unit.activeState !== "active" && can("service.control") && (
                        <IconAction
                          label="Start"
                          onClick={() => act(unit, "start").catch((e) => toast.error(String(e)))}
                        >
                          <Play />
                        </IconAction>
                      )}
                      {can("destructive") && (
                        <>
                          <IconAction
                            label="Restart"
                            onClick={() =>
                              confirm({
                                title: "Restart unit",
                                phrase: unit.name,
                                confirmLabel: "Restart",
                                description: (
                                  <p>
                                    <b>{unit.name}</b> restarts, interrupting whatever it serves.
                                  </p>
                                ),
                                action: (c) => act(unit, "restart", c),
                              })
                            }
                          >
                            <RotateCw />
                          </IconAction>
                          {unit.activeState === "active" && (
                            <IconAction
                              label="Stop"
                              onClick={() =>
                                confirm({
                                  title: "Stop unit",
                                  phrase: unit.name,
                                  confirmLabel: "Stop",
                                  description: (
                                    <p>
                                      <b>{unit.name}</b> stops until started again.
                                    </p>
                                  ),
                                  action: (c) => act(unit, "stop", c),
                                })
                              }
                            >
                              <Square />
                            </IconAction>
                          )}
                        </>
                      )}
                      {can("system.admin") && (
                        <Button
                          size="xs"
                          variant="ghost"
                          className="text-muted-foreground"
                          onClick={() =>
                            act(unit, unit.enabled ? "disable" : "enable").catch((e) =>
                              toast.error(String(e)),
                            )
                          }
                        >
                          {unit.enabled ? "Disable" : "Enable"}
                        </Button>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
              {visible.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="p-0">
                    <EmptyState icon={ListChecks} title="No units match" />
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </PanelBody>
      </Panel>
      <UnitJournalSheet unit={journalFor} onOpenChange={(o) => !o && setJournalFor(null)} />
      {dialog}
    </>
  )
}

function ProcessTableTab() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [query, setQuery] = useState("")
  // "What is eating my CPU" and "what is eating my RAM" are asked equally
  // often and have different answers — a leaking service sits at 0% CPU while
  // holding six gigabytes, and a CPU-sorted list buries it. The server sorts
  // before it truncates, so this changes which 200 rows come back, not just
  // their order.
  const [sort, setSort] = useState<"cpu" | "memory">("cpu")
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<ProcessRow[]>("/processes/", { limit: 200, q: query, sort }, signal),
    4000,
    [query, sort],
  )
  // The host figures the table is a breakdown of. Without them the heaviest
  // row is a number with no denominator: 30% CPU is the whole problem on a
  // quiet box and a rounding error on a busy one.
  const { snapshot } = useMetrics()

  if (loading && !data) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  const memTotal = snapshot?.memory.total ?? 0

  return (
    <>
      <Panel>
        <PanelHeader
          icon={Cpu}
          title="Process table"
          description={
            data && data.length >= 200
              ? // The server caps the reply at 200 rows. Saying so beats letting
                // someone conclude a process is not running when it was simply cut.
                `Showing the 200 heaviest by ${sort === "cpu" ? "CPU" : "memory"} — filter to reach the rest`
              : `${data?.length ?? 0} processes`
          }
          actions={
            snapshot && (
              <MetricStrip>
                <Metric label="CPU" value={percent(snapshot.cpu.totalPercent, 0)} />
                <Metric label="Available" value={bytes(snapshot.memory.available)} />
                {/* Running and blocked, because they are what a long list of
                    idle processes cannot tell you: how many are actually
                    competing, and how many are stuck waiting on a device. */}
                <Metric
                  label="Run queue"
                  value={`${snapshot.procs?.running ?? 0} / ${snapshot.procs?.blocked ?? 0}`}
                />
              </MetricStrip>
            )
          }
        />
        <PanelToolbar>
          <SearchInput
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter by name, command or user"
          />
          <ToggleGroup
            type="single"
            value={sort}
            onValueChange={(next) => setSort((next as "cpu" | "memory") || sort)}
            variant="outline"
            size="sm"
            aria-label="Sort by"
          >
            <ToggleGroupItem value="cpu" className="px-2.5 text-[11px]">
              By CPU
            </ToggleGroupItem>
            <ToggleGroupItem value="memory" className="px-2.5 text-[11px]">
              By memory
            </ToggleGroupItem>
          </ToggleGroup>
        </PanelToolbar>
        <PanelBody flush>
          <Table containerClassName="max-h-[calc(100svh-23rem)]">
            <TableHeader className={stickyTableHeader}>
              <TableRow>
                <TableHead className="w-20">PID</TableHead>
                <TableHead className="w-full">Process</TableHead>
                <TableHead>User</TableHead>
                <TableHead className="text-right">CPU</TableHead>
                <TableHead className="text-right">Memory</TableHead>
                <TableHead>Started</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {data?.map((proc) => (
                <TableRow key={proc.pid} className="group">
                  <TableCell className="numeric font-mono text-xs">{proc.pid}</TableCell>
                  <TableCell>
                    {/* A process name can be a full Chromium argv — hundreds of
                        characters. Bounding it here is what keeps one row from
                        setting the width of the whole table. */}
                    <div className="max-w-[26rem] min-w-0">
                      <div className="truncate text-[13px] font-medium" title={proc.name}>
                        {proc.name}
                      </div>
                      <p
                        className="truncate font-mono text-[11px] text-muted-foreground"
                        title={proc.cmdline}
                      >
                        {proc.cmdline}
                      </p>
                    </div>
                  </TableCell>
                  <TableCell className="text-xs">{proc.username}</TableCell>
                  {/* Weight, not just value: the eye finds the two rows that
                      matter far faster in a column where most entries are
                      grey and one is not. */}
                  <TableCell
                    className={cn(
                      "numeric text-right font-mono text-xs",
                      proc.cpuPercent >= 50
                        ? "font-medium text-destructive"
                        : proc.cpuPercent >= 10
                          ? "text-warning"
                          : "text-muted-foreground",
                    )}
                  >
                    {percent(proc.cpuPercent)}
                  </TableCell>
                  <TableCell className="numeric text-right font-mono text-xs">
                    {bytes(proc.rss)}
                    {memTotal > 0 && (
                      <span className="ml-1 text-muted-foreground">
                        {((proc.rss / memTotal) * 100).toFixed(0)}%
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {relativeTime(proc.createTime)}
                  </TableCell>
                  <TableCell>
                    {can("destructive") && (
                      <Button
                        size="xs"
                        variant="ghost"
                        className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                        onClick={() =>
                          confirm({
                            title: "Signal process",
                            phrase: String(proc.pid),
                            confirmLabel: "Send SIGTERM",
                            description: (
                              <>
                                <p>
                                  Sends SIGTERM to <b>{proc.name}</b> (pid {proc.pid}, user{" "}
                                  {proc.username}).
                                </p>
                                <p className="font-mono text-xs text-muted-foreground">
                                  {proc.cmdline}
                                </p>
                              </>
                            ),
                            action: async (c) => {
                              await post(
                                `/processes/${proc.pid}/signal`,
                                { signal: "SIGTERM" },
                                { confirm: c },
                              )
                              refresh()
                            },
                          })
                        }
                      >
                        Kill
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {data?.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7} className="p-0">
                    <EmptyState icon={Cpu} title="No processes match" />
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </PanelBody>
      </Panel>
      {dialog}
    </>
  )
}

function CronTab() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [user, setUser] = useState("root")
  const [draft, setDraft] = useState<string | null>(null)

  const users = usePoll((signal) => get<string[]>("/cron/users", undefined, signal), 0)
  const crontab = usePoll(
    (signal) => get<Crontab>(`/cron/user/${encodeURIComponent(user)}`, undefined, signal),
    0,
    [user],
  )
  const system = usePoll((signal) => get<Crontab[]>("/cron/system", undefined, signal), 0)

  return (
    <>
      <div className="flex min-w-0 flex-col gap-4">
        <Panel>
          <PanelHeader
            icon={Clock}
            title="User crontab"
            description={crontab.data?.source}
            actions={
              <Select
                value={user}
                onValueChange={(v) => {
                  setUser(v)
                  setDraft(null)
                }}
              >
                <SelectTrigger size="sm" className="w-44">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="root">root</SelectItem>
                  {users.data
                    ?.filter((u) => u !== "root")
                    .map((u) => (
                      <SelectItem key={u} value={u}>
                        {u}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            }
          />

          {crontab.error && (
            <PanelBody>
              <ErrorState error={crontab.error} />
            </PanelBody>
          )}

          {crontab.data && draft === null && (
            <>
              {crontab.data.jobs.length === 0 ? (
                <PanelBody>
                  <EmptyState icon={Clock} title={`No cron jobs for ${user}`} />
                </PanelBody>
              ) : (
                <PanelBody flush>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-44">Schedule</TableHead>
                        <TableHead className="w-full">Command</TableHead>
                        <TableHead className="w-24">State</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {crontab.data.jobs.map((job) => (
                        <TableRow key={job.line}>
                          <TableCell className="font-mono text-xs">{job.schedule}</TableCell>
                          <TableCell className="whitespace-normal">
                            <div className="font-mono text-xs break-all">{job.command}</div>
                            {job.comment && (
                              <p className="text-[11px] text-muted-foreground">{job.comment}</p>
                            )}
                          </TableCell>
                          <TableCell>
                            <Badge
                              variant={job.disabled ? "secondary" : "success"}
                              className="font-normal"
                            >
                              {job.disabled ? "disabled" : "active"}
                            </Badge>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </PanelBody>
              )}
              {can("system.admin") && (
                <div className="border-t border-hairline bg-surface-header/60 px-4 py-2.5">
                  <Button variant="outline" size="sm" onClick={() => setDraft(crontab.data!.raw)}>
                    Edit crontab
                  </Button>
                </div>
              )}
            </>
          )}

          {draft !== null && (
            <PanelBody className="space-y-3">
              <Textarea
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                className="min-h-64 font-mono text-xs"
                spellCheck={false}
              />
              <div className="flex gap-2">
                <Button
                  size="sm"
                  onClick={() =>
                    confirm({
                      title: "Replace crontab",
                      phrase: user,
                      confirmLabel: "Save",
                      description: (
                        <p>
                          Replaces the entire crontab for <b>{user}</b>. Scheduled jobs start
                          running on the new schedule immediately.
                        </p>
                      ),
                      action: async (c) => {
                        await put(
                          `/cron/user/${encodeURIComponent(user)}`,
                          { content: draft },
                          { confirm: c },
                        )
                        setDraft(null)
                        crontab.refresh()
                      },
                    })
                  }
                >
                  Save
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setDraft(null)}>
                  Cancel
                </Button>
              </div>
            </PanelBody>
          )}
        </Panel>

        <Panel>
          <PanelHeader
            icon={Clock}
            title="System cron"
            description="Package-managed schedules from /etc/crontab and /etc/cron.d, read-only"
          />
          <PanelBody className="space-y-4">
            {system.data?.map((file) => (
              <div key={file.source} className="min-w-0 space-y-1.5">
                <p className="font-mono text-[11px] text-muted-foreground">{file.source}</p>
                {file.jobs.length === 0 ? (
                  <p className="text-xs text-muted-foreground">No jobs.</p>
                ) : (
                  <Well className="space-y-1 p-2">
                    {file.jobs.map((job, i) => (
                      <div
                        key={i}
                        className={cn(
                          "flex gap-3 text-[11px]",
                          // A commented-out schedule is real syntax, not a
                          // running job — /etc/crontab ships one as its own
                          // worked example. Dimming it keeps the two apart.
                          job.disabled && "opacity-55",
                        )}
                      >
                        <span className="w-32 shrink-0 text-muted-foreground">
                          {job.disabled && <span className="mr-1">#</span>}
                          {job.schedule}
                        </span>
                        {/* /etc/crontab and /etc/cron.d put the account between
                            the schedule and the command; a personal crontab
                            does not, so this column only appears when parsed. */}
                        {job.user && (
                          <span className="w-20 shrink-0 text-muted-foreground">{job.user}</span>
                        )}
                        <span className="break-all">{job.command}</span>
                      </div>
                    ))}
                  </Well>
                )}
              </div>
            ))}
            {!system.data?.length && <EmptyState icon={Clock} title="No system cron files" />}
          </PanelBody>
        </Panel>
      </div>
      {dialog}
    </>
  )
}
