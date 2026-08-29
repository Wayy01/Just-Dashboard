"use client"

import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { NetworksTab } from "@/components/docker/networks-tab"

export default function DockerNetworksPage() {
  const { confirm, dialog } = useConfirm()
  return (
    <Page>
      <PageHeader eyebrow="Docker" title="Networks" />
      <NetworksTab confirm={confirm} />
      {dialog}
    </Page>
  )
}
