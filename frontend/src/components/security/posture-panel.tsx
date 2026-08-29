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
} from "lucide-react"
import { relativeTime } from "@/lib/format"
import type { Posture, SecurityFinding } from "@/lib/types"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Status } from "@/components/status-dot"
import { FindingList, type Finding } from "@/components/metrics/finding-list"
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
 * itself, a button. Rendered through `FindingList`, the same list the health
 * verdict uses, rather than a wall of tinted alert boxes.
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

  return (
    <Panel className={className}>
      <PanelHeader
        icon={ok ? ShieldCheck : Siren}
        title="Security posture"
        description={`${posture.checks} checks · ${relativeTime(posture.checkedAt)}${
          posture.skipped.length > 0 ? ` · not checked: ${posture.skipped.join(", ")}` : ""
        }`}
        actions={<PostureBadge status={posture.status} />}
      />
      <PanelBody>
        <FindingList
          findings={posture.findings.map((f) => toFinding(f, onFix))}
          emptyLabel="Exposure, firewall, SSH, intrusion prevention, open ports, certificates and pending security updates all check out"
        />
      </PanelBody>
    </Panel>
  )
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
    <div className={className}>
      <FindingList findings={findings.map((f) => toFinding(f, onFix))} />
    </div>
  )
}

/** A `SecurityFinding` as the shape `FindingList` renders: area on the right, fix as a button. */
function toFinding(finding: SecurityFinding, onFix?: (f: SecurityFinding) => void): Finding {
  return {
    id: finding.id,
    level: finding.level,
    title: finding.title,
    detail: finding.detail,
    advice: finding.advice,
    meta: (
      <span className="inline-flex items-center gap-1">
        <AreaIcon area={finding.area} className="size-3" />
        {finding.area}
      </span>
    ),
    action:
      finding.fix && onFix
        ? { label: finding.fixLabel ?? "Fix", onClick: () => onFix(finding) }
        : undefined,
  }
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
  const Icon =
    status === "ok"
      ? CheckCircle2
      : status === "critical"
        ? ShieldAlert
        : status === "warning"
          ? AlertTriangle
          : Info
  return <Status verdict={status} label={label} icon={Icon} className={className} />
}
