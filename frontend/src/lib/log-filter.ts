import type { LogFilterState, LogTimeRange } from "@/components/logs/types"

/**
 * The filter vocabulary, worst first. `unknown` is not a level any log writes —
 * it is the chip for the lines the parser could not classify, which on a
 * typical host is most of them. Without it, turning on any level filter
 * silently hid every line that did not happen to contain one of a dozen
 * English words, including the continuation lines of the stack trace being
 * hunted.
 */
export const LOG_LEVELS = ["critical", "error", "warn", "info", "debug", "unknown"] as const
export type LogLevel = (typeof LOG_LEVELS)[number]

export const LEVEL_LABEL: Record<LogLevel, string> = {
  critical: "critical",
  error: "error",
  warn: "warn",
  info: "info",
  debug: "debug",
  unknown: "other",
}

/** What a line's level means, one hover away — the levels are not obvious. */
export const LEVEL_HINT: Record<LogLevel, string> = {
  critical: "The process is failing or the host is: emerg, alert, crit, fatal.",
  error: "Something did not work. err and error.",
  warn: "Working, but it will not stay that way. warn and warning.",
  info: "Normal operation. notice and info.",
  debug: "Developer detail. debug and trace.",
  unknown: "No level in the line — including the continuation lines of a stack trace.",
}

export const LEVEL_TEXT: Record<string, string> = {
  critical: "text-destructive",
  error: "text-destructive",
  warn: "text-warning",
  info: "text-foreground",
  debug: "text-muted-foreground",
}

export const LEVEL_EDGE: Record<string, string> = {
  critical: "bg-destructive",
  error: "bg-destructive/70",
  warn: "bg-warning",
  info: "bg-transparent",
  debug: "bg-transparent",
}

export const EMPTY_FILTER: LogFilterState = {
  q: "",
  exclude: "",
  regex: false,
  ignoreCase: true,
  levels: [],
}

export function isFilterActive(f: LogFilterState): boolean {
  return f.q !== "" || f.exclude !== "" || f.levels.length > 0
}

/**
 * The query every log route shares. One builder means the live socket, the
 * history search and the export cannot drift apart: narrowing the stream to one
 * request id and pressing Export gives you that, not the whole file.
 */
export function filterQuery(f: LogFilterState) {
  return {
    q: f.q || undefined,
    exclude: f.exclude || undefined,
    regex: f.regex ? "true" : undefined,
    // The server defaults this on, so only the opt-out needs sending.
    ignoreCase: f.ignoreCase ? undefined : "false",
    levels: f.levels.length ? f.levels.join(",") : undefined,
  }
}

export const TIME_RANGES: { id: LogTimeRange; label: string; minutes: number | null }[] = [
  { id: "15m", label: "Last 15 minutes", minutes: 15 },
  { id: "1h", label: "Last hour", minutes: 60 },
  { id: "6h", label: "Last 6 hours", minutes: 360 },
  { id: "24h", label: "Last 24 hours", minutes: 1440 },
  { id: "7d", label: "Last 7 days", minutes: 10080 },
  { id: "all", label: "Everything on disk", minutes: null },
  { id: "custom", label: "Custom range", minutes: null },
]

/**
 * Resolves the window to the pair of instants the server takes. Presets are
 * relative to *now* and are re-resolved on every search rather than pinned when
 * chosen — "last hour" that quietly means an hour that ended twenty minutes ago
 * is the kind of wrongness nobody notices until it has cost them an afternoon.
 */
export function resolveRange(
  range: LogTimeRange,
  customSince: string,
  customUntil: string,
): { since?: string; until?: string } {
  if (range === "custom") {
    return {
      since: customSince ? new Date(customSince).toISOString() : undefined,
      until: customUntil ? new Date(customUntil).toISOString() : undefined,
    }
  }
  const preset = TIME_RANGES.find((r) => r.id === range)
  if (!preset?.minutes) return {}
  return { since: new Date(Date.now() - preset.minutes * 60_000).toISOString() }
}

/**
 * Where the search term sits inside a line, for the client-side case.
 *
 * The server sends byte ranges for search results because the browser cannot
 * re-run a Go regular expression faithfully. A live tail has no such answer —
 * computing ranges per line on a hot streaming path is work for lines nobody
 * will scroll back to — so highlighting there is done here, and a pattern
 * JavaScript refuses simply goes unhighlighted rather than throwing.
 */
export function highlightRanges(text: string, f: LogFilterState): [number, number][] {
  if (!f.q) return []
  let re: RegExp
  try {
    const pattern = f.regex ? f.q : f.q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
    re = new RegExp(pattern, f.ignoreCase ? "gi" : "g")
  } catch {
    return []
  }
  const out: [number, number][] = []
  for (const m of text.matchAll(re)) {
    if (m.index === undefined || m[0].length === 0) continue
    out.push([m.index, m.index + m[0].length])
    if (out.length >= 32) break
  }
  return out
}

/** Splits a line into plain and highlighted runs, given ranges in byte order. */
export function segmentLine(text: string, ranges: [number, number][] | undefined) {
  if (!ranges?.length) return [{ text, hit: false }]
  const out: { text: string; hit: boolean }[] = []
  let at = 0
  for (const [start, end] of ranges) {
    if (start < at || start >= text.length) continue
    if (start > at) out.push({ text: text.slice(at, start), hit: false })
    out.push({ text: text.slice(start, Math.min(end, text.length)), hit: true })
    at = Math.min(end, text.length)
  }
  if (at < text.length) out.push({ text: text.slice(at), hit: false })
  return out
}
