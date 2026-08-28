"use client"

import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { ImagesTab } from "@/components/docker/images-tab"

export default function DockerImagesPage() {
  const { confirm, dialog } = useConfirm()
  return (
    <Page>
      <PageHeader eyebrow="Docker" title="Images" />
      <ImagesTab confirm={confirm} />
      {dialog}
    </Page>
  )
}
