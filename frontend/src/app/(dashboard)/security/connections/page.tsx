"use client"

import { Page, PageHeader } from "@/components/page"
import { ConnectionsPanel } from "@/components/security/connections-panel"
import { AreaFindings } from "@/components/security/posture-panel"
import { useSecurity } from "@/components/security/security-context"

export default function SecurityConnectionsPage() {
  const { posture, applyFix } = useSecurity()

  return (
    <Page>
      <PageHeader eyebrow="Security" title="Connections" />
      <AreaFindings posture={posture} area="ports" onFix={applyFix} />
      <ConnectionsPanel />
    </Page>
  )
}
