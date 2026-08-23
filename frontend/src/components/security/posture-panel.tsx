"use client"

import {
  AlertTriangle,
  CheckCircle2,
  Globe,
  HardDriveDownload,
  Info,
  Plug,
  ShieldAlert,
  ShieldCheck,
  Siren,
  TerminalSquare,
  Wrench,
} from "lucide-react"
import { relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { Posture, SecurityFinding } from "@/lib/types"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"

/**
 * Whether this machine is in reasonable shape, as a verdict.
 *
 * Everything else on this page shows what is configured — a rule list, a jail,
 * a table of open ports — and leaves the reading to somebody who already knows
 * how. The people who most need the answer are exactly the ones who do not.
 * Cockpit and Webmin show the facts and stop; the hosting panels sell a score
 * out of a hundred, which is a number to optimise rather than a thing to fix.
 *
 * So each finding carries three separate things — what was measured, what it
 * means, and what to do — and where the dashboard can carry the remedy out
 * itself, a button. The reasoning is on screen so the verdict can be argued
 * with rather than merely obeyed.
 */
export function PosturePanel({
  posture,
  loading,
  onFix,
  className,
}: {
  posture: Posture | undefined
  loading: boolean
  onFix?: (finding: SecurityFinding) => void
  className?: string
}) {
  if (loading && !posture) {
    return (
      <Panel className={className}>
        <PanelHeader icon={ShieldAlert} title="Security posture" />
        <PanelBody className="space-y-2">
          <Skeleton className="h-4 w-48" />
          <Skeleton className="h-4 w-72" />
        </PanelBody>
      </Panel>
    )
  }
  if (!posture) return null

  const ok = posture.findings.length === 0
  const counts = {
    critical: posture.findings.filter((f) => f.level === "critical").length,
    warning: posture.findings.filter((f) => f.level === "warning").length,
    notice: posture.findings.filter((f) => f.level === "notice").length,
  }

  return (
    <Panel className={className}>
      <PanelHeader
        icon={ok ? ShieldCheck : Siren}
        title="Security posture"
        description={
          ok
            ? "Every check passed"
            : [
                counts.critical && `${counts.critical} critical`,
                counts.warning && `${counts.warning} to fix`,
                counts.notice && `${counts.notice} worth knowing`,
              ]
                .filter(Boolean)
                .join(" · ")
        }
        actions={<PostureBadge status={posture.status} />}
      />
      <PanelBody className={ok ? undefined : "space-y-2"}>
        {ok ? (
          <p className="text-[13px] text-muted-foreground">
            Exposure, firewall, SSH, intrusion prevention, open ports, certificates and pending
            security updates all check out.
            {/* Said explicitly. "No findings" from a check that never ran looks
                exactly like "no findings" from one that did, and only one of
                those is reassuring. */}
            {posture.skipped.length > 0 &&
              ` Not checked on this host: ${posture.skipped.join(", ")} — not installed.`}
          </p>
        ) : (
          posture.findings.map((finding) => (
            <FindingRow key={finding.id} finding={finding} onFix={onFix} />
          ))
        )}
        <p className="pt-1 text-[11px] text-muted-foreground">
          {posture.checks} checks, evaluated {relativeTime(posture.checkedAt)}
          {!ok && posture.skipped.length > 0 && ` · not checked: ${posture.skipped.join(", ")}`}
        </p>
      </PanelBody>
    </Panel>
  )
}

/**
 * The area's icon.
 *
 * A branch per area returning real JSX rather than resolving a component into
 * a capitalised local: the latter produces a fresh component type on every
 * render, which React treats as a different element and remounts.
 */
function AreaIcon({ area, className }: { area: SecurityFinding["area"]; className?: string }) {
  if (area === "exposure") return <Globe className={className} />
  if (area === "firewall") return <ShieldAlert className={className} />
  if (area === "ssh") return <TerminalSquare className={className} />
  if (area === "intrusion") return <Siren className={className} />
  if (area === "ports") return <Plug className={className} />
  if (area === "tls") return <ShieldCheck className={className} />
  if (area === "updates") return <HardDriveDownload className={className} />
  return <Info className={className} />
}

function LevelIcon({ level, className }: { level: string; className?: string }) {
  if (level === "critical") return <ShieldAlert className={className} />
  if (level === "warning") return <AlertTriangle className={className} />
  return <Info className={className} />
}

function FindingRow({
  finding,
  onFix,
}: {
  finding: SecurityFinding
  onFix?: (finding: SecurityFinding) => void
}) {
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
      <LevelIcon level={finding.level} className={cn("mt-0.5 size-4 shrink-0", toneText(finding.level))} />
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-1">
          <span className="text-[13px] font-medium">{finding.title}</span>
          <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
            <AreaIcon area={finding.area} className="size-3" />
            {finding.area}
          </span>
        </div>
        <p className="text-[11px] leading-relaxed text-muted-foreground">{finding.detail}</p>
        {finding.advice && (
          <p className="text-[11px] leading-relaxed text-foreground/80">{finding.advice}</p>
        )}
      </div>
      {finding.fix && onFix && (
        <Button
          size="xs"
          variant="outline"
          className="shrink-0 self-start"
          onClick={() => onFix(finding)}
        >
          <Wrench className="size-3" />
          {finding.fixLabel ?? "Fix"}
        </Button>
      )}
    </div>
  )
}

/** The one-word verdict, small enough for a panel header. */
export function PostureBadge({
  status,
  className,
}: {
  status: Posture["status"]
  className?: string
}) {
  const label =
    status === "critical"
      ? "Action needed"
      : status === "warning"
        ? "Needs attention"
        : status === "notice"
          ? "Minor notes"
          : "Hardened"
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
      {status === "ok" ? <CheckCircle2 className="size-3" /> : <LevelIcon level={status} className="size-3" />}
      {label}
    </span>
  )
}

function toneText(level: string) {
  if (level === "critical") return "text-destructive"
  if (level === "warning") return "text-warning"
  return "text-muted-foreground"
}

/** Findings filtered to one area, for rendering next to the panel that fixes them. */
export function AreaFindings({
  posture,
  area,
  onFix,
  className,
}: {
  posture: Posture | undefined
  area: SecurityFinding["area"] | SecurityFinding["area"][]
  onFix?: (finding: SecurityFinding) => void
  className?: string
}) {
  const areas = Array.isArray(area) ? area : [area]
  const findings = posture?.findings.filter((f) => areas.includes(f.area)) ?? []
  if (findings.length === 0) return null
  return (
    <div className={cn("space-y-2", className)}>
      {findings.map((finding) => (
        <FindingRow key={finding.id} finding={finding} onFix={onFix} />
      ))}
    </div>
  )
}
