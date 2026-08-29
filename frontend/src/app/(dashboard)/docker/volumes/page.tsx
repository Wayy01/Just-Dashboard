"use client"

import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { VolumesTab } from "@/components/docker/volumes-tab"

export default function DockerVolumesPage() {
  const { confirm, dialog } = useConfirm()
  return (
    <Page>
      <PageHeader eyebrow="Docker" title="Volumes" />
      <VolumesTab confirm={confirm} />
      {dialog}
    </Page>
  )
}
