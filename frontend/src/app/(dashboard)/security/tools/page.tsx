"use client"

import { Page, PageHeader } from "@/components/page"
import { ToolsPanel } from "@/components/security/tools-panel"

export default function SecurityToolsPage() {
  return (
    <Page>
      <PageHeader
        eyebrow="Security"
        title="Tools"
        description="Twenty probes that run from this server — each card keeps its own input and runs on its own."
      />
      <ToolsPanel />
    </Page>
  )
}
