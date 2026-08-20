"use client"

import { useCallback, useSyncExternalStore } from "react"
import { get, ApiError } from "@/lib/api"
import { usePoll } from "@/hooks/use-poll"
import {
  currentRange,
  rangeSpec,
  serverRange,
  storeRange,
  subscribeRange,
  type RangeKey,
} from "@/lib/metrics-range"
import type { MetricsHistory } from "@/lib/types"

export type HistoryState = {
  history: MetricsHistory | undefined
  error: Error | undefined
  loading: boolean
  /** True when the server is not recording history at all, which is a setting rather than a fault. */
  disabled: boolean
}

/**
 * Reads the server's recorded history for one window.
 *
 * This is the half of the metrics story that outlives the browser tab. The
 * live socket can only ever show the time since the page was opened; this
 * endpoint is what makes the overnight spike visible at all, so the Overview
 * page defaults to it rather than to the live feed.
 *
 * Disabled for the "live" range, where there is nothing to fetch.
 */
export function useMetricsHistory(range: RangeKey): HistoryState {
  const spec = rangeSpec(range)
  const query = spec.query

  const fetcher = useCallback(
    (signal: AbortSignal) => {
      if (!query) return Promise.resolve(undefined)
      return get<MetricsHistory>("/system/metrics/history", { range: query, points: spec.points }, signal)
    },
    [query, spec.points],
  )

  // Polling is paused on a hidden tab by usePoll, so a dashboard left open in
  // a background tab does not keep asking the server it is monitoring for a
  // week of history every five minutes.
  const { data, error, loading } = usePoll<MetricsHistory | undefined>(
    fetcher,
    query ? spec.refreshMs : 0,
    [query],
  )

  const disabled = error instanceof ApiError && error.code === "metrics_history_disabled"
  return {
    history: data,
    error: disabled ? undefined : error,
    loading: Boolean(query) && loading,
    disabled,
  }
}

/**
 * The chosen window, remembered across reloads.
 *
 * Backed by an external store rather than component state so the preference
 * can be read during render: reading localStorage in an effect and setting
 * state from it would render the wrong window first and then correct it, which
 * costs a fetch of the default range on every page load.
 */
export function useRangePreference(): [RangeKey, (key: RangeKey) => void] {
  const range = useSyncExternalStore(subscribeRange, currentRange, serverRange)
  return [range, storeRange]
}
