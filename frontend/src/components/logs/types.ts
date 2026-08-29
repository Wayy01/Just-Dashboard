import type { LogLevel } from "@/lib/log-filter"

/**
 * What the operator has narrowed to. Live and Search are two questions about
 * the same thing — what is happening now, and what happened — so they share one
 * filter: seeing errors scroll past and asking when they started is a mode
 * switch, not a form to fill in again.
 */
export type LogFilterState = {
  q: string
  exclude: string
  regex: boolean
  ignoreCase: boolean
  levels: LogLevel[]
}

export type LogMode = "live" | "search"

export type LogTimeRange = "15m" | "1h" | "6h" | "24h" | "7d" | "all" | "custom"
