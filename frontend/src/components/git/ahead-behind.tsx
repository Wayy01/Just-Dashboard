"use client"

import { ArrowDownToLine, ArrowUpFromLine } from "lucide-react"

/**
 * How a branch stands against its upstream: commits waiting to push (ahead) and
 * commits waiting to pull (behind). Renders nothing when in sync, so a row that
 * is up to date stays quiet rather than showing two zeroes.
 */
export function AheadBehind({
  ahead,
  behind,
  showSynced,
}: {
  ahead: number
  behind: number
  /** When set, print "in sync" instead of nothing — for a header that needs a value. */
  showSynced?: boolean
}) {
  if (!ahead && !behind) {
    return showSynced ? <span className="text-xs text-muted-foreground">in sync</span> : null
  }
  return (
    <span className="numeric flex items-center gap-1.5 font-mono text-xs">
      {ahead > 0 && (
        <span className="inline-flex items-center gap-0.5 text-success" title={`${ahead} to push`}>
          <ArrowUpFromLine className="size-3" />
          {ahead}
        </span>
      )}
      {behind > 0 && (
        <span className="inline-flex items-center gap-0.5 text-warning" title={`${behind} to pull`}>
          <ArrowDownToLine className="size-3" />
          {behind}
        </span>
      )}
    </span>
  )
}
