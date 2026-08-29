"use client"

import { Page, PageHeader } from "@/components/page"
import { CertsPanel } from "@/components/proxy/certs-panel"

export default function ProxyCertificatesPage() {
  return (
    <Page>
      <PageHeader eyebrow="Proxy" title="Certificates" />
      <CertsPanel />
    </Page>
  )
}
