import { Suspense } from "react"
import { Page, PageHeader } from "@/components/page"
import { LoadingPanel } from "@/components/state"
import { DeploymentWorkspace } from "@/components/deploy/deployment-workspace"

export default function DeploymentPage() {
  return (
    <Suspense
      fallback={
        <Page>
          <PageHeader eyebrow="Deployments" title="Loading deployment" />
          <LoadingPanel rows={6} />
        </Page>
      }
    >
      <DeploymentWorkspace />
    </Suspense>
  )
}
