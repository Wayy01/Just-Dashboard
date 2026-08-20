"use client"

import { useMemo, useRef, useState } from "react"
import { Area, AreaChart, CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts"
import { Activity, Cpu, HardDrive, MemoryStick, Network, Server, Waves } from "lucide-react"
import { get } from "@/lib/api"
import { cn } from "@/lib/utils"
import { bytes, duration, percent, rate } from "@/lib/format"
import type { DirEntry, Snapshot } from "@/lib/types"
import { useMetrics } from "@/hooks/use-metrics"
import {
  useMetricsHistory,
  useRangePreference,
  useStorageHistory,
  type HistoryState,
} from "@/hooks/use-metrics-history"
import {
  coverageNote,
  historyRows,
  liveRows,
  liveStorageRows,
  storageRows,
  RANGES,
  rangeSpec,
  retentionNote,
  type ChartRow,
  type RangeKey,
  type RangeSpec,
} from "@/lib/metrics-range"
import { Page, PageHeader, Metric, MetricStrip, Section } from "@/components/page"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { StatTile, utilisationBar, utilisationTone } from "@/components/stat-tile"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
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

// Peaks share their series' colour: they are the same measurement seen at a
// finer resolution, not a different quantity, and giving them a colour of
// their own would read as four unrelated lines instead of two pairs.
const cpuConfig = {
  cpu: { label: "CPU", color: "var(--chart-1)" },
  cpuPeak: { label: "CPU peak", color: "var(--chart-1)" },
} satisfies ChartConfig

const memConfig = {
  mem: { label: "Memory", color: "var(--chart-2)" },
  memPeak: { label: "Memory peak", color: "var(--chart-2)" },
  swap: { label: "Swap", color: "var(--chart-4)" },
} satisfies ChartConfig

const ioConfig = {
  diskRead: { label: "Read", color: "var(--chart-2)" },
  diskReadPeak: { label: "Read peak", color: "var(--chart-2)" },
  diskWrite: { label: "Write", color: "var(--chart-5)" },
  diskWritePeak: { label: "Write peak", color: "var(--chart-5)" },
} satisfies ChartConfig

const netConfig = {
  rx: { label: "In", color: "var(--chart-2)" },
  rxPeak: { label: "In peak", color: "var(--chart-2)" },
  tx: { label: "Out", color: "var(--chart-5)" },
  txPeak: { label: "Out peak", color: "var(--chart-5)" },
} satisfies ChartConfig

export default function OverviewPage() {
  // Two sources, deliberately kept apart. The live socket is owned by the
  // dashboard shell and is only ever a view of "since this tab opened"; the
  // recorded series comes from the backend, which has been sampling on its own
  // timer whether or not anyone was watching. The page defaults to the
  // recorded one, because a server that has been up for ten hours has ten
  // hours worth looking at and a chart that starts at zero every visit cannot
  // show a spike that happened overnight.
  const { host, snapshot, history, error } = useMetrics()
  const [range, setRange] = useRangePreference()
  const spec = rangeSpec(range)
  const recorded = useMetricsHistory(range)

  const recordedStorage = useStorageHistory(range)

  const rows = useMemo<ChartRow[]>(() => {
    if (range === "live") return liveRows(history)
    return recorded.history ? historyRows(recorded.history) : []
  }, [range, history, recorded.history])

  // The live feed carries only the fullest figure per frame, so the live view
  // is one honestly-labelled line and the recorded views are one per mount.
  const storage = useMemo(() => {
    if (range === "live") return liveStorageRows(history, snapshot?.mounts ?? [])
    return recordedStorage.storage
      ? storageRows(recordedStorage.storage)
      : { rows: [], series: [] }
  }, [range, history, snapshot?.mounts, recordedStorage.storage])

  const storageConfig = useMemo<ChartConfig>(
    () =>
      Object.fromEntries(
        storage.series.map((mount) => [mount.key, { label: mount.mountpoint, color: mount.color }]),
      ),
    [storage.series],
  )

  if (error && !snapshot) {
    return (
      <Page>
        <PageHeader eyebrow="Server" title="Overview" />
        <ErrorState error={new Error(error)} />
      </Page>
    )
  }

  if (!snapshot || !host) {
    return (
      <Page>
        <PageHeader eyebrow="Server" title="Overview" description="Waiting for the first frame…" />
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4 [&>*]:min-w-0">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-[7.5rem] rounded-xl" />
          ))}
        </div>
        <LoadingPanel rows={5} />
      </Page>
    )
  }

  const memTone = utilisationTone(snapshot.memory.usedPercent)
  // The tiles are "right now", so they read the newest snapshot rather than
  // the chart series — which, on a recorded range, is a six-minute average.
  const throughput = {
    rx: snapshot.net.reduce((sum, n) => sum + n.recvRate, 0),
    tx: snapshot.net.reduce((sum, n) => sum + n.sendRate, 0),
  }
  const showPeaks = range !== "live"
  const empty = rows.length === 0
  const placeholder = emptyChartNote(range, recorded)

  return (
    <Page>
      <PageHeader
        eyebrow="Server"
        title={host.hostname}
        description={
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span>
              {host.platform} {host.platformVersion}
            </span>
            <Dot />
            <span>kernel {host.kernelVersion}</span>
            <Dot />
            <span>{host.kernelArch}</span>
            {host.virtualization && (
              <Badge variant="outline" className="font-normal">
                {host.virtualization}
              </Badge>
            )}
          </span>
        }
        actions={
          <MetricStrip>
            <Metric label="Uptime" value={duration(snapshot.uptimeSeconds)} />
            <Metric label="Processes" value={host.processes} />
            <Metric label="Cores" value={snapshot.cpu.cores} />
          </MetricStrip>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4 [&>*]:min-w-0">
        <StatTile
          label="CPU"
          icon={Cpu}
          value={percent(snapshot.cpu.totalPercent)}
          meter={snapshot.cpu.totalPercent}
          tone={utilisationTone(snapshot.cpu.totalPercent)}
          hint={`load ${snapshot.cpu.loadAvg1.toFixed(2)} · ${snapshot.cpu.loadAvg5.toFixed(2)} · ${snapshot.cpu.loadAvg15.toFixed(2)}`}
          trailing={
            <span className="text-[11px] text-muted-foreground">{snapshot.cpu.cores} cores</span>
          }
        />
        <StatTile
          label="Memory"
          icon={MemoryStick}
          value={percent(snapshot.memory.usedPercent)}
          meter={snapshot.memory.usedPercent}
          tone={memTone}
          hint={`${bytes(snapshot.memory.available)} available`}
          trailing={
            <span className="numeric text-[11px] text-muted-foreground">
              {bytes(snapshot.memory.used)} / {bytes(snapshot.memory.total)}
            </span>
          }
        />
        <StatTile
          label="Swap"
          icon={HardDrive}
          value={snapshot.swap.total === 0 ? "none" : percent(snapshot.swap.usedPercent)}
          meter={snapshot.swap.total === 0 ? 0 : snapshot.swap.usedPercent}
          tone={utilisationTone(snapshot.swap.usedPercent)}
          hint={
            snapshot.swap.total === 0
              ? "no swap configured"
              : `${bytes(snapshot.swap.used)} of ${bytes(snapshot.swap.total)}`
          }
        />
        <StatTile
          label="Network"
          icon={Waves}
          value={rate(throughput.rx)}
          hint={`${rate(throughput.tx)} out · ${snapshot.net.length} interfaces`}
          trailing={<span className="text-[11px] text-muted-foreground">in</span>}
        />
      </div>

      <Section
        title="Utilisation"
        description={<SeriesCaption range={range} spec={spec} recorded={recorded} />}
        actions={<RangePicker value={range} onChange={setRange} />}
      >
        {recorded.error && <ErrorState error={recorded.error} />}

        {/* Two charts of the same shape side by side. Both bodies are flex
            columns whose chart takes the slack, because the grid stretches the
            shorter panel to the taller one's height and a fixed-height chart
            would leave that difference as dead space inside the card. */}
        <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
          <Panel>
            <PanelHeader
              icon={Cpu}
              title="Processor"
              description={host.cpuModel}
              actions={
                <span className="numeric text-sm font-medium">
                  {percent(snapshot.cpu.totalPercent)}
                </span>
              }
            />
            <PanelBody className="flex flex-1 flex-col gap-4">
              {empty ? (
                <ChartPlaceholder note={placeholder} className="min-h-[190px] flex-1" />
              ) : (
                <ChartContainer
                  config={cpuConfig}
                  className="aspect-auto min-h-[190px] w-full flex-1"
                >
                  <AreaChart data={rows} margin={{ left: -22, right: 4, top: 4 }}>
                    <CartesianGrid vertical={false} strokeDasharray="3 3" opacity={0.4} />
                    <XAxis
                      dataKey="t"
                      tickLine={false}
                      axisLine={false}
                      minTickGap={48}
                      fontSize={10}
                    />
                    <YAxis
                      domain={[0, 100]}
                      tickLine={false}
                      axisLine={false}
                      fontSize={10}
                      unit="%"
                    />
                    <ChartTooltip content={<ChartTooltipContent labelFormatter={rowLabel} />} />
                    {showPeaks && <ChartLegend content={<ChartLegendContent />} />}
                    {/* Drawn first so it sits behind the average: the peak is the
                        envelope the average lives inside, not a second reading. */}
                    {showPeaks && (
                      <Area
                        dataKey="cpuPeak"
                        type="monotone"
                        stroke="var(--color-cpuPeak)"
                        strokeWidth={1}
                        strokeOpacity={0.45}
                        fill="var(--color-cpuPeak)"
                        fillOpacity={0.07}
                        isAnimationActive={false}
                      />
                    )}
                    <Area
                      dataKey="cpu"
                      type="monotone"
                      stroke="var(--color-cpu)"
                      strokeWidth={1.5}
                      fill="var(--color-cpu)"
                      fillOpacity={0.14}
                      isAnimationActive={false}
                    />
                  </AreaChart>
                </ChartContainer>
              )}
            </PanelBody>
          </Panel>

          <Panel>
            <PanelHeader icon={MemoryStick} title="Memory and swap" description="Share of total" />
            <PanelBody className="flex flex-1 flex-col gap-4">
              {empty ? (
                <ChartPlaceholder note={placeholder} className="min-h-[190px] flex-1" />
              ) : (
                <ChartContainer
                  config={memConfig}
                  className="aspect-auto min-h-[190px] w-full flex-1"
                >
                  <LineChart data={rows} margin={{ left: -22, right: 4, top: 4 }}>
                    <CartesianGrid vertical={false} strokeDasharray="3 3" opacity={0.4} />
                    <XAxis
                      dataKey="t"
                      tickLine={false}
                      axisLine={false}
                      minTickGap={48}
                      fontSize={10}
                    />
                    <YAxis
                      domain={[0, 100]}
                      tickLine={false}
                      axisLine={false}
                      fontSize={10}
                      unit="%"
                    />
                    <ChartTooltip content={<ChartTooltipContent labelFormatter={rowLabel} />} />
                    <ChartLegend content={<ChartLegendContent />} />
                    <Line
                      dataKey="mem"
                      type="monotone"
                      stroke="var(--color-mem)"
                      strokeWidth={1.5}
                      dot={false}
                      isAnimationActive={false}
                    />
                    {showPeaks && (
                      <Line
                        dataKey="memPeak"
                        type="monotone"
                        stroke="var(--color-memPeak)"
                        strokeWidth={1}
                        strokeOpacity={0.5}
                        strokeDasharray="3 3"
                        dot={false}
                        isAnimationActive={false}
                      />
                    )}
                    <Line
                      dataKey="swap"
                      type="monotone"
                      stroke="var(--color-swap)"
                      strokeWidth={1.5}
                      dot={false}
                      isAnimationActive={false}
                    />
                  </LineChart>
                </ChartContainer>
              )}
              {/* Equal columns rather than the header strip's left-packed run:
                    four figures across a half-page panel otherwise leave the
                    right third of the row empty. */}
              <MetricStrip className="[&>*]:flex-1">
                <Metric label="Used" value={bytes(snapshot.memory.used)} />
                <Metric label="Cached" value={bytes(snapshot.memory.cached)} />
                <Metric label="Buffers" value={bytes(snapshot.memory.buffers)} />
                <Metric label="Available" value={bytes(snapshot.memory.available)} />
              </MetricStrip>
            </PanelBody>
          </Panel>
        </div>

        <Panel>
          <PanelHeader icon={Activity} title="Network throughput" description="All interfaces">
            <span className="flex-1" />
          </PanelHeader>
          <PanelBody>
            {empty ? (
              <ChartPlaceholder note={placeholder} className="h-[180px]" />
            ) : (
              <ChartContainer config={netConfig} className="h-[180px] w-full">
                <AreaChart data={rows} margin={{ left: 4, right: 4, top: 4 }}>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" opacity={0.4} />
                  <XAxis
                    dataKey="t"
                    tickLine={false}
                    axisLine={false}
                    minTickGap={48}
                    fontSize={10}
                  />
                  <YAxis
                    width={56}
                    tickLine={false}
                    axisLine={false}
                    fontSize={10}
                    tickFormatter={(v) => bytes(v)}
                  />
                  <ChartTooltip
                    content={
                      <ChartTooltipContent
                        labelFormatter={rowLabel}
                        formatter={(value) => rate(Number(value))}
                      />
                    }
                  />
                  <ChartLegend content={<ChartLegendContent />} />
                  <Area
                    dataKey="rx"
                    type="monotone"
                    stroke="var(--color-rx)"
                    strokeWidth={1.5}
                    fill="var(--color-rx)"
                    fillOpacity={0.14}
                    isAnimationActive={false}
                  />
                  <Area
                    dataKey="tx"
                    type="monotone"
                    stroke="var(--color-tx)"
                    strokeWidth={1.5}
                    fill="var(--color-tx)"
                    fillOpacity={0.14}
                    isAnimationActive={false}
                  />
                  {/* Peaks are lines rather than filled areas: stacked
                      translucent fills stop being readable at four series. */}
                  {showPeaks && (
                    <Line
                      dataKey="rxPeak"
                      type="monotone"
                      stroke="var(--color-rxPeak)"
                      strokeWidth={1}
                      strokeOpacity={0.5}
                      strokeDasharray="3 3"
                      dot={false}
                      isAnimationActive={false}
                    />
                  )}
                  {showPeaks && (
                    <Line
                      dataKey="txPeak"
                      type="monotone"
                      stroke="var(--color-txPeak)"
                      strokeWidth={1}
                      strokeOpacity={0.5}
                      strokeDasharray="3 3"
                      dot={false}
                      isAnimationActive={false}
                    />
                  )}
                </AreaChart>
              </ChartContainer>
            )}
          </PanelBody>
        </Panel>
        <Panel>
          <PanelHeader
            icon={HardDrive}
            title="Storage"
            description="Capacity of the fullest filesystem, and what the disks are doing"
          />
          <PanelBody>
            {storage.rows.length === 0 ? (
              <ChartPlaceholder note={placeholder} className="h-[170px]" />
            ) : (
              // Two charts rather than one with two axes. A capacity
              // percentage and a byte rate share no scale, and overlaying them
              // on twin axes lets the reader infer a relationship between two
              // lines that have nothing to do with each other.
              <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
                <div className="space-y-1.5">
                  <p className="text-[11px] text-muted-foreground">Used capacity</p>
                  {/* One line per filesystem, not one for "the disk". A single
                      worst-of line changes which filesystem it is describing
                      without saying so: clear space on the fullest mount and it
                      drops to whatever the runner-up was, which reads as space
                      freed on a disk that did not change. */}
                  <ChartContainer config={storageConfig} className="h-[150px] w-full">
                    <LineChart data={storage.rows} margin={{ left: -22, right: 4, top: 4 }}>
                      <CartesianGrid vertical={false} strokeDasharray="3 3" opacity={0.4} />
                      <XAxis
                        dataKey="t"
                        tickLine={false}
                        axisLine={false}
                        minTickGap={48}
                        fontSize={10}
                      />
                      <YAxis
                        domain={[0, 100]}
                        tickLine={false}
                        axisLine={false}
                        fontSize={10}
                        unit="%"
                      />
                      <ChartTooltip
                        content={
                          <ChartTooltipContent
                            labelFormatter={rowLabel}
                            formatter={(value) => percent(Number(value))}
                          />
                        }
                      />
                      <ChartLegend content={<ChartLegendContent />} />
                      {storage.series.map((mount) => (
                        <Line
                          key={mount.key}
                          dataKey={mount.key}
                          type="monotone"
                          stroke={mount.color}
                          strokeWidth={1.5}
                          dot={false}
                          isAnimationActive={false}
                        />
                      ))}
                    </LineChart>
                  </ChartContainer>
                </div>

                <div className="space-y-1.5">
                  <p className="text-[11px] text-muted-foreground">Throughput</p>
                  <ChartContainer config={ioConfig} className="h-[150px] w-full">
                    <AreaChart data={rows} margin={{ left: 4, right: 4, top: 4 }}>
                      <CartesianGrid vertical={false} strokeDasharray="3 3" opacity={0.4} />
                      <XAxis
                        dataKey="t"
                        tickLine={false}
                        axisLine={false}
                        minTickGap={48}
                        fontSize={10}
                      />
                      <YAxis
                        width={56}
                        tickLine={false}
                        axisLine={false}
                        fontSize={10}
                        tickFormatter={(v) => bytes(v)}
                      />
                      <ChartTooltip
                        content={
                          <ChartTooltipContent
                            labelFormatter={rowLabel}
                            formatter={(value) => rate(Number(value))}
                          />
                        }
                      />
                      <ChartLegend content={<ChartLegendContent />} />
                      <Area
                        dataKey="diskRead"
                        type="monotone"
                        stroke="var(--color-diskRead)"
                        strokeWidth={1.5}
                        fill="var(--color-diskRead)"
                        fillOpacity={0.12}
                        isAnimationActive={false}
                      />
                      <Area
                        dataKey="diskWrite"
                        type="monotone"
                        stroke="var(--color-diskWrite)"
                        strokeWidth={1.5}
                        fill="var(--color-diskWrite)"
                        fillOpacity={0.12}
                        isAnimationActive={false}
                      />
                      {showPeaks && (
                        <Line
                          dataKey="diskReadPeak"
                          type="monotone"
                          stroke="var(--color-diskReadPeak)"
                          strokeWidth={1}
                          strokeOpacity={0.5}
                          strokeDasharray="3 3"
                          dot={false}
                          isAnimationActive={false}
                        />
                      )}
                      {showPeaks && (
                        <Line
                          dataKey="diskWritePeak"
                          type="monotone"
                          stroke="var(--color-diskWritePeak)"
                          strokeWidth={1}
                          strokeOpacity={0.5}
                          strokeDasharray="3 3"
                          dot={false}
                          isAnimationActive={false}
                        />
                      )}
                    </AreaChart>
                  </ChartContainer>
                </div>
              </div>
            )}
          </PanelBody>
        </Panel>
      </Section>

      {snapshot.cpu.perCore.length > 0 && (
        <Panel>
          <PanelHeader
            icon={Cpu}
            title="Per-core utilisation"
            description={`${snapshot.cpu.cores} logical processors · right now`}
          />
          <PanelBody>
            <PerCoreBars cores={snapshot.cpu.perCore} />
          </PanelBody>
        </Panel>
      )}

      <div className="grid items-start gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        <MountsPanel snapshot={snapshot} />
        <InterfacesPanel snapshot={snapshot} />
      </div>
    </Page>
  )
}

function Dot() {
  return <span className="text-muted-foreground/40">·</span>
}

/**
 * Chooses the window the charts cover.
 *
 * "Live" is kept as an option rather than made the only one: at two seconds a
 * sample it shows detail no stored series can, and it is the right view when
 * you are watching something happen right now. It is simply the wrong default
 * for a page you open to find out what happened while you were not here.
 */
function RangePicker({ value, onChange }: { value: RangeKey; onChange: (next: RangeKey) => void }) {
  return (
    <ToggleGroup
      type="single"
      value={value}
      // Radix reports "" when the active item is pressed again. Falling back to
      // the current value makes that a no-op instead of clearing the chart.
      onValueChange={(next) => onChange((next as RangeKey) || value)}
      variant="outline"
      size="sm"
      aria-label="Chart time range"
    >
      {RANGES.map((option) => (
        <ToggleGroupItem key={option.key} value={option.key} className="px-2.5 text-[11px]">
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

/**
 * Says which series is on screen and how coarse it is.
 *
 * A downsampled chart that does not admit to being downsampled invites the
 * wrong reading: 6-minute averages look like a calm hour unless the reader
 * knows the peak line is what a spike would show up in.
 */
function SeriesCaption({
  range,
  spec,
  recorded,
}: {
  range: RangeKey
  spec: RangeSpec
  recorded: HistoryState
}) {
  if (range === "live") {
    return <>Live feed, every 2 seconds · begins when this tab opened</>
  }
  if (recorded.disabled) {
    return <>History recording is turned off on this server (JD_METRICS_RETENTION=0)</>
  }
  const parts = [
    recorded.history
      ? `Recorded on the server · ${bucketLabel(recorded.history.stepSeconds)} averages and peaks`
      : `Recorded on the server · last ${spec.label}`,
  ]
  const coverage = coverageNote(recorded.history)
  if (coverage) parts.push(coverage)
  const retention = retentionNote(recorded.history, spec)
  if (retention) parts.push(retention)
  return <>{parts.join(" · ")}</>
}

function bucketLabel(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)} minute`
  return `${Math.round(seconds / 3600)} hour`
}

/**
 * What a chart shows when it has nothing to draw.
 *
 * Deliberately not an error: a dashboard installed ten minutes ago genuinely
 * has no week of history, and rendering that as a failure would send an
 * operator looking for a fault that is not there.
 */
function emptyChartNote(range: RangeKey, recorded: HistoryState): string {
  if (range === "live") return "Waiting for the first frame…"
  if (recorded.disabled) {
    return "History is not being recorded on this server. Set JD_METRICS_RETENTION to keep it."
  }
  if (recorded.loading) return "Loading history…"
  if (recorded.error) return "History could not be read."
  const every = recorded.history?.sampleIntervalSeconds
  return every
    ? `Nothing recorded in this window yet — the server samples every ${every}s.`
    : "Nothing recorded in this window yet."
}

function ChartPlaceholder({ note, className }: { note: string; className?: string }) {
  return (
    <div
      className={cn(
        "flex w-full items-center justify-center rounded-lg border border-dashed border-hairline bg-surface-sunken px-4 text-center text-xs text-muted-foreground",
        className,
      )}
    >
      {note}
    </div>
  )
}

/**
 * The tooltip heading.
 *
 * The axis label is deliberately terse — a bare clock, or a day and an hour —
 * so it fits at a glance; the tooltip is where the full timestamp belongs,
 * because "14:06" in a week-wide chart is not an answer to "when".
 */
function rowLabel(_: unknown, payload: readonly { payload?: unknown }[] | undefined) {
  const row = payload?.[0]?.payload as ChartRow | undefined
  return row?.at ?? ""
}

function PerCoreBars({ cores }: { cores: number[] }) {
  if (cores.length === 0) return null
  return (
    // As many columns as fit the panel, not a fixed two: the track count follows
    // the panel's own width rather than the viewport's, which is what keeps a
    // 48-thread host from becoming a 24-row column that sets the height of the
    // whole row — and lets a 2-core host spread across it instead.
    <div className="grid grid-cols-[repeat(auto-fit,minmax(9rem,1fr))] gap-x-4 gap-y-1.5">
      {cores.map((value, i) => (
        <div key={i} className="flex min-w-0 items-center gap-2">
          <span className="w-9 shrink-0 font-mono text-[10px] text-muted-foreground">cpu{i}</span>
          <Progress
            value={value}
            className={cn("h-1 flex-1", utilisationBar(utilisationTone(value)))}
          />
          <span className="numeric w-8 shrink-0 text-right font-mono text-[10px] text-muted-foreground">
            {value.toFixed(0)}%
          </span>
        </div>
      ))}
    </div>
  )
}

function MountsPanel({ snapshot }: { snapshot: Snapshot }) {
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
    <Panel>
      <PanelHeader
        icon={HardDrive}
        title="Filesystems"
        description="Expand a mount to see what is using it"
      />
      <PanelBody className="space-y-4">
        {snapshot.mounts.map((mount) => {
          const tone = utilisationTone(mount.usedPercent)
          return (
            <div key={mount.mountpoint} className="min-w-0 space-y-2">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-[13px] font-medium">{mount.mountpoint}</span>
                    <Badge variant="outline" className="shrink-0 text-[10px] font-normal">
                      {mount.fstype}
                    </Badge>
                  </div>
                  <p className="truncate font-mono text-[11px] text-muted-foreground">
                    {mount.device}
                  </p>
                </div>
                <div className="shrink-0 text-right">
                  <div className="numeric text-[13px]">
                    {bytes(mount.used)}{" "}
                    <span className="text-muted-foreground">/ {bytes(mount.total)}</span>
                  </div>
                  <p className="numeric text-[11px] text-muted-foreground">
                    {rate(mount.readRate)} read · {rate(mount.writeRate)} write
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Progress
                  value={mount.usedPercent}
                  className={cn("h-1 flex-1", utilisationBar(tone))}
                />
                <span
                  className={cn(
                    "numeric w-9 text-right font-mono text-[11px]",
                    tone === "danger"
                      ? "text-destructive"
                      : tone === "warning"
                        ? "text-warning"
                        : "text-muted-foreground",
                  )}
                >
                  {mount.usedPercent.toFixed(0)}%
                </span>
                <Button
                  size="xs"
                  variant="ghost"
                  className="text-muted-foreground"
                  disabled={scanning !== null}
                  onClick={() => scan(mount.mountpoint)}
                >
                  {scanning === mount.mountpoint ? "scanning…" : "scan"}
                </Button>
              </div>
              {breakdown[mount.mountpoint] && (
                <div className="space-y-1 rounded-lg border border-hairline bg-surface-sunken p-2">
                  {breakdown[mount.mountpoint].map((entry) => (
                    <div key={entry.path} className="flex justify-between gap-2 text-[11px]">
                      <span className="truncate font-mono">{entry.name}</span>
                      <span className="numeric shrink-0 text-muted-foreground">
                        {bytes(entry.size)}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )
        })}
        {snapshot.mounts.length === 0 && (
          <EmptyState title="No filesystems reported" icon={HardDrive} />
        )}
      </PanelBody>
    </Panel>
  )
}

function InterfacesPanel({ snapshot }: { snapshot: Snapshot }) {
  const total = useMemo(
    () => ({
      rx: snapshot.net.reduce((s, n) => s + n.bytesRecv, 0),
      tx: snapshot.net.reduce((s, n) => s + n.bytesSent, 0),
    }),
    [snapshot.net],
  )

  return (
    <Panel>
      <PanelHeader
        icon={Network}
        title="Interfaces"
        description={`${bytes(total.rx)} in · ${bytes(total.tx)} out since boot`}
      />
      <PanelBody flush>
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
                <TableCell className="numeric text-right font-mono text-xs">
                  {rate(iface.recvRate)}
                </TableCell>
                <TableCell className="numeric text-right font-mono text-xs">
                  {rate(iface.sendRate)}
                </TableCell>
                <TableCell className="numeric text-right font-mono text-xs text-muted-foreground">
                  {iface.errIn + iface.errOut}
                </TableCell>
              </TableRow>
            ))}
            {snapshot.net.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="p-0">
                  <EmptyState title="No interfaces reported" icon={Server} />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </PanelBody>
    </Panel>
  )
}
