"use client"

import { Page, PageHeader } from "@/components/page"
import { FirewallPanel } from "@/components/security/firewall-panel"
import { useSecurity } from "@/components/security/security-context"

export default function SecurityFirewallPage() {
  const {
    firewall,
    firewallLoading,
    firewallError,
    refreshFirewall,
    refreshPosture,
    posture,
    applyFix,
  } = useSecurity()

  return (
    <Page>
      <PageHeader eyebrow="Security" title="Firewall" />
      <FirewallPanel
        status={firewall}
        posture={posture}
        loading={firewallLoading}
        error={firewallError}
        onFix={applyFix}
        refresh={() => {
          refreshFirewall()
          refreshPosture()
        }}
      />
    </Page>
  )
}
