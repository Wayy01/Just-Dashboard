"use client"

import Link from "next/link"
import {
  Bug,
  Connection,
  Crosshair,
  NetworkDevice,
  Shield,
  TerminalWindow,
  Users,
} from "@/components/icons"
import type { SecurityFinding } from "@/lib/types"
import { Page, PageHeader, Section } from "@/components/page"
import { Panel, PanelBody } from "@/components/panel"
import { StatusDot, type Tone } from "@/components/status-dot"
import { ExposurePanel } from "@/components/security/exposure-panel"
import { PosturePanel } from "@/components/security/posture-panel"
import { useSecurity } from "@/components/security/security-context"

type Area = SecurityFinding["area"]

const FIREWALL_HREF = "/security/firewall"

const AREAS: { area: Area | Area[]; href: string; title: string; icon: typeof Shield; blurb: string }[] =
  [
    { area: "firewall", href: FIREWALL_HREF, title: "Firewall", icon: Shield, blurb: "Inbound rules and default policy" },
    { area: "ssh", href: "/security/ssh", title: "SSH", icon: TerminalWindow, blurb: "sshd's effective configuration" },
    { area: "intrusion", href: "/security/intrusion", title: "Intrusion", icon: Bug, blurb: "fail2ban jails and ban activity" },
    { area: "ports", href: "/security/connections", title: "Connections", icon: NetworkDevice, blurb: "Live TCP connections in and out" },
    { area: [], href: "/security/logins", title: "Logins", icon: Users, blurb: "Who is on the host, and who has been" },
    { area: [], href: "/security/network", title: "Network", icon: Connection, blurb: "Interfaces, routes and listeners" },
    { area: [], href: "/security/tools", title: "Tools", icon: Crosshair, blurb: "Port scan and TLS probe" },
  ]

export default function SecurityOverviewPage() {
  const { posture, postureLoading, firewall, applyFix } = useSecurity()

  const countFor = (area: Area | Area[]) => {
    const list = Array.isArray(area) ? area : [area]
    if (list.length === 0) return null
    return posture?.findings.filter((f) => list.includes(f.area)) ?? []
  }

  return (
    <Page>
      <PageHeader
        eyebrow="Network"
        title="Security"
        description="Exposure, firewall, SSH, intrusion prevention and who is connected"
      />

      <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        <ExposurePanel />
        <PosturePanel posture={posture} loading={postureLoading} onFix={applyFix} />
      </div>

      <Section title="Jump to">
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 [&>*]:min-w-0">
          {AREAS.map(({ area, href, title, icon: Icon, blurb }) => {
            const findings = countFor(area)
            const worst = worstLevel(findings)
            const isFirewall = href === FIREWALL_HREF
            const tone: Tone =
              isFirewall && firewall && !firewall.enabled
                ? "warning"
                : worst
                  ? LEVEL_TONE[worst]
                  : "running"
            const detail =
              isFirewall && firewall
                ? firewall.enabled
                  ? `${firewall.backend} · ${firewall.rules.length} rules`
                  : `${firewall.backend} · not enabled`
                : findings === null
                  ? blurb
                  : findings.length > 0
                    ? `${findings.length} finding${findings.length === 1 ? "" : "s"}`
                    : "nothing outstanding"

            return (
              <Link key={href} href={href} className="block min-w-0">
                <Panel className="h-full transition-colors hover:border-primary/30">
                  <PanelBody className="flex items-start gap-3">
                    <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted">
                      <Icon className="size-4 text-muted-foreground" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="text-[13px] font-medium">{title}</p>
                      <p className="mt-0.5 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                        <StatusDot tone={tone} />
                        <span className="truncate">{detail}</span>
                      </p>
                    </div>
                  </PanelBody>
                </Panel>
              </Link>
            )
          })}
        </div>
      </Section>
    </Page>
  )
}

const LEVEL_TONE: Record<SecurityFinding["level"], Tone> = {
  critical: "critical",
  warning: "warning",
  notice: "notice",
}

function worstLevel(findings: SecurityFinding[] | null): SecurityFinding["level"] | null {
  if (!findings || findings.length === 0) return null
  if (findings.some((f) => f.level === "critical")) return "critical"
  if (findings.some((f) => f.level === "warning")) return "warning"
  return "notice"
}
