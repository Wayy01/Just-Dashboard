"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Area, AreaChart, CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts"
import { Cpu, HardDrive, MemoryStick, Network, Server, Timer } from "lucide-react"
import { get } from "@/lib/api"
import { cn } from "@/lib/utils"
import { bytes, clock, duration, percent, rate } from "@/lib/format"
import type { DirEntry, HostInfo, Snapshot } from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { PageHeader } from "@/components/page-header"
import { StatCard, utilisationBar, utilisationTone } from "@/components/stat-card"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/** How many samples the live charts keep — roughly five minutes at 2s. */
const HISTORY = 150

type Point = {
  t: string
  cpu: number
  mem: number
  swap: number
  rx: number
  tx: number
}

const cpuConfig = {
  cpu: { label: "CPU", color: "var(--chart-1)" },
} satisfies ChartConfig

const memConfig = {
  mem: { label: "Memory", color: "var(--chart-2)" },
  swap: { label: "Swap", color: "var(--chart-4)" },
} satisfies ChartConfig

const netConfig = {
  rx: { label: "In", color: "var(--chart-2)" },
  tx: { label: "Out", color: "var(--chart-5)" },
} satisfies ChartConfig

export default function OverviewPage() {
  const [host, setHost] = useState<HostInfo>()
  const [snapshot, setSnapshot] = useState<Snapshot>()
  const [history, setHistory] = useState<Point[]>([])
  const [error, setError] = useState<Error>()

  const onMessage = useCallback((envelope: Envelope) => {
    if (envelope.type === "host") {
      setHost(envelope.data as HostInfo)
      return
    }
    if (envelope.type !== "metrics") return
    const snap = envelope.data as Snapshot
    setSnapshot(snap)
    setHistory((prev) => {
      const rx = snap.net.reduce((sum, n) => sum + n.recvRate, 0)
      const tx = snap.net.reduce((sum, n) => sum + n.sendRate, 0)
      const next = [
        ...prev,
        {
          t: clock(snap.ts),
          cpu: snap.cpu.totalPercent,
          mem: snap.memory.usedPercent,
          swap: snap.swap.usedPercent,
          rx,
          tx,
        },
      ]
      return next.length > HISTORY ? next.slice(next.length - HISTORY) : next
    })
  }, [])

  const { state } = useSocket("/system/stream", { onMessage, query: { interval: 2000 } })

  // The socket carries everything once connected; this single fetch just
  // avoids an empty screen during the handshake.
  useEffect(() => {
    let cancelled = false
    Promise.all([get<HostInfo>("/system/host"), get<Snapshot>("/system/metrics")])
      .then(([h, m]) => {
        if (cancelled) return
        setHost((prev) => prev ?? h)
        setSnapshot((prev) => prev ?? m)
      })
      .catch((err) => !cancelled && setError(err))
    return () => {
      cancelled = true
    }
  }, [])

  if (error && !snapshot) return <ErrorState error={error} />
  if (!snapshot || !host) return <LoadingRows rows={6} />

  const memTone = utilisationTone(snapshot.memory.usedPercent)

  return (
    <>
      <PageHeader
        title={host.hostname}
        description={
          <span className="flex flex-wrap items-center gap-2">
            <span>
              {host.platform} {host.platformVersion} · kernel {host.kernelVersion} ·{" "}
              {host.kernelArch}
            </span>
            {host.virtualization && <Badge variant="outline">{host.virtualization}</Badge>}
          </span>
        }
        actions={
          <Badge variant={state === "open" ? "success" : "secondary"} className="gap-1.5">
            <span
              className={cn(
                "size-1.5 rounded-full",
                state === "open" ? "bg-success" : "bg-muted-foreground",
              )}
            />
            {state === "open" ? "live" : state}
          </Badge>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4 [&>*]:min-w-0">
        <StatCard
          title="CPU"
          icon={Cpu}
          value={percent(snapshot.cpu.totalPercent)}
          percent={snapshot.cpu.totalPercent}
          tone={utilisationTone(snapshot.cpu.totalPercent)}
          detail={`${snapshot.cpu.cores} cores · load ${snapshot.cpu.loadAvg1.toFixed(2)} ${snapshot.cpu.loadAvg5.toFixed(2)} ${snapshot.cpu.loadAvg15.toFixed(2)}`}
        />
        <StatCard
          title="Memory"
          icon={MemoryStick}
          value={percent(snapshot.memory.usedPercent)}
          percent={snapshot.memory.usedPercent}
          tone={memTone}
          detail={`${bytes(snapshot.memory.used)} of ${bytes(snapshot.memory.total)} · ${bytes(snapshot.memory.available)} available`}
        />
        <StatCard
          title="Swap"
          icon={HardDrive}
          value={snapshot.swap.total === 0 ? "none" : percent(snapshot.swap.usedPercent)}
          percent={snapshot.swap.total === 0 ? 0 : snapshot.swap.usedPercent}
          tone={utilisationTone(snapshot.swap.usedPercent)}
          detail={
            snapshot.swap.total === 0
              ? "no swap configured"
              : `${bytes(snapshot.swap.used)} of ${bytes(snapshot.swap.total)}`
          }
        />
        <StatCard
          title="Uptime"
          icon={Timer}
          value={duration(snapshot.uptimeSeconds)}
          detail={`${host.processes} processes · booted ${new Date(host.bootTime).toLocaleDateString()}`}
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">CPU</CardTitle>
            <CardDescription className="font-mono text-xs">{host.cpuModel}</CardDescription>
          </CardHeader>
          <CardContent>
            <ChartContainer config={cpuConfig} className="h-[200px] w-full">
              <AreaChart data={history} margin={{ left: -20, right: 4, top: 4 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <XAxis
                  dataKey="t"
                  tickLine={false}
                  axisLine={false}
                  minTickGap={48}
                  fontSize={11}
                />
                <YAxis domain={[0, 100]} tickLine={false} axisLine={false} fontSize={11} unit="%" />
                <ChartTooltip content={<ChartTooltipContent />} />
                <Area
                  dataKey="cpu"
                  type="monotone"
                  stroke="var(--color-cpu)"
                  fill="var(--color-cpu)"
                  fillOpacity={0.15}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ChartContainer>
            <PerCoreBars cores={snapshot.cpu.perCore} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Memory and swap</CardTitle>
            <CardDescription>Share of total, sampled every 2 seconds</CardDescription>
          </CardHeader>
          <CardContent>
            <ChartContainer config={memConfig} className="h-[200px] w-full">
              <LineChart data={history} margin={{ left: -20, right: 4, top: 4 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <XAxis
                  dataKey="t"
                  tickLine={false}
                  axisLine={false}
                  minTickGap={48}
                  fontSize={11}
                />
                <YAxis domain={[0, 100]} tickLine={false} axisLine={false} fontSize={11} unit="%" />
                <ChartTooltip content={<ChartTooltipContent />} />
                <ChartLegend content={<ChartLegendContent />} />
                <Line
                  dataKey="mem"
                  type="monotone"
                  stroke="var(--color-mem)"
                  dot={false}
                  isAnimationActive={false}
                />
                <Line
                  dataKey="swap"
                  type="monotone"
                  stroke="var(--color-swap)"
                  dot={false}
                  isAnimationActive={false}
                />
              </LineChart>
            </ChartContainer>
            <div className="mt-3 grid grid-cols-2 gap-3 text-xs text-muted-foreground">
              <div>Cached {bytes(snapshot.memory.cached)}</div>
              <div>Buffers {bytes(snapshot.memory.buffers)}</div>
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Network throughput</CardTitle>
          <CardDescription>Combined across all interfaces</CardDescription>
        </CardHeader>
        <CardContent>
          <ChartContainer config={netConfig} className="h-[180px] w-full">
            <AreaChart data={history} margin={{ left: 4, right: 4, top: 4 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="t" tickLine={false} axisLine={false} minTickGap={48} fontSize={11} />
              <YAxis
                width={58}
                tickLine={false}
                axisLine={false}
                fontSize={11}
                tickFormatter={(v) => bytes(v)}
              />
              <ChartTooltip
                content={<ChartTooltipContent formatter={(value) => rate(Number(value))} />}
              />
              <ChartLegend content={<ChartLegendContent />} />
              <Area
                dataKey="rx"
                type="monotone"
                stroke="var(--color-rx)"
                fill="var(--color-rx)"
                fillOpacity={0.15}
                isAnimationActive={false}
              />
              <Area
                dataKey="tx"
                type="monotone"
                stroke="var(--color-tx)"
                fill="var(--color-tx)"
                fillOpacity={0.15}
                isAnimationActive={false}
              />
            </AreaChart>
          </ChartContainer>
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        <MountsCard snapshot={snapshot} />
        <InterfacesCard snapshot={snapshot} />
      </div>
    </>
  )
}

function PerCoreBars({ cores }: { cores: number[] }) {
  if (cores.length === 0) return null
  return (
    <div className="mt-4 space-y-1.5">
      {cores.map((value, i) => (
        <div key={i} className="flex items-center gap-2">
          <span className="w-10 shrink-0 font-mono text-[11px] text-muted-foreground">cpu{i}</span>
          <Progress
            value={value}
            className={cn("h-1.5 flex-1", utilisationBar(utilisationTone(value)))}
          />
          <span className="w-12 shrink-0 text-right font-mono text-[11px] tabular-nums text-muted-foreground">
            {value.toFixed(0)}%
          </span>
        </div>
      ))}
    </div>
  )
}

function MountsCard({ snapshot }: { snapshot: Snapshot }) {
  const [scanning, setScanning] = useState<string | null>(null)
  const [breakdown, setBreakdown] = useState<Record<string, DirEntry[]>>({})
  const abortRef = useRef<AbortController>(null)

  const scan = async (mountpoint: string) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    setScanning(mountpoint)
    try {
      const entries = await get<DirEntry[]>(
        "/system/disk-usage",
        { path: mountpoint, limit: 12 },
        controller.signal,
      )
      setBreakdown((prev) => ({ ...prev, [mountpoint]: entries }))
    } catch {
      // A scan that times out or hits an unreadable tree is not worth a toast;
      // the row simply stays un-expanded.
    } finally {
      setScanning(null)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Filesystems</CardTitle>
        <CardDescription>Expand a mount to see what is using it</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {snapshot.mounts.map((mount) => (
          <div key={mount.mountpoint} className="space-y-2">
            <div className="flex items-center justify-between gap-3 text-sm">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate font-medium">{mount.mountpoint}</span>
                  <Badge variant="outline" className="shrink-0 text-[10px]">
                    {mount.fstype}
                  </Badge>
                </div>
                <p className="truncate font-mono text-xs text-muted-foreground">{mount.device}</p>
              </div>
              <div className="shrink-0 text-right">
                <div className="tabular-nums">
                  {bytes(mount.used)} / {bytes(mount.total)}
                </div>
                <p className="text-xs text-muted-foreground">
                  {rate(mount.readRate)} read · {rate(mount.writeRate)} write
                </p>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Progress
                value={mount.usedPercent}
                className={cn("h-1.5 flex-1", utilisationBar(utilisationTone(mount.usedPercent)))}
              />
              <span
                className={cn(
                  "w-12 text-right font-mono text-xs",
                  mount.usedPercent >= 90
                    ? "text-destructive"
                    : mount.usedPercent >= 75
                      ? "text-warning"
                      : "text-muted-foreground",
                )}
              >
                {mount.usedPercent.toFixed(0)}%
              </span>
              <Button
                size="sm"
                variant="ghost"
                className="h-6 px-2 text-xs"
                disabled={scanning !== null}
                onClick={() => scan(mount.mountpoint)}
              >
                {scanning === mount.mountpoint ? "scanning…" : "scan"}
              </Button>
            </div>
            {breakdown[mount.mountpoint] && (
              <div className="space-y-1 rounded-md border bg-muted/40 p-2">
                {breakdown[mount.mountpoint].map((entry) => (
                  <div key={entry.path} className="flex justify-between gap-2 text-xs">
                    <span className="truncate font-mono">{entry.name}</span>
                    <span className="shrink-0 tabular-nums text-muted-foreground">
                      {bytes(entry.size)}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}
        {snapshot.mounts.length === 0 && (
          <EmptyState title="No filesystems reported" icon={HardDrive} />
        )}
      </CardContent>
    </Card>
  )
}

function InterfacesCard({ snapshot }: { snapshot: Snapshot }) {
  const total = useMemo(
    () => ({
      rx: snapshot.net.reduce((s, n) => s + n.bytesRecv, 0),
      tx: snapshot.net.reduce((s, n) => s + n.bytesSent, 0),
    }),
    [snapshot.net],
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Network className="size-4" />
          Interfaces
        </CardTitle>
        <CardDescription>
          {bytes(total.rx)} in · {bytes(total.tx)} out since boot
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Interface</TableHead>
              <TableHead className="text-right">In</TableHead>
              <TableHead className="text-right">Out</TableHead>
              <TableHead className="text-right">Errors</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {snapshot.net.map((iface) => (
              <TableRow key={iface.interface}>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <span
                      className={cn(
                        "size-1.5 rounded-full",
                        iface.isUp ? "bg-success" : "bg-muted-foreground",
                      )}
                    />
                    <span className="font-mono text-xs">{iface.interface}</span>
                  </div>
                  <p className="truncate font-mono text-[11px] text-muted-foreground">
                    {iface.addrs.join(", ") || "no address"}
                  </p>
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  {rate(iface.recvRate)}
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  {rate(iface.sendRate)}
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                  {iface.errIn + iface.errOut}
                </TableCell>
              </TableRow>
            ))}
            {snapshot.net.length === 0 && (
              <TableRow>
                <TableCell colSpan={4}>
                  <EmptyState title="No interfaces reported" icon={Server} />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
