"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { ApiError } from "@/lib/api"

type PollState<T> = {
  data: T | undefined
  error: Error | undefined
  loading: boolean
  refresh: () => void
}

type PollOptions = {
  /**
   * Whether to fetch at all. False leaves the last result in place and makes
   * no request.
   *
   * `useSocket` has always had this and `usePoll` did not, which turned out to
   * matter: a detail panel that is closed still renders, and a fetcher built
   * as `/docker/stacks/${id ?? ""}` with no id addresses the *list* endpoint —
   * so a closed panel quietly received an array, rendered `data.services.map`
   * on it, and took the whole page down with a TypeError. Guarding each call
   * site with a ternary would have worked; a hook that can be switched off is
   * the version the next panel gets for free.
   */
  enabled?: boolean
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
  { enabled = true }: PollOptions = {},
): PollState<T> {
  const [data, setData] = useState<T>()
  const [error, setError] = useState<Error>()
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)
  // The fetcher is closed over by the interval, so it is kept in a ref that
  // is synced after render rather than assigned during it — writing a ref
  // mid-render is what makes a component's output depend on when it ran.
  const fetcherRef = useRef(fetcher)
  useEffect(() => {
    fetcherRef.current = fetcher
  })

  const refresh = useCallback(() => setTick((t) => t + 1), [])

  useEffect(() => {
    if (!enabled) return
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
    // The spread is what the disable is for: the caller's deps cannot be
    // named here, and React throws if a deps array changes length between
    // renders. Every call site passes a fixed-length literal, which is the
    // condition this relies on — a caller building `deps` conditionally would
    // crash the page rather than poll wrongly.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intervalMs, tick, enabled, ...deps])

  // A disabled poll is not loading: nothing is in flight, and reporting
  // otherwise would leave a caller showing a skeleton forever.
  return { data, error, loading: enabled && loading, refresh }
}

export function isAuthError(error: Error | undefined) {
  return error instanceof ApiError && error.isAuthProblem
}
