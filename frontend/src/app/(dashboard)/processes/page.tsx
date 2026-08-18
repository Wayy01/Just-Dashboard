"use client"

import { useMemo, useState } from "react"
import {
  Activity,
  Clock,
  Cpu,
  ListChecks,
  Play,
  RotateCw,
  Search,
  Square,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import { del, get, post, put } from "@/lib/api"
import { bytes, duration, percent, relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { Crontab, PM2Process, ProcessRow, SystemdUnit } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { StatusBadge } from "@/components/status-dot"
import { UnitJournalSheet } from "@/components/procs/unit-journal"
import { PM2LogSheet } from "@/components/procs/pm2-logs"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
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
    <>
      <PageHeader
        title="Processes"
        description="PM2 applications, systemd units, the raw process table and cron"
      />
      <Tabs defaultValue="pm2">
        <TabsList>
          <TabsTrigger value="pm2">PM2</TabsTrigger>
          <TabsTrigger value="systemd">systemd</TabsTrigger>
          <TabsTrigger value="table">Process table</TabsTrigger>
          <TabsTrigger value="cron">Cron</TabsTrigger>
        </TabsList>
        <TabsContent value="pm2">
          <PM2Tab />
        </TabsContent>
        <TabsContent value="systemd">
          <SystemdTab />
        </TabsContent>
        <TabsContent value="table">
          <ProcessTableTab />
        </TabsContent>
        <TabsContent value="cron">
          <CronTab />
        </TabsContent>
      </Tabs>
    </>
  )
}

function PM2Tab() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [logsFor, setLogsFor] = useState<string | null>(null)
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<{ available: boolean; processes: PM2Process[] }>("/pm2/", undefined, signal),
    5000,
  )

  if (loading) return <LoadingRows />
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

  return (
    <>
      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Application</TableHead>
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
                    <button
                      className="font-medium hover:underline"
                      onClick={() => setLogsFor(proc.name)}
                    >
                      {proc.name}
                    </button>
                    <p className="truncate font-mono text-[11px] text-muted-foreground">
                      {proc.scriptPath}
                    </p>
                  </TableCell>
                  <TableCell>
                    <StatusBadge state={proc.status} />
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {percent(proc.cpu)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {bytes(proc.memory)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
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
                    <div className="flex gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
                      {proc.status !== "online" && can("service.control") && (
                        <Button
                          size="icon"
                          variant="ghost"
                          className="size-7"
                          title="Start"
                          onClick={() => act(proc, "start").catch((e) => toast.error(String(e)))}
                        >
                          <Play className="size-3.5" />
                        </Button>
                      )}
                      {can("destructive") && (
                        <>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-7"
                            title="Restart"
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
                            <RotateCw className="size-3.5" />
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-7"
                            title="Stop"
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
                            <Square className="size-3.5" />
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-7 text-destructive"
                            title="Delete"
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
                            <Trash2 className="size-3.5" />
                          </Button>
                        </>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
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

  if (loading) return <LoadingRows />
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
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="relative max-w-sm flex-1">
          <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter units"
            className="pl-8"
          />
        </div>
        <Select value={stateFilter} onValueChange={setStateFilter}>
          <SelectTrigger className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All states</SelectItem>
            <SelectItem value="active">Active</SelectItem>
            <SelectItem value="inactive">Inactive</SelectItem>
            <SelectItem value="failed">Failed</SelectItem>
          </SelectContent>
        </Select>
        {failed > 0 && <Badge variant="destructive">{failed} failed</Badge>}
      </div>

      <Card>
        <CardContent className="p-0">
          <Table containerClassName="max-h-[calc(100svh-20rem)]">
            <TableHeader className={stickyTableHeader}>
              <TableRow>
                <TableHead>Unit</TableHead>
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
                    <button
                      className="font-medium hover:underline"
                      onClick={() => setJournalFor(unit.name)}
                    >
                      {unit.name}
                    </button>
                    <p className="truncate text-xs text-muted-foreground">{unit.description}</p>
                  </TableCell>
                  <TableCell>
                    <StatusBadge
                      state={unit.activeState}
                      label={`${unit.activeState} (${unit.subState})`}
                    />
                  </TableCell>
                  <TableCell>
                    <Badge variant={unit.enabled ? "default" : "secondary"}>
                      {unit.unitFileState || "unknown"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <div className="flex gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
                      {unit.activeState !== "active" && can("service.control") && (
                        <Button
                          size="icon"
                          variant="ghost"
                          className="size-7"
                          title="Start"
                          onClick={() => act(unit, "start").catch((e) => toast.error(String(e)))}
                        >
                          <Play className="size-3.5" />
                        </Button>
                      )}
                      {can("destructive") && (
                        <>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-7"
                            title="Restart"
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
                            <RotateCw className="size-3.5" />
                          </Button>
                          {unit.activeState === "active" && (
                            <Button
                              size="icon"
                              variant="ghost"
                              className="size-7"
                              title="Stop"
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
                              <Square className="size-3.5" />
                            </Button>
                          )}
                        </>
                      )}
                      {can("system.admin") && (
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 px-2 text-xs"
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
        </CardContent>
      </Card>
      <UnitJournalSheet unit={journalFor} onOpenChange={(o) => !o && setJournalFor(null)} />
      {dialog}
    </>
  )
}

function ProcessTableTab() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [query, setQuery] = useState("")
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<ProcessRow[]>("/processes/", { limit: 200, q: query }, signal),
    4000,
    [query],
  )

  if (loading && !data) return <LoadingRows />
  if (error) return <ErrorState error={error} />

  return (
    <>
      <div className="relative mb-4 max-w-sm">
        <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter by name, command or user"
          className="pl-8"
        />
      </div>
      {data && data.length >= 200 && (
        // The server caps the reply at 200 rows. Saying so beats letting
        // someone conclude a process is not running when it was simply cut.
        <p className="mb-2 text-xs text-muted-foreground">
          Showing the 200 heaviest processes. Filter to reach the rest.
        </p>
      )}
      <Card>
        <CardContent className="p-0">
          <Table containerClassName="max-h-[calc(100svh-20rem)]">
            <TableHeader className={stickyTableHeader}>
              <TableRow>
                <TableHead className="w-20">PID</TableHead>
                <TableHead>Process</TableHead>
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
                  <TableCell className="font-mono text-xs tabular-nums">{proc.pid}</TableCell>
                  <TableCell>
                    {/* A process name can be a full Chromium argv — hundreds of
                        characters. Bounding it here is what keeps one row from
                        setting the width of the whole table. */}
                    <div className="max-w-[28rem] min-w-0">
                      <div className="truncate font-medium" title={proc.name}>
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
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {percent(proc.cpuPercent)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {bytes(proc.rss)}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {relativeTime(proc.createTime)}
                  </TableCell>
                  <TableCell>
                    {can("destructive") && (
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 px-2 text-xs text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100"
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
        </CardContent>
      </Card>
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
      <div className="space-y-6">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0">
            <div>
              <CardTitle className="text-base">User crontab</CardTitle>
              <CardDescription>{crontab.data?.source}</CardDescription>
            </div>
            <Select
              value={user}
              onValueChange={(v) => {
                setUser(v)
                setDraft(null)
              }}
            >
              <SelectTrigger className="w-48">
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
          </CardHeader>
          <CardContent className="space-y-4">
            {crontab.error && <ErrorState error={crontab.error} />}
            {crontab.data && draft === null && (
              <>
                {crontab.data.jobs.length === 0 ? (
                  <EmptyState icon={Clock} title={`No cron jobs for ${user}`} />
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-40">Schedule</TableHead>
                        <TableHead>Command</TableHead>
                        <TableHead className="w-24">State</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {crontab.data.jobs.map((job) => (
                        <TableRow key={job.line}>
                          <TableCell className="font-mono text-xs">{job.schedule}</TableCell>
                          <TableCell>
                            <div className="font-mono text-xs break-all">{job.command}</div>
                            {job.comment && (
                              <p className="text-[11px] text-muted-foreground">{job.comment}</p>
                            )}
                          </TableCell>
                          <TableCell>
                            <Badge variant={job.disabled ? "secondary" : "default"}>
                              {job.disabled ? "disabled" : "active"}
                            </Badge>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
                {can("system.admin") && (
                  <Button variant="outline" size="sm" onClick={() => setDraft(crontab.data!.raw)}>
                    Edit crontab
                  </Button>
                )}
              </>
            )}

            {draft !== null && (
              <div className="space-y-3">
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
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">System cron</CardTitle>
            <CardDescription>
              Package-managed schedules from /etc/crontab and /etc/cron.d, shown read-only
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {system.data?.map((file) => (
              <div key={file.source} className="space-y-2">
                <p className="font-mono text-xs text-muted-foreground">{file.source}</p>
                {file.jobs.length === 0 ? (
                  <p className="text-xs text-muted-foreground">No jobs.</p>
                ) : (
                  <div className="space-y-1 rounded-md border p-2">
                    {file.jobs.map((job, i) => (
                      <div
                        key={i}
                        className={cn(
                          "flex gap-3 font-mono text-[11px]",
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
                  </div>
                )}
              </div>
            ))}
            {!system.data?.length && <EmptyState icon={Clock} title="No system cron files" />}
          </CardContent>
        </Card>
      </div>
      {dialog}
    </>
  )
}
