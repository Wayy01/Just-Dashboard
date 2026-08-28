"use client"

import { Page, PageHeader } from "@/components/page"
import { PortsPanel } from "@/components/proxy/ports-panel"

export default function ProxyPortsPage() {
  return (
    <Page>
      <PageHeader eyebrow="Proxy" title="Listening ports" />
      <PortsPanel />
    </Page>
  )
}
