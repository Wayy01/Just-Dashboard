"use client"

import { Page, PageHeader } from "@/components/page"
import { SSHPanel } from "@/components/security/ssh-panel"
import { AreaFindings } from "@/components/security/posture-panel"
import { useSecurity } from "@/components/security/security-context"

export default function SecuritySSHPage() {
  const { posture, applyFix } = useSecurity()

  return (
    <Page>
      <PageHeader eyebrow="Security" title="SSH" />
      <AreaFindings posture={posture} area="ssh" onFix={applyFix} />
      <SSHPanel />
    </Page>
  )
}
