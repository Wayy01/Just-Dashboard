"use client"

import { useMemo, useRef, useState } from "react"
import {
  Activity,
  AlertTriangle,
  Cpu,
  Gauge,
  HardDrive,
  MemoryStick,
  Network,
  Plug,
  Rocket,
  Server,
  Waves,
} from "lucide-react"
import { get } from "@/lib/api"
import { cn } from "@/lib/utils"
import { bytes, clock, duration, percent, rate, relativeTime } from "@/lib/format"
import type { DirEntry, MetricEvent, Snapshot } from "@/lib/types"
import { useMetrics } from "@/hooks/use-metrics"
import {
  useHealth,
  useMetricEvents,
  useMetricsHistory,
  useStorageHistory,
  type HistoryState,
} from "@/hooks/use-metrics-history"
import { useMetricsWindow } from "@/hooks/use-metrics-window"
import {
  coverageNote,
  historyRows,
  liveRows,
  liveStorageRows,
  storageRows,
  rangeSpec,
  retentionNote,
  windowSeconds,
  type ChartRow,
  type MetricsWindow,
  type StorageSeriesMeta,
} from "@/lib/metrics-range"
import { Page, PageHeader, Metric, MetricStrip, Section } from "@/components/page"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { StatTile, utilisationBar, utilisationTone } from "@/components/stat-tile"
import { ChartPanel } from "@/components/metrics/chart-panel"
import { HealthBadge, HealthPanel } from "@/components/metrics/health-panel"
import { RangePicker, windowSpanNote } from "@/components/metrics/range-picker"
import { eventColor } from "@/components/metrics/metric-chart"
import type { Series } from "@/components/metrics/metric-chart"
import { EmptyState, ErrorState } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
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
const cpuSeries: Series[] = [
  { key: "cpu", label: "CPU", color: "var(--chart-1)", kind: "area", peakKey: "cpuPeak" },
]

/**
 * The same processor time, split by what it was spent on.
 *
 * `steal` and `iowait` are the reason this view exists. A single "68% busy"
 * figure cannot distinguish a server doing work from one waiting on a disk
 * from one whose hypervisor is running another tenant on the core — and the
 * response to each is completely different. Stacked, because the four parts
 * are shares of one whole rather than four independent measurements.
 */
const cpuModeSeries: Series[] = [
  { key: "cpuUser", label: "User", color: "var(--chart-1)", stack: "cpu" },
  { key: "cpuSystem", label: "System", color: "var(--chart-2)", stack: "cpu" },
  { key: "cpuIowait", label: "I/O wait", color: "var(--chart-4)", stack: "cpu" },
  { key: "cpuSteal", label: "Steal", color: "var(--destructive)", stack: "cpu" },
]

const memSeries: Series[] = [
  { key: "mem", label: "Memory", color: "var(--chart-2)", peakKey: "memPeak" },
  { key: "swap", label: "Swap", color: "var(--chart-4)" },
]

const netSeries: Series[] = [
  { key: "rx", label: "In", color: "var(--chart-2)", kind: "area", peakKey: "rxPeak" },
  { key: "tx", label: "Out", color: "var(--chart-5)", kind: "area", peakKey: "txPeak" },
]

const ioSeries: Series[] = [
  { key: "diskRead", label: "Read", color: "var(--chart-2)", kind: "area", peakKey: "diskReadPeak" },
  { key: "diskWrite", label: "Write", color: "var(--chart-5)", kind: "area", peakKey: "diskWritePeak" },
]

const iopsSeries: Series[] = [
  { key: "diskReads", label: "Reads/s", color: "var(--chart-2)", peakKey: "diskReadsPeak" },
  { key: "diskWrites", label: "Writes/s", color: "var(--chart-5)", peakKey: "diskWritesPeak" },
]

const latencySeries: Series[] = [
  { key: "diskAwait", label: "Latency", color: "var(--chart-4)", kind: "area", peakKey: "diskAwaitPeak" },
  { key: "diskBusy", label: "Busy", color: "var(--chart-3)" },
]

const pressureSeries: Series[] = [
  { key: "psiCpu", label: "CPU", color: "var(--chart-1)", peakKey: "psiCpuPeak" },
  { key: "psiMem", label: "Memory", color: "var(--chart-2)", peakKey: "psiMemPeak" },
  { key: "psiIo", label: "I/O", color: "var(--chart-4)", peakKey: "psiIoPeak" },
]

const loadSeries: Series[] = [
  { key: "load1", label: "1 min", color: "var(--chart-1)", peakKey: "load1Peak" },
  { key: "load5", label: "5 min", color: "var(--chart-2)" },
  { key: "load15", label: "15 min", color: "var(--chart-3)" },
]

const socketSeries: Series[] = [
  { key: "tcp", label: "TCP in use", color: "var(--chart-1)", kind: "area", peakKey: "tcpPeak" },
  { key: "tcpTimeWait", label: "TIME_WAIT", color: "var(--chart-4)" },
]

/*
 * Formatters, domains and thresholds as module constants.
 *
 * These are passed to a memoised ChartPanel, whose bail-out is a shallow prop
 * comparison — so an inline `format={fmtRate}` or `domain={PERCENT_DOMAIN}`
 * is a new identity on every render and quietly turns the memo off for that
 * panel. Hoisting them is the difference between the page's two-second tick
 * touching two panels and touching all ten.
 */
const fmtRate = (v: number) => rate(v)
const fmtPercent0 = (v: number) => percent(v, 0)
const fmtLoad = (v: number) => v.toFixed(2)
const fmtOps = (v: number) => `${Math.round(v)}/s`
const fmtMillis = (v: number) => v.toFixed(1)
const fmtCount = (v: number) => Math.round(v).toLocaleString()

/**
 * A byte figure short enough for an axis gutter.
 *
 * "585.9 KB/s" wraps to two lines and clips; "586 KB" does not, and an axis
 * only has to establish the scale. The units and the exact number belong in
 * the legend and the tooltip, which have room for them.
 */
const axisBytes = (v: number) => bytes(v, 0)

const PERCENT_DOMAIN: [number, number] = [0, 100]
const FROM_ZERO: [number, string] = [0, "auto"]

const DISK_THRESHOLD = [{ value: 85, label: "85%", tone: "warning" as const }]
const INODE_THRESHOLD = [{ value: 90, label: "90%", tone: "warning" as const }]
const PRESSURE_THRESHOLD = [{ value: 10, label: "stalling", tone: "warning" as const }]

// Shared empties. A fresh `[]` each render is a new identity, which is exactly
// the re-render these memos exist to avoid.
const NO_ROWS: ChartRow[] = []
const NO_STORAGE: { rows: Record<string, number | string | null>[]; series: StorageSeriesMeta[] } = {
  rows: [],
  series: [],
}

export default function OverviewPage() {
  // Two sources, deliberately kept apart. The live socket is owned by the
  // dashboard shell and is only ever a view of "since this tab opened"; the
  // recorded series comes from the backend, which has been sampling on its own
  // timer whether or not anyone was watching. The page defaults to the
  // recorded one, because a server that has been up for ten hours has ten
  // hours worth looking at and a chart that starts at zero every visit cannot
  // show a spike that happened overnight.
  const { host, snapshot, history, error } = useMetrics()
  const controls = useMetricsWindow()
  const win = controls.window
  const recorded = useMetricsHistory(win)
  const recordedStorage = useStorageHistory(win)
  const events = useMetricEvents(win)
  const { health, loading: healthLoading } = useHealth()

  // A zoomed window always reads the recorded series, even when the range
  // underneath was "live": dragging across the live chart is a request for the
  // stored detail of that span, which is finer than the browser's own buffer
  // once the tab has been open a while.
  const live = win.key === "live" && win.from === undefined

  // Two memos rather than one branch inside a single memo, and this is
  // load-bearing rather than tidiness.
  //
  // One memo has to depend on everything either branch reads — including the
  // live socket buffer, which is replaced every two seconds. On a recorded
  // range that rebuilt all ~240 rows of chart data forty times a minute for a
  // series that had not changed, handed every chart a new `data` identity, and
  // made recharts re-render ten charts and ~38 series each time. That is what
  // made the wider windows feel heavy; the pointer was only ever adding to an
  // already saturated frame.
  //
  // Split, each memo returns a shared empty constant when its branch is not
  // the one on screen, so the idle branch costs an identity comparison.
  const liveChartRows = useMemo(() => (live ? liveRows(history) : NO_ROWS), [live, history])
  const recordedChartRows = useMemo(
    () => (!live && recorded.history ? historyRows(recorded.history) : NO_ROWS),
    [live, recorded.history],
  )
  const rows = live ? liveChartRows : recordedChartRows

  // The live feed carries only the fullest figure per frame, so the live view
  // is one honestly-labelled line and the recorded views are one per mount.
  const liveStorage = useMemo(
    () => (live ? liveStorageRows(history, snapshot?.mounts ?? []) : NO_STORAGE),
    [live, history, snapshot?.mounts],
  )
  const recordedStorageRows = useMemo(
    () => (!live && recordedStorage.storage ? storageRows(recordedStorage.storage) : NO_STORAGE),
    [live, recordedStorage.storage],
  )
  const storage = live ? liveStorage : recordedStorageRows

  const storageSeries = useMemo<Series[]>(
    () => storage.series.map((m) => ({ key: m.key, label: m.mountpoint, color: m.color })),
    [storage.series],
  )
  const inodeSeries = useMemo<Series[]>(
    () => storage.series.map((m) => ({ key: `${m.key}i`, label: m.mountpoint, color: m.color })),
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
        <Skeleton className="h-[16rem] rounded-xl" />
      </Page>
    )
  }

  const modes = snapshot.cpu.modes
  // The tiles are "right now", so they read the newest snapshot rather than
  // the chart series — which, on a recorded range, is a six-minute average.
  const throughput = {
    rx: snapshot.net.reduce((sum, n) => sum + n.recvRate, 0),
    tx: snapshot.net.reduce((sum, n) => sum + n.sendRate, 0),
  }
  const availPercent =
    snapshot.memory.total > 0 ? (snapshot.memory.available / snapshot.memory.total) * 100 : 0
  const cores = snapshot.cpu.cores || 1
  const showPeaks = !live
  const note = emptyChartNote(live, recorded)
  const zoom = controls.zoomTo
  // The one threshold that depends on the host rather than on the metric, so
  // it cannot be a module constant. Rebuilt only when the core count changes,
  // which is to say never.
  const loadThreshold = coreThreshold(cores)

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
            {health && <HealthBadge status={health.status} />}
          </span>
        }
        actions={
          <MetricStrip>
            <Metric label="Uptime" value={duration(snapshot.uptimeSeconds)} />
            <Metric label="Processes" value={snapshot.procs?.total || host.processes} />
            <Metric label="Cores" value={cores} />
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
          // The breakdown, not the load average, because the breakdown is what
          // changes the answer: 60% busy is fine, 60% busy of which 40 is
          // steal is somebody else's server.
          hint={
            modes
              ? `${modes.user.toFixed(0)}% user · ${modes.system.toFixed(0)}% sys · ${modes.iowait.toFixed(0)}% wait`
              : `load ${snapshot.cpu.loadAvg1.toFixed(2)}`
          }
          trailing={
            modes && modes.steal >= 1 ? (
              <span className="numeric text-[11px] font-medium text-destructive">
                {percent(modes.steal, 0)} steal
              </span>
            ) : (
              <span className="text-[11px] text-muted-foreground">{cores} cores</span>
            )
          }
        />
        <StatTile
          label="Memory"
          icon={MemoryStick}
          // Available, not used. On Linux "used" counts the page cache, which
          // the kernel hands straight back on demand — a dashboard that leads
          // with it reports a permanent, meaningless 90%.
          value={bytes(snapshot.memory.available)}
          meter={100 - availPercent}
          tone={availPercent <= 5 ? "danger" : availPercent <= 10 ? "warning" : "default"}
          hint={`${percent(snapshot.memory.usedPercent, 0)} used · ${bytes(snapshot.memory.cached)} cached`}
          trailing={<span className="text-[11px] text-muted-foreground">available</span>}
        />
        <StatTile
          label="Load"
          icon={Gauge}
          value={snapshot.cpu.loadAvg1.toFixed(2)}
          // Against the core count, which is the only thing that makes a load
          // average mean anything: 8.0 is a crisis on two cores and idle on
          // thirty-two.
          meter={(snapshot.cpu.loadAvg1 / cores) * 100}
          tone={utilisationTone((snapshot.cpu.loadAvg5 / cores) * 100)}
          hint={`${snapshot.cpu.loadAvg5.toFixed(2)} · ${snapshot.cpu.loadAvg15.toFixed(2)} over 5 and 15 min`}
          trailing={
            <span className="numeric text-[11px] text-muted-foreground">
              {(snapshot.cpu.loadAvg1 / cores).toFixed(2)}/core
            </span>
          }
        />
        <StatTile
          label="Network"
          icon={Waves}
          value={rate(throughput.rx)}
          hint={`${rate(throughput.tx)} out · ${snapshot.sockets?.tcpInUse ?? 0} TCP sockets`}
          trailing={<span className="text-[11px] text-muted-foreground">in</span>}
        />
      </div>

      {/* Above the charts when there is something wrong, because a warning
          below the fold is a warning nobody reads. It stays on the page when
          everything is fine — a green verdict from a checker that ran is worth
          more than the absence of a red one. */}
      <HealthPanel health={health} loading={healthLoading} />

      <Section
        title="Utilisation"
        description={<SeriesCaption live={live} win={win} recorded={recorded} />}
        actions={<RangePicker controls={controls} />}
      >
        {recorded.error && <ErrorState error={recorded.error} />}

        <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
          <ProcessorPanel
            rows={rows}
            events={events}
            onZoom={zoom}
            showPeaks={showPeaks}
            note={note}
            model={host.cpuModel}
            now={snapshot}
          />

          <ChartPanel
            icon={MemoryStick}
            title="Memory and swap"
            description="Share of total"
            rows={rows}
            series={memSeries}
            unit="%"
            domain={PERCENT_DOMAIN}
            events={events}
            onZoom={zoom}
            showPeaks={showPeaks}
            note={note}
            height={190}
            footer={
              // Equal columns rather than the header strip's left-packed run:
              // four figures across a half-page panel otherwise leave the
              // right third of the row empty.
              <MetricStrip className="[&>*]:flex-1">
                <Metric label="Used" value={bytes(snapshot.memory.used)} />
                <Metric label="Cached" value={bytes(snapshot.memory.cached)} />
                <Metric label="Buffers" value={bytes(snapshot.memory.buffers)} />
                <Metric label="Available" value={bytes(snapshot.memory.available)} />
              </MetricStrip>
            }
          />
        </div>

        <ChartPanel
          icon={Activity}
          title="Network throughput"
          description="All interfaces"
          rows={rows}
          series={netSeries}
          format={fmtRate}
          axisFormat={axisBytes}
          events={events}
          onZoom={zoom}
          showPeaks={showPeaks}
          note={note}
          height={180}
        />

        <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
          {/* Two charts rather than one with two axes. A capacity percentage
              and a byte rate share no scale, and overlaying them on twin axes
              invites the reader to infer a relationship between two lines that
              have nothing to do with each other. */}
          <ChartPanel
            icon={HardDrive}
            title="Capacity"
            // One line per filesystem, not one for "the disk". A single
            // worst-of line changes which filesystem it is describing without
            // saying so: clear space on the fullest mount and it drops to
            // whatever the runner-up was, which reads as space freed on a disk
            // that did not change.
            description="Used space per filesystem"
            rows={storage.rows as { ts: number }[]}
            series={storageSeries}
            unit="%"
            domain={PERCENT_DOMAIN}
            format={fmtPercent0}
            events={events}
            onZoom={zoom}
            showPeaks={false}
            note={note}
            height={165}
            thresholds={DISK_THRESHOLD}
          />
          <ChartPanel
            icon={HardDrive}
            title="Disk throughput"
            description="Bytes moved per second"
            rows={rows}
            series={ioSeries}
            format={fmtRate}
            axisFormat={axisBytes}
            events={events}
            onZoom={zoom}
            showPeaks={showPeaks}
            note={note}
            height={165}
          />
        </div>
      </Section>

      {/*
        Saturation is a separate question from utilisation, and the reason most
        one-server dashboards leave people stuck. "The CPU is 40% busy" and
        "requests are queueing" are both true at once far more often than they
        look like they should be, and only the charts below can say so: kernel
        pressure, the run queue, device service time and socket counts are the
        measurements that saturate before any percentage does.
      */}
      <Section
        title="Saturation"
        description="Whether work is waiting rather than running — the questions a utilisation percentage cannot answer"
      >
        <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
          <ChartPanel
            icon={AlertTriangle}
            title="Pressure"
            description={
              snapshot.pressure?.supported
                ? "Share of time work was stalled waiting for a resource"
                : "Not reported by this kernel"
            }
            rows={rows}
            series={pressureSeries}
            unit="%"
            domain={FROM_ZERO}
            events={events}
            onZoom={zoom}
            showPeaks={showPeaks}
            height={165}
            thresholds={PRESSURE_THRESHOLD}
            note={
              snapshot.pressure?.supported === false
                ? "This kernel does not expose /proc/pressure. Pressure needs Linux 4.20 or newer with PSI enabled."
                : note
            }
          />
          <ChartPanel
            icon={Gauge}
            title="Load average"
            description={`Queued work against ${cores} ${cores === 1 ? "core" : "cores"}`}
            rows={rows}
            series={loadSeries}
            format={fmtLoad}
            events={events}
            onZoom={zoom}
            showPeaks={showPeaks}
            height={165}
            // The line where the run queue is exactly as long as the machine
            // can serve. Without it a load average is a number with no scale.
            thresholds={loadThreshold}
            note={live ? "Load averages are read from the recorded series — pick a range above." : note}
          />
          <ChartPanel
            icon={HardDrive}
            title="Disk operations"
            description="Requests per second, which is what a device actually runs out of"
            rows={rows}
            series={iopsSeries}
            format={fmtOps}
            events={events}
            onZoom={zoom}
            showPeaks={showPeaks}
            height={165}
            note={note}
          />
          <ChartPanel
            icon={Waves}
            title="Disk latency and busy time"
            description="Milliseconds per request on the slowest device, and its iostat %util"
            rows={rows}
            series={latencySeries}
            format={fmtMillis}
            events={events}
            onZoom={zoom}
            showPeaks={showPeaks}
            height={165}
            note={note}
          />
        </div>

        <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
          <ChartPanel
            icon={Plug}
            title="Sockets"
            description="Open TCP connections, and how many are waiting out their close"
            rows={rows}
            series={socketSeries}
            format={fmtCount}
            events={events}
            onZoom={zoom}
            showPeaks={showPeaks}
            height={165}
            note={note}
          />
          <InodePanel
            rows={storage.rows as { ts: number }[]}
            series={inodeSeries}
            events={events}
            onZoom={zoom}
            // The live frame carries only the fullest mount's capacity, not a
            // per-mount inode count, so this series exists in the recorded
            // history alone and says so rather than showing "waiting for the
            // first frame" forever.
            note={
              live
                ? "Inode usage is only in the recorded history — pick a range above."
                : note
            }
          />
        </div>
      </Section>

      <div className="grid items-start gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        <EventsPanel events={events} window={win} />
        {snapshot.cpu.perCore.length > 0 && (
          <Panel>
            <PanelHeader
              icon={Cpu}
              title="Per-core utilisation"
              description={`${cores} logical processors · right now`}
            />
            <PanelBody>
              <PerCoreBars cores={snapshot.cpu.perCore} />
            </PanelBody>
          </Panel>
        )}
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        <MountsPanel snapshot={snapshot} />
        <InterfacesPanel snapshot={snapshot} />
      </div>
    </Page>
  )
}

/**
 * The processor panel, which can be read two ways.
 *
 * "Total" is the familiar one line. "Breakdown" is the one that answers the
 * question the total raises: a machine pinned at 90% is either working hard,
 * waiting on a disk, or losing its cores to another tenant, and those are
 * three different problems with three different responses. Offered as a toggle
 * rather than two panels because they are the same measurement — showing both
 * at once would put the same area on screen twice.
 */
function ProcessorPanel({
  rows,
  events,
  onZoom,
  showPeaks,
  note,
  model,
  now,
}: {
  rows: ChartRow[]
  events: MetricEvent[]
  onZoom: (from: number, to: number) => void
  showPeaks: boolean
  note: string
  model: string
  now: Snapshot
}) {
  const [view, setView] = useState<"total" | "modes">("total")
  const breakdown = view === "modes"

  return (
    <ChartPanel
      icon={Cpu}
      title="Processor"
      description={model}
      rows={rows}
      series={breakdown ? cpuModeSeries : cpuSeries}
      unit="%"
      domain={PERCENT_DOMAIN}
      events={events}
      onZoom={onZoom}
      showPeaks={showPeaks && !breakdown}
      stacked={breakdown}
      note={note}
      height={190}
      actions={
        <div className="flex items-center gap-2">
          <span className="numeric text-sm font-medium">{percent(now.cpu.totalPercent)}</span>
          <ToggleGroup
            type="single"
            value={view}
            onValueChange={(next) => setView((next as "total" | "modes") || view)}
            variant="outline"
            size="sm"
            aria-label="Processor view"
          >
            <ToggleGroupItem value="total" className="px-2 text-[11px]">
              Total
            </ToggleGroupItem>
            <ToggleGroupItem value="modes" className="px-2 text-[11px]">
              Breakdown
            </ToggleGroupItem>
          </ToggleGroup>
        </div>
      }
    />
  )
}

/**
 * Inode consumption per filesystem.
 *
 * Its own panel rather than a second line on the capacity chart: they are both
 * percentages but of completely different things, and a mount at 30% of its
 * bytes and 96% of its inodes needs those read as two facts. That mount is
 * about to start refusing writes while every capacity chart in the product
 * calls it empty.
 */
function InodePanel({
  rows,
  series,
  events,
  onZoom,
  note,
}: {
  rows: { ts: number }[]
  series: Series[]
  events: MetricEvent[]
  onZoom: (from: number, to: number) => void
  note: string
}) {
  return (
    <ChartPanel
      icon={HardDrive}
      title="Inodes"
      description="A filesystem can run out of these while reporting free space"
      rows={rows}
      series={series}
      unit="%"
      domain={PERCENT_DOMAIN}
      format={fmtPercent0}
      events={events}
      onZoom={onZoom}
      showPeaks={false}
      height={165}
      thresholds={INODE_THRESHOLD}
      note={note}
    />
  )
}

/**
 * What happened, as a list rather than only as marks on the charts.
 *
 * The markers answer "was anything going on at 03:14"; this answers "what has
 * been going on at all", which is the question you have before you know where
 * to look. Both read the same events, so a line on a chart and a row here are
 * always the same fact.
 */
function EventsPanel({ events, window: win }: { events: MetricEvent[]; window: MetricsWindow }) {
  const newestFirst = useMemo(() => [...events].reverse(), [events])

  return (
    <Panel>
      <PanelHeader
        icon={Rocket}
        title="What happened"
        description="Deploys, backups, restarts and the actions that change things"
      />
      <PanelBody className={newestFirst.length === 0 ? undefined : "max-h-[19rem] overflow-y-auto"}>
        {newestFirst.length === 0 ? (
          <p className="text-[13px] text-muted-foreground">
            Nothing recorded in the last {rangeSpec(win.key).label === "Live" ? "hour" : rangeSpec(win.key).label}.
          </p>
        ) : (
          <ol className="space-y-2">
            {newestFirst.map((event, i) => (
              <li key={`${event.ts}-${i}`} className="flex min-w-0 gap-2.5">
                <span
                  aria-hidden
                  className="mt-1.5 size-1.5 shrink-0 rounded-full"
                  style={{ background: eventColor(event) }}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                    <span className="truncate text-[13px]">{event.title}</span>
                    <Badge variant="outline" className="shrink-0 text-[10px] font-normal">
                      {event.kind}
                    </Badge>
                  </div>
                  <p className="truncate text-[11px] text-muted-foreground">
                    {relativeTime(event.ts)}
                    {event.detail ? ` · ${event.detail}` : ""}
                    {event.durationSeconds ? ` · took ${duration(event.durationSeconds)}` : ""}
                  </p>
                </div>
                {/* The clock, not a slice of the localised timestamp: that
                    string is "Aug 21, 2026, 16:47:15" in some locales and
                    "21/08/2026, 16:47" in others, so cutting it on a comma
                    prints the year about as often as the time. */}
                <span className="numeric shrink-0 text-[11px] text-muted-foreground">
                  {clock(event.ts)}
                </span>
              </li>
            ))}
          </ol>
        )}
      </PanelBody>
    </Panel>
  )
}

/**
 * The "one runnable task per core" line, cached per core count.
 *
 * A fresh array literal each render would be a new prop identity and would
 * switch off the memo on the panel it is passed to. There is exactly one host,
 * so a single-entry cache is the whole of what this needs.
 */
let coreThresholdCache: { cores: number; value: { value: number; label: string; tone: "warning" }[] } | null = null
function coreThreshold(cores: number) {
  if (!coreThresholdCache || coreThresholdCache.cores !== cores) {
    coreThresholdCache = {
      cores,
      value: [{ value: cores, label: `${cores} cores`, tone: "warning" }],
    }
  }
  return coreThresholdCache.value
}

function Dot() {
  return <span className="text-muted-foreground/40">·</span>
}

/**
 * Says which series is on screen and how coarse it is.
 *
 * A downsampled chart that does not admit to being downsampled invites the
 * wrong reading: 6-minute averages look like a calm hour unless the reader
 * knows the peak line is what a spike would show up in.
 */
function SeriesCaption({
  live,
  win,
  recorded,
}: {
  live: boolean
  win: MetricsWindow
  recorded: HistoryState
}) {
  if (live) {
    return (
      <>Live feed, every 2 seconds · begins when this tab opened · drag across a chart to zoom</>
    )
  }
  if (recorded.disabled) {
    return <>History recording is turned off on this server (JD_METRICS_RETENTION=0)</>
  }
  const parts: string[] = []
  const span = windowSpanNote(win)
  if (span) parts.push(span)
  parts.push(
    recorded.history
      ? `${bucketLabel(recorded.history.stepSeconds)} averages and peaks`
      : `last ${rangeSpec(win.key).label}`,
  )
  const coverage = coverageNote(recorded.history)
  if (coverage) parts.push(coverage)
  const retention = retentionNote(recorded.history, {
    ...rangeSpec(win.key),
    seconds: windowSeconds(win),
  })
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
function emptyChartNote(live: boolean, recorded: HistoryState): string {
  if (live) return "Waiting for the first frame…"
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
          const inodes =
            mount.inodesTotal > 0 ? (mount.inodesUsed / mount.inodesTotal) * 100 : 0
          return (
            <div key={mount.mountpoint} className="min-w-0 space-y-2">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-[13px] font-medium">{mount.mountpoint}</span>
                    <Badge variant="outline" className="shrink-0 text-[10px] font-normal">
                      {mount.fstype}
                    </Badge>
                    {/* Only when it is close enough to matter. A permanent
                        inode figure on every row is noise on the ninety-nine
                        mounts where it will never be the limit. */}
                    {inodes >= 80 && (
                      <Badge variant="outline" className="shrink-0 border-warning/40 text-[10px] font-normal text-warning">
                        {inodes.toFixed(0)}% inodes
                      </Badge>
                    )}
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
              {/* Drops next to errors: a link that is up, error-free and
                  dropping packets is a queue that is too short, not a fault,
                  and the two counters are what tell them apart. */}
              <TableHead className="text-right">Errors</TableHead>
              <TableHead className="text-right">Drops</TableHead>
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
                  {/* Bounded: a dual-stack interface lists an IPv4, an IPv6
                      and a link-local address, which together are wider than
                      the panel and push the throughput columns off the edge of
                      it. The full list is in the title attribute. */}
                  <p
                    className="max-w-[14rem] truncate font-mono text-[11px] text-muted-foreground"
                    title={iface.addrs.join(", ")}
                  >
                    {iface.addrs.join(", ") || "no address"}
                  </p>
                </TableCell>
                <TableCell className="numeric text-right font-mono text-xs">
                  {rate(iface.recvRate)}
                </TableCell>
                <TableCell className="numeric text-right font-mono text-xs">
                  {rate(iface.sendRate)}
                </TableCell>
                <TableCell
                  className={cn(
                    "numeric text-right font-mono text-xs",
                    iface.errIn + iface.errOut > 0 ? "text-warning" : "text-muted-foreground",
                  )}
                >
                  {iface.errIn + iface.errOut}
                </TableCell>
                <TableCell
                  className={cn(
                    "numeric text-right font-mono text-xs",
                    iface.dropIn + iface.dropOut > 0 ? "text-warning" : "text-muted-foreground",
                  )}
                >
                  {iface.dropIn + iface.dropOut}
                </TableCell>
              </TableRow>
            ))}
            {snapshot.net.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="p-0">
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
