"use client"

import { AlertTriangle, CheckCircle2, Info, ShieldAlert } from "lucide-react"
import { cn } from "@/lib/utils"
import { relativeTime } from "@/lib/format"
import type { Health, HealthFinding } from "@/lib/types"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Skeleton } from "@/components/ui/skeleton"

/**
 * What the numbers mean.
 *
 * Every dashboard in this class shows utilisation and stops there, leaving the
 * reader to know that 3% steal is bad, that 88% memory usually is not, and
 * that a filesystem at 30% can still refuse to create a file. Netdata takes a
 * position and buries the reasoning in a configuration file; Cockpit does not
 * take one at all.
 *
 * So each finding carries three separate things: what was measured, what it
 * means, and what to do about it. The advice is the part worth having — a
 * warning that says "steal is high" and nothing else has told the reader
 * nothing they could not see on the chart.
 */
export function HealthPanel({
  health,
  loading,
  className,
}: {
  health: Health | undefined
  loading: boolean
  className?: string
}) {
  if (loading && !health) {
    return (
      <Panel className={className}>
        <PanelHeader icon={ShieldAlert} title="Health" />
        <PanelBody className="space-y-2">
          <Skeleton className="h-4 w-40" />
          <Skeleton className="h-4 w-64" />
        </PanelBody>
      </Panel>
    )
  }
  if (!health) return null

  const ok = health.findings.length === 0

  return (
    <Panel className={className}>
      <PanelHeader
        icon={ok ? CheckCircle2 : iconFor(health.status)}
        title="Health"
        description={
          ok
            ? "Nothing to report"
            : `${health.findings.length} ${health.findings.length === 1 ? "finding" : "findings"}`
        }
        actions={<HealthBadge status={health.status} />}
      />
      <PanelBody className={ok ? undefined : "space-y-2"}>
        {ok ? (
          <p className="text-[13px] text-muted-foreground">
            Every check passed: capacity, memory headroom, CPU steal, pressure and sockets.
            {/* Said explicitly rather than left implied. "No warnings" from a
                checker that never ran looks exactly like "no warnings" from
                one that did, and only one of those is reassuring. */}
            {health.recorded
              ? " Checked against the last hour of recorded history."
              : " Checked against the current reading only — history is not being recorded."}
          </p>
        ) : (
          health.findings.map((finding) => <FindingRow key={finding.id} finding={finding} />)
        )}
        <p className="pt-1 text-[11px] text-muted-foreground">
          Evaluated {relativeTime(health.checkedAt)}
        </p>
      </PanelBody>
    </Panel>
  )
}

/**
 * The level's icon.
 *
 * A branch per level returning real JSX, rather than resolving a component
 * into a capitalised local and rendering that: the latter produces a fresh
 * component type on every render, which React treats as a different element
 * and remounts.
 */
function LevelIcon({ level, className }: { level: string; className?: string }) {
  if (level === "critical") return <ShieldAlert className={className} />
  if (level === "warning") return <AlertTriangle className={className} />
  if (level === "notice") return <Info className={className} />
  return <CheckCircle2 className={className} />
}

function FindingRow({ finding }: { finding: HealthFinding }) {
  return (
    <div
      className={cn(
        "flex min-w-0 gap-2.5 rounded-lg border p-2.5",
        finding.level === "critical"
          ? "border-destructive/30 bg-destructive/5"
          : finding.level === "warning"
            ? "border-warning/30 bg-warning/5"
            : "border-hairline bg-surface-sunken",
      )}
    >
      <LevelIcon
        level={finding.level}
        className={cn("mt-0.5 size-4 shrink-0", toneText(finding.level))}
      />
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
          <span className="text-[13px] font-medium">{finding.title}</span>
          <span className="numeric text-[11px] text-muted-foreground">{finding.detail}</span>
        </div>
        {finding.advice && (
          <p className="text-[11px] leading-relaxed text-muted-foreground">{finding.advice}</p>
        )}
      </div>
    </div>
  )
}

/** The one-word verdict, small enough for the top bar and a panel header alike. */
export function HealthBadge({ status, className }: { status: Health["status"]; className?: string }) {
  const label =
    status === "critical" ? "Critical" : status === "warning" ? "Warning" : status === "notice" ? "Notice" : "Healthy"
  return (
    <span
      className={cn(
        "flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-medium",
        status === "critical"
          ? "border-destructive/30 bg-destructive/10 text-destructive"
          : status === "warning"
            ? "border-warning/30 bg-warning/10 text-warning"
            : status === "notice"
              ? "border-border bg-muted/50 text-muted-foreground"
              : "border-success/25 bg-success/10 text-success",
        className,
      )}
    >
      <span
        aria-hidden
        className={cn(
          "size-1.5 rounded-full",
          status === "critical"
            ? "bg-destructive"
            : status === "warning"
              ? "bg-warning"
              : status === "notice"
                ? "bg-muted-foreground"
                : "bg-success",
        )}
      />
      {label}
    </span>
  )
}

function iconFor(level: string) {
  if (level === "critical") return ShieldAlert
  if (level === "warning") return AlertTriangle
  if (level === "notice") return Info
  return CheckCircle2
}

function toneText(level: string) {
  if (level === "critical") return "text-destructive"
  if (level === "warning") return "text-warning"
  return "text-muted-foreground"
}
