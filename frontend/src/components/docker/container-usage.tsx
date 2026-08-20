"use client"

import { useMemo, useState } from "react"
import { Area, AreaChart, CartesianGrid, ReferenceLine, XAxis, YAxis } from "recharts"
import { get, ApiError } from "@/lib/api"
import { bytes, percent } from "@/lib/format"
import {
  containerRows,
  coverageNote,
  HISTORY_RANGES,
  memoryLimit,
  rangeSpec,
  retentionNote,
  type ContainerRow,
  type RangeKey,
} from "@/lib/metrics-range"
import type { ContainerHistory } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Metric, MetricStrip } from "@/components/page"
import { ErrorState } from "@/components/state"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Cpu, MemoryStick } from "lucide-react"
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"

const cpuConfig = {
  cpu: { label: "CPU", color: "var(--chart-1)" },
  cpuPeak: { label: "CPU peak", color: "var(--chart-1)" },
} satisfies ChartConfig

const memConfig = {
  mem: { label: "Memory", color: "var(--chart-2)" },
  memPeak: { label: "Memory peak", color: "var(--chart-2)" },
} satisfies ChartConfig

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
  const [range, setRange] = useState<RangeKey>("1h")
  const spec = rangeSpec(range)

  const { data, error, loading } = usePoll<ContainerHistory>(
    (signal) =>
      get<ContainerHistory>(
        `/docker/containers/${encodeURIComponent(containerId)}/stats/history`,
        { range: spec.query, points: spec.points },
        signal,
      ),
    spec.refreshMs,
    [containerId, spec.query],
  )

  const rows = useMemo<ContainerRow[]>(() => (data ? containerRows(data) : []), [data])
  const limit = memoryLimit(data)
  const disabled = error instanceof ApiError && error.code === "metrics_history_disabled"
  const peaks = useMemo(() => summarise(rows), [rows])

  const picker = (
    <ToggleGroup
      type="single"
      value={range}
      onValueChange={(next) => setRange((next as RangeKey) || range)}
      variant="outline"
      size="sm"
      aria-label="History range"
    >
      {HISTORY_RANGES.map((option) => (
        <ToggleGroupItem key={option.key} value={option.key} className="px-2.5 text-[11px]">
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )

  if (disabled) {
    return (
      <Panel>
        <PanelHeader icon={Cpu} title="Usage history" />
        <PanelBody>
          <Placeholder note="History is not being recorded on this server. Set JD_METRICS_RETENTION to keep it." />
        </PanelBody>
      </Panel>
    )
  }

  if (error) return <ErrorState error={error} />

  const empty = rows.length === 0
  const note = loading
    ? "Loading history…"
    : `Nothing recorded for ${name} in this window yet — the server samples every ${data?.sampleIntervalSeconds ?? 15}s.`

  return (
    <div className="space-y-3">
      <Panel>
        <PanelHeader
          icon={Cpu}
          title="Processor"
          description={caption(data, spec.label)}
          actions={picker}
        />
        <PanelBody className="space-y-3">
          {empty ? (
            <Placeholder note={note} />
          ) : (
            <ChartContainer config={cpuConfig} className="h-[170px] w-full">
              <AreaChart data={rows} margin={{ left: -16, right: 4, top: 4 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" opacity={0.4} />
                <XAxis dataKey="t" tickLine={false} axisLine={false} minTickGap={48} fontSize={10} />
                {/* Not capped at 100: a container using two cores is at 200%,
                    and clipping that would hide the thing worth seeing. */}
                <YAxis tickLine={false} axisLine={false} fontSize={10} unit="%" />
                <ChartTooltip
                  content={
                    <ChartTooltipContent
                      labelFormatter={rowLabel}
                      formatter={(value) => percent(Number(value))}
                    />
                  }
                />
                <ChartLegend content={<ChartLegendContent />} />
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
          <MetricStrip className="[&>*]:flex-1">
            <Metric label="Peak CPU" value={peaks.cpu === null ? "—" : percent(peaks.cpu)} />
            <Metric label="Peak memory" value={peaks.mem === null ? "—" : bytes(peaks.mem)} />
            <Metric label="Limit" value={limit > 0 ? bytes(limit) : "none"} />
          </MetricStrip>
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader
          icon={MemoryStick}
          title="Memory"
          description={
            limit > 0
              ? `Against a ${bytes(limit)} limit`
              : "No limit set — bounded only by the host"
          }
        />
        <PanelBody>
          {empty ? (
            <Placeholder note={note} />
          ) : (
            <ChartContainer config={memConfig} className="h-[170px] w-full">
              <AreaChart data={rows} margin={{ left: 4, right: 4, top: 4 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" opacity={0.4} />
                <XAxis dataKey="t" tickLine={false} axisLine={false} minTickGap={48} fontSize={10} />
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
                      formatter={(value) => bytes(Number(value))}
                    />
                  }
                />
                <ChartLegend content={<ChartLegendContent />} />
                {/* The limit is the line that explains an OOM kill, so it is
                    drawn even when the series never gets near it. */}
                {limit > 0 && (
                  <ReferenceLine
                    y={limit}
                    stroke="var(--destructive)"
                    strokeDasharray="4 4"
                    strokeOpacity={0.7}
                  />
                )}
                <Area
                  dataKey="memPeak"
                  type="monotone"
                  stroke="var(--color-memPeak)"
                  strokeWidth={1}
                  strokeOpacity={0.45}
                  fill="var(--color-memPeak)"
                  fillOpacity={0.07}
                  isAnimationActive={false}
                />
                <Area
                  dataKey="mem"
                  type="monotone"
                  stroke="var(--color-mem)"
                  strokeWidth={1.5}
                  fill="var(--color-mem)"
                  fillOpacity={0.14}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ChartContainer>
          )}
        </PanelBody>
      </Panel>
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

function rowLabel(_: unknown, payload: readonly { payload?: unknown }[] | undefined) {
  const row = payload?.[0]?.payload as ContainerRow | undefined
  return row?.at ?? ""
}

function Placeholder({ note }: { note: string }) {
  return (
    <div className="flex h-[170px] w-full items-center justify-center rounded-lg border border-dashed border-hairline bg-surface-sunken px-4 text-center text-xs text-muted-foreground">
      {note}
    </div>
  )
}
