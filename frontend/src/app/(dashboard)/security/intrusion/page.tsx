"use client"

import { Page, PageHeader } from "@/components/page"
import { IntrusionPanels } from "@/components/security/intrusion-panels"

export default function SecurityIntrusionPage() {
  return (
    <Page>
      <PageHeader eyebrow="Security" title="Intrusion prevention" />
      <IntrusionPanels />
    </Page>
  )
}
