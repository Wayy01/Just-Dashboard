import { clock, timestamp } from "@/lib/format"
import type {
  ContainerHistory,
  ContainerHistoryPoint,
  MetricsHistory,
  MetricsHistoryPoint,
} from "@/lib/types"
import type { MetricsPoint } from "@/lib/metrics-store"

/**
 * The windows the Overview charts can be drawn over.
 *
 * "live" is the two-second socket feed the browser accumulates while the
 * dashboard is open; everything else is read back from the server, which has
 * been sampling on its own timer whether or not anybody was looking. The two
 * are deliberately never spliced into one line: the cadences differ by two
 * orders of magnitude, and a chart that draws twenty coarse points and a
 * hundred fine ones at equal spacing is lying about when things happened.
 */
export type RangeKey = "live" | "1h" | "6h" | "24h" | "7d"

export type RangeSpec = {
  key: RangeKey
  label: string
  /** What the server is asked for. Absent for the live buffer. */
  query?: string
  /** How many buckets to request — roughly one per two horizontal pixels. */
  points: number
  /** How often to re-read. A week of history does not change every 15 seconds. */
  refreshMs: number
  /** Width of the window, used for the "recording since" note. */
  seconds: number
}

export const RANGES: RangeSpec[] = [
  { key: "live", label: "Live", points: 0, refreshMs: 0, seconds: 300 },
  { key: "1h", label: "1h", query: "1h", points: 240, refreshMs: 15_000, seconds: 3_600 },
  { key: "6h", label: "6h", query: "6h", points: 288, refreshMs: 60_000, seconds: 21_600 },
  { key: "24h", label: "24h", query: "24h", points: 288, refreshMs: 120_000, seconds: 86_400 },
  { key: "7d", label: "7d", query: "7d", points: 336, refreshMs: 300_000, seconds: 604_800 },
]

/**
 * The windows that come from the server.
 *
 * A container has no in-browser live buffer to fall back on — nothing
 * accumulates its stats across a page load — so its charts offer only the
 * recorded ranges rather than a "Live" option that would draw nothing.
 */
export const HISTORY_RANGES = RANGES.filter((r) => r.query)

export function rangeSpec(key: RangeKey): RangeSpec {
  return RANGES.find((r) => r.key === key) ?? RANGES[1]
}

export const DEFAULT_RANGE: RangeKey = "1h"

const STORAGE_KEY = "just-dashboard.metrics.range"

/**
 * The chosen window, held outside React so it can be read during render
 * without an effect.
 *
 * It lives in localStorage rather than sessionStorage: an operator who always
 * wants the last day should not have to say so again in every new tab. Reading
 * it through useSyncExternalStore is what keeps that honest across the server
 * render — the server has no localStorage, so it renders the default and React
 * swaps in the stored value after hydration, rather than the two disagreeing.
 */
let current: RangeKey | null = null
const rangeListeners = new Set<() => void>()

export function subscribeRange(listener: () => void) {
  rangeListeners.add(listener)
  return () => {
    rangeListeners.delete(listener)
  }
}

export function currentRange(): RangeKey {
  if (current === null) current = readStoredRange()
  return current
}

/** What the server renders. It cannot know the preference, so it draws the default. */
export function serverRange(): RangeKey {
  return DEFAULT_RANGE
}

export function storeRange(key: RangeKey) {
  if (current === key) return
  current = key
  try {
    window.localStorage.setItem(STORAGE_KEY, key)
  } catch {
    // Private-mode storage refusals cost a remembered preference, not a chart.
  }
  for (const listener of rangeListeners) listener()
}

function readStoredRange(): RangeKey {
  if (typeof window === "undefined") return DEFAULT_RANGE
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (raw && RANGES.some((r) => r.key === raw)) return raw as RangeKey
  } catch {
    // As above.
  }
  return DEFAULT_RANGE
}

/**
 * One row of chart data. Peaks are present only on server-backed ranges; the
 * live feed is already at its own resolution and has no bucket to peak within.
 *
 * A null in any series is a break in the record — the dashboard was not
 * running — and recharts draws it as a gap rather than joining the two sides.
 * That distinction matters: a straight line across four hours of downtime
 * reads as four hours of steady load.
 */
export type ChartRow = {
  t: string
  ts: number
  /** Full timestamp for the tooltip; the axis label alone is ambiguous past a day. */
  at: string
  cpu: number | null
  cpuPeak: number | null
  mem: number | null
  memPeak: number | null
  swap: number | null
  rx: number | null
  rxPeak: number | null
  tx: number | null
  txPeak: number | null
  /** Fullest filesystem, as a percentage. */
  disk: number | null
  diskRead: number | null
  diskReadPeak: number | null
  diskWrite: number | null
  diskWritePeak: number | null
}

/** Turns the live socket buffer into chart rows. No peaks, no gaps to bridge. */
export function liveRows(history: MetricsPoint[]): ChartRow[] {
  return history.map((p) => ({
    t: p.t,
    ts: p.ts,
    at: new Date(p.ts).toLocaleString(),
    cpu: p.cpu,
    cpuPeak: null,
    mem: p.mem,
    memPeak: null,
    swap: p.swap,
    rx: p.rx,
    rxPeak: null,
    tx: p.tx,
    txPeak: null,
    disk: p.disk,
    diskRead: p.dread,
    diskReadPeak: null,
    diskWrite: p.dwrite,
    diskWritePeak: null,
  }))
}

/**
 * Turns a recorded series into chart rows, inserting an explicit break
 * wherever consecutive buckets are further apart than the bucket width.
 *
 * The tolerance is generous — a bucket and a half — because a sampler that
 * drifts by a few hundred milliseconds is still running, and punching a hole
 * in the chart for that would make every graph look like an outage.
 */
export function historyRows(history: MetricsHistory): ChartRow[] {
  const step = Math.max(history.stepSeconds, 1) * 1000
  const rows: ChartRow[] = []
  let previous = 0

  for (const point of history.points) {
    const ts = new Date(point.ts).getTime()
    if (Number.isNaN(ts)) continue
    if (previous !== 0 && ts - previous > step * 1.5) rows.push(gapRow(previous + step))
    rows.push(toRow(point, ts, step))
    previous = ts
  }
  return rows
}

function toRow(point: MetricsHistoryPoint, ts: number, step: number): ChartRow {
  return {
    // Past a day the bare clock is ambiguous, so the axis carries the date too
    // once the buckets are wide enough for that to be the reading.
    t: step >= 3_600_000 ? shortDateTime(ts) : clock(point.ts),
    ts,
    at: timestamp(point.ts),
    cpu: point.cpu,
    cpuPeak: point.cpuPeak,
    mem: point.mem,
    memPeak: point.memPeak,
    swap: point.swap,
    rx: point.rx,
    rxPeak: point.rxPeak,
    tx: point.tx,
    txPeak: point.txPeak,
    disk: point.diskPercent,
    diskRead: point.diskRead,
    diskReadPeak: point.diskReadPeak,
    diskWrite: point.diskWrite,
    diskWritePeak: point.diskWritePeak,
  }
}

function gapRow(ts: number): ChartRow {
  return {
    t: "",
    ts,
    at: "",
    cpu: null,
    cpuPeak: null,
    mem: null,
    memPeak: null,
    swap: null,
    rx: null,
    rxPeak: null,
    tx: null,
    txPeak: null,
    disk: null,
    diskRead: null,
    diskReadPeak: null,
    diskWrite: null,
    diskWritePeak: null,
  }
}

function shortDateTime(ts: number): string {
  return new Date(ts).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  })
}

/**
 * The frame shared by every recorded series — the host's and a container's
 * alike. Both endpoints return the same envelope, so the notes below read it
 * structurally rather than caring which series they were handed.
 */
export type HistoryWindow = {
  from: string
  earliest: string | null
  stepSeconds: number
  retentionSeconds: number
}

/**
 * How much of the requested window the record actually covers.
 *
 * A dashboard installed this morning cannot answer a question about last
 * Tuesday, and the honest response is to say so — an empty left-hand half of a
 * 7d chart otherwise reads as a week of zero load.
 */
export function coverageNote(history: HistoryWindow | undefined): string | null {
  if (!history || !history.earliest) return null
  const earliest = new Date(history.earliest).getTime()
  if (Number.isNaN(earliest)) return null
  const from = new Date(history.from).getTime()
  // A minute of slack: the first sample is always a little after the window
  // opens, and reporting that as incomplete coverage would be pedantic.
  if (earliest <= from + 60_000) return null
  return `recorded from ${timestamp(history.earliest)}`
}

/** The retention ceiling, phrased for the range that ran into it. */
export function retentionNote(history: HistoryWindow | undefined, spec: RangeSpec): string | null {
  if (!history) return null
  if (history.retentionSeconds >= spec.seconds) return null
  return `history is kept for ${Math.round(history.retentionSeconds / 3600)}h`
}

/** One row of a container's chart data, with the same gap semantics as the host rows. */
export type ContainerRow = {
  t: string
  ts: number
  at: string
  cpu: number | null
  cpuPeak: number | null
  mem: number | null
  memPeak: number | null
}

export function containerRows(history: ContainerHistory): ContainerRow[] {
  const step = Math.max(history.stepSeconds, 1) * 1000
  const rows: ContainerRow[] = []
  let previous = 0

  for (const point of history.points) {
    const ts = new Date(point.ts).getTime()
    if (Number.isNaN(ts)) continue
    // A container that was stopped and started again leaves a real hole here,
    // and it is worth seeing as a hole: the flat line a chart would otherwise
    // draw across it says the container was idle when it was not running.
    if (previous !== 0 && ts - previous > step * 1.5) rows.push(containerGapRow(previous + step))
    rows.push(toContainerRow(point, ts, step))
    previous = ts
  }
  return rows
}

function toContainerRow(point: ContainerHistoryPoint, ts: number, step: number): ContainerRow {
  return {
    t: step >= 3_600_000 ? shortDateTime(ts) : clock(point.ts),
    ts,
    at: timestamp(point.ts),
    cpu: point.cpu,
    cpuPeak: point.cpuPeak,
    mem: point.memBytes,
    memPeak: point.memBytesPeak,
  }
}

function containerGapRow(ts: number): ContainerRow {
  return { t: "", ts, at: "", cpu: null, cpuPeak: null, mem: null, memPeak: null }
}

/** The memory ceiling to draw the limit line at, or 0 when the container has none. */
export function memoryLimit(history: ContainerHistory | undefined): number {
  if (!history) return 0
  let limit = 0
  for (const p of history.points) {
    if (p.memLimit > limit) limit = p.memLimit
  }
  return limit
}
