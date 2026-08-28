"use client"

import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { StacksTab } from "@/components/docker/stacks-tab"

export default function DockerStacksPage() {
  const { confirm, dialog } = useConfirm()
  return (
    <Page>
      <PageHeader eyebrow="Docker" title="Stacks" />
      <StacksTab confirm={confirm} />
      {dialog}
    </Page>
  )
}
