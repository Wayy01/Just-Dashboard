"use client"

import { Page, PageHeader } from "@/components/page"
import { NetworkPanel } from "@/components/security/network-panel"

export default function SecurityNetworkPage() {
  return (
    <Page>
      <PageHeader eyebrow="Security" title="Network" />
      <NetworkPanel />
    </Page>
  )
}
