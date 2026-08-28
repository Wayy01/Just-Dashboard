"use client"

import { Globe, ShieldAlert, ShieldCheck } from "lucide-react"
import { get } from "@/lib/api"
import type { Exposure } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Skeleton } from "@/components/ui/skeleton"
import { Status, type Verdict } from "@/components/status-dot"

const GRADE: Record<Exposure["grade"], { label: string; verdict: Verdict }> = {
  tailscale: { label: "Tailscale only", verdict: "ok" },
  tunnel: { label: "SSH tunnel only", verdict: "ok" },
  private: { label: "Private network", verdict: "ok" },
  public: { label: "Public addresses", verdict: "warning" },
  open: { label: "Open to the internet", verdict: "critical" },
}

/**
 * How this dashboard is reachable — the security property the whole product
 * rests on, and the one that lives in an env file nobody opens again after
 * install day. On screen it stays true: a machine that quietly became
 * reachable from the internet says so here instead of waiting to be found.
 */
export function ExposurePanel({ className }: { className?: string }) {
  const { data } = usePoll<Exposure>((signal) => get("/exposure", undefined, signal), 60_000)

  if (!data) {
    return (
      <Panel className={className}>
        <PanelHeader icon={Globe} title="Reachable from" />
        <PanelBody className="space-y-2">
          <Skeleton className="h-4 w-56" />
          <Skeleton className="h-4 w-40" />
        </PanelBody>
      </Panel>
    )
  }

  const grade = GRADE[data.grade]
  const ok = grade.verdict === "ok"

  return (
    <Panel className={className}>
      <PanelHeader
        icon={ok ? ShieldCheck : ShieldAlert}
        title="Reachable from"
        description={grade.label}
        actions={<Status verdict={grade.verdict} label={grade.label} />}
      />
      <PanelBody className="space-y-3">
        <p className="text-[13px] text-muted-foreground">{data.summary}</p>
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="eyebrow">allowlist</span>
          {data.allowlist.map((cidr) => (
            <code
              key={cidr}
              className="rounded border border-hairline bg-surface-sunken px-1.5 py-0.5 font-mono text-[11px]"
            >
              {cidr}
            </code>
          ))}
        </div>
        {data.recommendation && (
          <p className="text-[13px] font-medium text-foreground">{data.recommendation}</p>
        )}
      </PanelBody>
    </Panel>
  )
}
