"use client"

import { useMemo } from "react"
import { get, ApiError } from "@/lib/api"
import { bytes, percent, rate } from "@/lib/format"
import {
  containerRows,
  coverageNote,
  HISTORY_RANGES,
  memoryLimit,
  rangeSpec,
  retentionNote,
  windowQuery,
  windowRefreshMs,
  type ContainerRow,
} from "@/lib/metrics-range"
import type { ContainerHistory } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useMetricEvents } from "@/hooks/use-metrics-history"
import { useMetricsWindow } from "@/hooks/use-metrics-window"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Metric, MetricStrip } from "@/components/page"
import { ErrorState } from "@/components/state"
import { ChartPanel, ChartPlaceholder } from "@/components/metrics/chart-panel"
import { RangePicker } from "@/components/metrics/range-picker"
import type { Series } from "@/components/metrics/metric-chart"
import { ChartActivity, Cpu, GridSquare, Servers } from "@/components/icons"

const cpuSeries: Series[] = [
  { key: "cpu", label: "CPU", color: "var(--chart-1)", kind: "area", peakKey: "cpuPeak" },
]

const memSeries: Series[] = [
  { key: "mem", label: "Memory", color: "var(--chart-2)", kind: "area", peakKey: "memPeak" },
]

const netSeries: Series[] = [
  { key: "netRx", label: "In", color: "var(--chart-2)", kind: "area" },
  { key: "netTx", label: "Out", color: "var(--chart-5)", kind: "area" },
]

const blockSeries: Series[] = [
  { key: "blockRead", label: "Read", color: "var(--chart-2)", kind: "area" },
  { key: "blockWrite", label: "Write", color: "var(--chart-5)", kind: "area" },
]

/**
 * What this container was doing before you opened the panel.
 *
 * The live stats socket, like the host one, only ever describes the time since
 * this panel was mounted — so a container that was killed for exceeding its
 * memory limit at 03:00, or that pinned a core for twenty minutes overnight,
 * left no trace anywhere in the dashboard. These charts read the series the
 * backend has been recording on its own timer.
 *
 * There is no "Live" option here on purpose: nothing accumulates a container's
 * stats across a page load, so it would draw an empty chart that fills in over
 * the next few minutes — the exact behaviour this panel exists to replace.
 */
export function ContainerUsage({ containerId, name }: { containerId: string; name: string }) {
  const controls = useMetricsWindow()
  const win = controls.window
  // The host's shared range preference starts on "1h", but a container has no
  // live series to fall back on, so a "live" preference has to resolve to the
  // narrowest recorded window rather than to nothing at all.
  const effective = win.key === "live" && win.from === undefined ? { ...win, key: "1h" as const } : win
  const params = windowQuery(effective, rangeSpec(effective.key).points)
  const signature = JSON.stringify(params)

  const { data, error, loading } = usePoll<ContainerHistory>(
    (signal) =>
      get<ContainerHistory>(
        `/docker/containers/${encodeURIComponent(containerId)}/stats/history`,
        params,
        signal,
      ),
    windowRefreshMs(effective),
    [containerId, signature],
  )

  // The same deploys, restarts and reboots the host charts are marked with.
  // A container's memory falling off a cliff is a different event depending on
  // whether the stack was redeployed a second earlier, and that is exactly the
  // fact these markers carry.
  const events = useMetricEvents(effective)

  const rows = useMemo<ContainerRow[]>(() => (data ? containerRows(data) : []), [data])
  const limit = memoryLimit(data)
  // Docker omits `networks` entirely for a container sharing the host's
  // network namespace — there is no per-container interface to measure. That
  // is an absence of the measurement, not a container doing nothing, and a
  // flat line at zero says the wrong one of those.
  const hasNetwork = useMemo(
    () => rows.some((r) => (r.netRx ?? 0) > 0 || (r.netTx ?? 0) > 0),
    [rows],
  )
  const disabled = error instanceof ApiError && error.code === "metrics_history_disabled"
  const peaks = useMemo(() => summarise(rows), [rows])

  if (disabled) {
    return (
      <Panel>
        <PanelHeader icon={Cpu} title="Usage history" />
        <PanelBody>
          <ChartPlaceholder note="History is not being recorded on this server. Set JD_METRICS_RETENTION to keep it." />
        </PanelBody>
      </Panel>
    )
  }

  if (error) return <ErrorState error={error} />

  const note = loading
    ? "Loading history…"
    : `Nothing recorded for ${name} in this window yet — the server samples every ${data?.sampleIntervalSeconds ?? 15}s.`

  return (
    <div className="space-y-3">
      <ChartPanel
        icon={Cpu}
        title="Processor"
        description={caption(data, rangeSpec(effective.key).label)}
        actions={<RangePicker controls={controls} ranges={HISTORY_RANGES} />}
        rows={rows}
        series={cpuSeries}
        unit="%"
        // Not capped at 100: a container using two cores is at 200%, and
        // clipping that would hide the thing worth seeing.
        format={(v) => percent(v)}
        events={events}
        onZoom={controls.zoomTo}
        note={note}
        height={170}
        footer={
          <MetricStrip className="[&>*]:flex-1">
            <Metric label="Peak CPU" value={peaks.cpu === null ? "—" : percent(peaks.cpu)} />
            <Metric label="Peak memory" value={peaks.mem === null ? "—" : bytes(peaks.mem)} />
            <Metric label="Limit" value={limit > 0 ? bytes(limit) : "none"} />
          </MetricStrip>
        }
      />

      <ChartPanel
        icon={GridSquare}
        title="Memory"
        description={
          limit > 0 ? `Against a ${bytes(limit)} limit` : "No limit set — bounded only by the host"
        }
        rows={rows}
        series={memSeries}
        format={(v) => bytes(v)}
        axisFormat={(v) => bytes(v, 0)}
        events={events}
        onZoom={controls.zoomTo}
        note={note}
        height={170}
        // Scaled to the limit rather than to the data. A container sitting at
        // a quarter of its ceiling draws a short line, which is the useful
        // picture: an axis fitted to the series makes every container look
        // equally close to being killed, and pushes the limit line off the top
        // of the chart where recharts silently discards it.
        domain={limit > 0 ? [0, Math.round(limit * 1.04)] : undefined}
        // The limit is the line that explains an OOM kill, so it is drawn even
        // when the series never gets near it.
        thresholds={limit > 0 ? [{ value: limit, label: "limit", tone: "danger" }] : undefined}
      />

      {/*
        Network and block throughput were being sampled for this container all
        along and thrown away at the end of every request. They are the two
        series that answer "is this the container saturating the host", which
        the CPU and memory charts on their own cannot.
      */}
      <div className="grid gap-3 lg:grid-cols-2 [&>*]:min-w-0">
        <ChartPanel
          icon={ChartActivity}
          title="Network"
          description={
            hasNetwork
              ? "Bytes per second in and out of this container"
              : "Not measured for this container"
          }
          // An all-zero series is passed as no series at all, so the panel
          // renders the explanation rather than a flat line at the bottom of
          // an axis labelled in single bytes.
          rows={hasNetwork ? rows : []}
          series={netSeries}
          format={(v) => rate(v)}
          axisFormat={(v) => bytes(v, 0)}
          events={events}
          onZoom={controls.zoomTo}
          showPeaks={false}
          note={
            hasNetwork
              ? note
              : "Docker reports no per-container interfaces here, which is what a container on the host's network namespace looks like. Its traffic is in the host's own network chart."
          }
          height={150}
        />
        <ChartPanel
          icon={Servers}
          title="Block I/O"
          description="Reads and writes against the host's devices"
          rows={rows}
          series={blockSeries}
          format={(v) => rate(v)}
          axisFormat={(v) => bytes(v, 0)}
          events={events}
          onZoom={controls.zoomTo}
          showPeaks={false}
          note={note}
          height={150}
        />
      </div>
    </div>
  )
}

/** The worst moment in the window, which is the number the charts exist to surface. */
function summarise(rows: ContainerRow[]): { cpu: number | null; mem: number | null } {
  let cpu: number | null = null
  let mem: number | null = null
  for (const row of rows) {
    if (row.cpuPeak !== null && (cpu === null || row.cpuPeak > cpu)) cpu = row.cpuPeak
    if (row.memPeak !== null && (mem === null || row.memPeak > mem)) mem = row.memPeak
  }
  return { cpu, mem }
}

function caption(history: ContainerHistory | undefined, label: string): string {
  const parts = [history ? bucketLabel(history.stepSeconds) : `last ${label}`]
  const coverage = coverageNote(history)
  if (coverage) parts.push(coverage)
  const retention = retentionNote(history, rangeSpec("7d"))
  if (retention) parts.push(retention)
  return parts.join(" · ")
}

function bucketLabel(seconds: number): string {
  if (seconds < 60) return `${seconds}s averages and peaks`
  if (seconds < 3600) return `${Math.round(seconds / 60)} minute averages and peaks`
  return `${Math.round(seconds / 3600)} hour averages and peaks`
}
