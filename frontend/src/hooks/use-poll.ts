"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { ApiError } from "@/lib/api"

type PollState<T> = {
  data: T | undefined
  error: Error | undefined
  loading: boolean
  refresh: () => void
}

/**
 * Fetches on mount and then on an interval. Each run aborts the previous one,
 * so a slow endpoint cannot stack requests, and the interval is paused while
 * the tab is hidden — a dashboard left open in a background tab should not
 * keep hammering the server it is monitoring.
 */
export function usePoll<T>(
  fetcher: (signal: AbortSignal) => Promise<T>,
  intervalMs = 5000,
  deps: unknown[] = [],
): PollState<T> {
  const [data, setData] = useState<T>()
  const [error, setError] = useState<Error>()
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)
  const fetcherRef = useRef(fetcher)
  fetcherRef.current = fetcher

  const refresh = useCallback(() => setTick((t) => t + 1), [])

  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()

    const run = async () => {
      try {
        const next = await fetcherRef.current(controller.signal)
        if (cancelled) return
        setData(next)
        setError(undefined)
      } catch (err) {
        if (cancelled || controller.signal.aborted) return
        if (err instanceof DOMException && err.name === "AbortError") return
        setError(err instanceof Error ? err : new Error(String(err)))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    run()
    if (intervalMs <= 0) {
      return () => {
        cancelled = true
        controller.abort()
      }
    }
    const timer = setInterval(() => {
      if (document.visibilityState === "visible") run()
    }, intervalMs)
    return () => {
      cancelled = true
      controller.abort()
      clearInterval(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intervalMs, tick, ...deps])

  return { data, error, loading, refresh }
}

export function isAuthError(error: Error | undefined) {
  return error instanceof ApiError && error.isAuthProblem
}
