"use client"

import { Page, PageHeader } from "@/components/page"
import { VHostsPanel } from "@/components/proxy/vhosts-panel"
import { useProxy } from "@/components/proxy/proxy-context"

export default function ProxySitesPage() {
  const { hasNginx } = useProxy()
  return (
    <Page>
      <PageHeader eyebrow="Proxy" title="Sites" />
      <VHostsPanel hasNginx={hasNginx} />
    </Page>
  )
}
