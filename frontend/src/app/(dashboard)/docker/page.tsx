"use client"

import { useMemo, useState } from "react"
import Link from "next/link"
import { ArrowRight, CircleSlash, HeartPulse, Layers, Play, Plus, Trash2 } from "lucide-react"
import { notify } from "@/lib/toast"
import { get } from "@/lib/api"
import { prune, pruneSummary, RECLAIM_SAFE } from "@/lib/docker-prune"
import type { Container, ContainerSpec, DockerDiagnosis } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { StatTile } from "@/components/stat-tile"
import { StatusDot } from "@/components/status-dot"
import { EmptyState, LoadingPanel } from "@/components/state"
import { healthLabel } from "@/components/docker/diagnosis-panel"
import { CreateContainerPanel } from "@/components/docker/create-container"
import { Button } from "@/components/ui/button"

export default function DockerOverviewPage() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [creating, setCreating] = useState<ContainerSpec | true | null>(null)

  const list = usePoll<Container[]>(
    (signal) => get<Container[]>("/docker/containers/", undefined, signal),
    30_000,
  )
  const health = usePoll<DockerDiagnosis>(
    (signal) => get<DockerDiagnosis>("/docker/health", undefined, signal),
    60_000,
  )

  const containers = useMemo(() => list.data ?? [], [list.data])
  const stacks = useMemo(() => groupStacks(containers), [containers])

  if (list.loading && !list.data) {
    return (
      <Page>
        <PageHeader eyebrow="Server" title="Docker" />
        <LoadingPanel />
      </Page>
    )
  }

  const running = containers.filter((c) => c.state === "running").length
  const stopped = containers.length - running
  const status = health.data?.status ?? "ok"
  const findings = health.data?.findings.length ?? 0

  return (
    <Page>
      <PageHeader
        eyebrow="Server"
        title="Docker"
        actions={
          <>
            {can("service.control") && (
              <Button size="sm" onClick={() => setCreating(true)}>
                <Plus className="size-4" />
                Run a container
              </Button>
            )}
            {can("destructive") && (
              <Button
                variant="outline"
                size="sm"
                onClick={() =>
                  confirm({
                    title: "Reclaim disk",
                    confirmLabel: "Reclaim",
                    description: (
                      <>
                        <p>
                          Removes stopped containers, unused networks, every image no container is
                          using, and the build cache.
                        </p>
                        <p>
                          Volumes are left alone — those hold data, and the Volumes page removes
                          them one at a time. Everything else here comes back from a registry or
                          rebuilds itself.
                        </p>
                      </>
                    ),
                    // The same scope as the health finding's "Reclaim it" and
                    // the disk panel's button. Three entry points running three
                    // different sweeps is how one of them ended up freeing
                    // nothing while the page promised forty gigabytes.
                    action: async () => {
                      const reports = await prune(RECLAIM_SAFE)
                      const { reclaimed, message, failed } = pruneSummary(reports)
                      if (failed.length && reclaimed === 0) notify.error(message)
                      else notify.success(message)
                      health.refresh()
                      list.refresh()
                    },
                  })
                }
              >
                <Trash2 className="size-4" />
                Prune
              </Button>
            )}
          </>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4 [&>*]:min-w-0">
        <StatTile
          label="Running"
          icon={Play}
          value={running}
          tone={running > 0 ? "success" : "default"}
          hint={`${containers.length} in total`}
        />
        <StatTile
          label="Stopped"
          icon={CircleSlash}
          value={stopped}
          hint={stopped ? "not currently serving" : "everything is up"}
        />
        <StatTile
          label="Compose stacks"
          icon={Layers}
          value={stacks.length}
          hint="labelled projects"
        />
        <Link href="/docker/containers" className="block min-w-0">
          <StatTile
            className="h-full transition-colors hover:border-primary/30"
            label="Health"
            icon={HeartPulse}
            value={healthLabel(status)}
            tone={status === "critical" ? "danger" : status === "warning" ? "warning" : "success"}
            hint={
              findings > 0
                ? `${findings} finding${findings === 1 ? "" : "s"} — review in Containers`
                : "nothing to report"
            }
          />
        </Link>
      </div>

      <Panel>
        <PanelHeader
          icon={Layers}
          title="Compose projects"
          actions={
            <Link
              href="/docker/stacks"
              className="flex items-center gap-1 text-[11px] font-medium text-muted-foreground hover:text-foreground"
            >
              Manage <ArrowRight className="size-3" />
            </Link>
          }
        />
        <PanelBody flush>
          {stacks.length === 0 ? (
            <EmptyState
              icon={Layers}
              title="No compose stacks"
              description="Containers with a compose project label are grouped here."
            />
          ) : (
            <ul className="divide-y divide-hairline">
              {stacks.map((stack) => {
                const tone =
                  stack.running === stack.total
                    ? "running"
                    : stack.running === 0
                      ? "stopped"
                      : "warning"
                return (
                  <li key={stack.name}>
                    <Link
                      href="/docker/stacks"
                      className="flex min-w-0 items-center justify-between gap-3 px-4 py-2.5 hover:bg-[var(--row-hover)]"
                    >
                      <span className="flex min-w-0 items-center gap-2.5">
                        <StatusDot tone={tone} />
                        <span className="truncate text-[13px] font-medium">{stack.name}</span>
                      </span>
                      <span className="numeric shrink-0 text-[11px] text-muted-foreground">
                        {stack.running}/{stack.total} up
                      </span>
                    </Link>
                  </li>
                )
              })}
            </ul>
          )}
        </PanelBody>
      </Panel>

      <CreateContainerPanel
        open={creating !== null}
        initialSpec={creating === true ? undefined : (creating ?? undefined)}
        onOpenChange={(open) => !open && setCreating(null)}
        onCreated={() => {
          list.refresh()
          health.refresh()
        }}
      />
      {dialog}
    </Page>
  )
}

type StackRow = { name: string; total: number; running: number }

function groupStacks(containers: Container[]): StackRow[] {
  const map = new Map<string, StackRow>()
  for (const c of containers) {
    if (!c.composeStack) continue
    const row = map.get(c.composeStack) ?? { name: c.composeStack, total: 0, running: 0 }
    row.total += 1
    if (c.state === "running") row.running += 1
    map.set(c.composeStack, row)
  }
  return [...map.values()].sort((a, b) => a.name.localeCompare(b.name))
}
