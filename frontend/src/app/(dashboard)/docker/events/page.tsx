"use client"

import { Page, PageHeader } from "@/components/page"
import { EventsTab } from "@/components/docker/events-tab"

export default function DockerEventsPage() {
  return (
    <Page>
      <PageHeader eyebrow="Docker" title="Events" />
      <EventsTab />
    </Page>
  )
}
