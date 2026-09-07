import { Suspense } from "react"
import { Page, PageHeader } from "@/components/page"
import { LoadingPanel } from "@/components/state"
import { DeploymentWizard } from "@/components/deploy/deployment-wizard"

export default function NewDeploymentPage() {
  return (
    <Suspense
      fallback={
        <Page>
          <PageHeader eyebrow="Deployments" title="Deploy something" />
          <LoadingPanel rows={5} />
        </Page>
      }
    >
      <DeploymentWizard />
    </Suspense>
  )
}
