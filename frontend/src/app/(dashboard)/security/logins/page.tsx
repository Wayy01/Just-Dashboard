"use client"

import { Page, PageHeader } from "@/components/page"
import { LoginsPanels } from "@/components/security/logins-panels"

export default function SecurityLoginsPage() {
  return (
    <Page>
      <PageHeader eyebrow="Security" title="Logins" />
      <LoginsPanels />
    </Page>
  )
}
