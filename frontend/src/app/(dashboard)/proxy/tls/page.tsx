"use client"

import { Page, PageHeader } from "@/components/page"
import { TLSReport } from "@/components/proxy/tls-report"

export default function ProxyTLSPage() {
  return (
    <Page>
      <PageHeader eyebrow="Proxy" title="TLS report" />
      <TLSReport />
    </Page>
  )
}
