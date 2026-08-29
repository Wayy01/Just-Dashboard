"use client"

import { Page, PageHeader } from "@/components/page"
import { StreamsPanel } from "@/components/proxy/streams-panel"

export default function ProxyStreamsPage() {
  return (
    <Page>
      <PageHeader eyebrow="Proxy" title="Streams" />
      <StreamsPanel />
    </Page>
  )
}
