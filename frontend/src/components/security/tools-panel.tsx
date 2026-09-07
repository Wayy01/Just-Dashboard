"use client"

import { Crosshair, Globe, LockClosed, NetworkDevice, Servers, Shield } from "@/components/icons"
import { Section } from "@/components/page"
import { EmptyState } from "@/components/state"
import { useAuth } from "@/hooks/use-auth"
import { SubnetCard } from "./tools/subnet-card"
import { TOOL_GROUPS } from "./tools/tool-defs"
import { ToolCard } from "./tools/tool-card"

// One glyph per group, in group order — the page holds twenty cards and the
// icon is what a returning eye lands on first.
const GROUP_ICONS = [Globe, NetworkDevice, LockClosed, Shield, Servers] as const

/**
 * The tools an operator opens a terminal for, on the page where the question
 * arose.
 *
 * One independent card per tool: each keeps its own target, port and answer,
 * and several can run at once. The old single-panel version shared one input
 * across every tool, so switching tools carried the wrong value along and a
 * slow traceroute blocked the whole page.
 */
export function ToolsPanel() {
  const { can } = useAuth()

  if (!can("system.admin")) {
    return (
      <EmptyState
        icon={Crosshair}
        title="Diagnostics need the admin capability"
        description="A probe makes the server send traffic to an address the caller chose, which is a scanner if it is handed to everybody."
      />
    )
  }

  return (
    <div className="flex min-w-0 flex-col gap-6">
      {TOOL_GROUPS.map((group, i) => {
        const Icon = GROUP_ICONS[i] ?? Crosshair
        return (
          <Section
            key={group.title}
            title={
              <span className="inline-flex items-center gap-1.5">
                <Icon className="size-3.5 text-muted-foreground" />
                {group.title}
              </span>
            }
            description={group.hint}
          >
            <div className="grid min-w-0 gap-4 xl:grid-cols-2">
              {group.tools.map((def) => (
                <ToolCard key={def.key} def={def} />
              ))}
              {group.title === "This host" && <SubnetCard />}
            </div>
          </Section>
        )
      })}
      <p className="text-[11px] text-muted-foreground">
        Every card runs on its own — inputs stay put while other tools run.
      </p>
    </div>
  )
}
