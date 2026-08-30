"use client"

import { Servers, Trash } from "@/components/icons"
import { notify } from "@/lib/toast"
import { get } from "@/lib/api"
import { bytes } from "@/lib/format"
import { prune, pruneContainers, pruneSummary, RECLAIM_SAFE } from "@/lib/docker-prune"
import type { DockerDiskUsage, DockerDiskUsageLine } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { LoadingRows } from "@/components/state"
import type { ConfirmFn } from "@/components/docker/shared"
import { Term } from "@/components/docker/explain"
import { Button } from "@/components/ui/button"

/**
 * Where the disk went, and the buttons that get it back.
 *
 * `docker system df` is the first thing anybody runs on a server that has
 * filled up, and this dashboard already computed every figure in it and showed
 * none of them: `GET /docker/disk-usage` existed with no caller. What the
 * operator got instead was a health finding announcing that forty gigabytes
 * could be reclaimed, whose button sent them to the image list — where the only
 * control prunes *dangling* images, which on a host that redeploys through
 * compose is usually nothing at all.
 *
 * So the panel states the four lines separately and puts the reclaim beside the
 * figure it belongs to. Build cache gets its own row because it is routinely
 * the largest of them and had no route in the product until 0.6.4; volumes get
 * one because leaving them out of a disk view is how somebody goes hunting for
 * the missing space in the wrong place — but their button is deliberately
 * absent, since that is the one line here that is data rather than cache.
 */
export function DiskPanel({ confirm, onPruned }: { confirm: ConfirmFn; onPruned?: () => void }) {
  const { can } = useAuth()
  const { data, loading, refresh } = usePoll(
    (signal) => get<DockerDiskUsage>("/docker/disk-usage", undefined, signal),
    60_000,
  )

  const done = () => {
    refresh()
    onPruned?.()
  }

  const run = async (scope: Parameters<typeof prune>[0]) => {
    const reports = await prune(scope)
    const { reclaimed, message, failed } = pruneSummary(reports)
    if (failed.length && reclaimed === 0) notify.error(message)
    else notify.success(message)
    done()
  }

  const safe = data ? data.images.reclaimable + data.buildCache.reclaimable : 0
  const total = data
    ? data.images.size + data.containers.size + data.volumes.size + data.buildCache.size
    : 0

  return (
    <Panel>
      <PanelHeader
        icon={Servers}
        title="Disk"
        description={
          data
            ? `${bytes(total)} used by Docker · ${bytes(safe)} can be reclaimed`
            : "what Docker is holding on this server"
        }
        actions={
          can("destructive") &&
          safe > 0 && (
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                confirm({
                  title: `Reclaim ${bytes(safe)}`,
                  confirmLabel: "Reclaim",
                  description: (
                    <>
                      <p>
                        Removes every image no container is using ({bytes(data!.images.reclaimable)}
                        ) and the whole <Term name="build cache">build cache</Term> (
                        {bytes(data!.buildCache.reclaimable)}), along with stopped containers and
                        unused networks.
                      </p>
                      <p>
                        Nothing a running container needs is touched, and <b>no volume is</b> — the
                        images come back from their registries and the cache rebuilds itself, more
                        slowly, on the next build.
                      </p>
                    </>
                  ),
                  action: () => run(RECLAIM_SAFE),
                })
              }
            >
              <Trash className="size-4" />
              Reclaim {bytes(safe)}
            </Button>
          )
        }
      />
      <PanelBody flush>
        {loading && !data ? (
          <LoadingRows rows={4} />
        ) : (
          <ul className="divide-y divide-hairline">
            <DiskRow
              label="Images"
              line={data?.images}
              unit="image"
              hint="layers pulled or built on this server"
              onReclaim={
                can("destructive") && data && data.images.reclaimable > 0
                  ? () =>
                      confirm({
                        title: "Remove unused images",
                        confirmLabel: "Remove",
                        description: (
                          <>
                            <p>
                              Removes every image no container is using —{" "}
                              {data.images.total - data.images.active} of {data.images.total},
                              freeing {bytes(data.images.reclaimable)}.
                            </p>
                            <p>
                              This is wider than{" "}
                              <Term name="dangling">pruning dangling images</Term>: it reaches
                              tagged ones too. Each comes back with a pull, and an image a container
                              is running is never removed.
                            </p>
                          </>
                        ),
                        action: () => run({ allImages: true }),
                      })
                  : undefined
              }
            />
            <DiskRow
              label="Build cache"
              line={data?.buildCache}
              unit="entry"
              hint="BuildKit's layer cache, kept after every build"
              onReclaim={
                can("destructive") && data && data.buildCache.reclaimable > 0
                  ? () =>
                      confirm({
                        title: "Empty the build cache",
                        confirmLabel: "Empty it",
                        description: (
                          <>
                            <p>
                              Frees {bytes(data.buildCache.reclaimable)} across{" "}
                              {data.buildCache.total} entries.
                            </p>
                            <p>
                              A cache holds nothing you cannot regenerate: the cost is that the next
                              build of each image starts from scratch and takes longer. Nothing that
                              is running is affected.
                            </p>
                          </>
                        ),
                        action: () => run({ buildCache: true, allBuildCache: true }),
                      })
                  : undefined
              }
            />
            <DiskRow
              label="Containers"
              line={data?.containers}
              unit="container"
              hint="what each one has written above its image"
              onReclaim={
                can("destructive") && data && data.containers.total > data.containers.active
                  ? () =>
                      confirm({
                        title: "Remove stopped containers",
                        confirmLabel: "Remove",
                        description: (
                          <p>
                            Removes the {data.containers.total - data.containers.active} container
                            {data.containers.total - data.containers.active === 1 ? "" : "s"} that
                            are not running, and whatever they had written to their own writable
                            layer. Their images and volumes stay.
                          </p>
                        ),
                        // The dedicated route, not the whole sweep: a button
                        // under a row labelled "Containers" must not also
                        // remove images and networks.
                        action: async () => {
                          const rep = await pruneContainers()
                          notify.success(
                            rep.items.length
                              ? `Removed ${rep.items.length} container${rep.items.length === 1 ? "" : "s"}, reclaiming ${bytes(rep.spaceReclaimed)}`
                              : "Nothing to remove",
                          )
                          done()
                        },
                      })
                  : undefined
              }
            />
            {/* No button. A volume is the one object on this page that is the
                data rather than a copy of something fetchable, so the disk view
                names the space and sends the operator to the tab that can show
                what each one holds before anything is deleted. */}
            <DiskRow
              label="Local volumes"
              line={data?.volumes}
              unit="volume"
              hint="the only line here that holds data — reclaim these one at a time"
            />
          </ul>
        )}
      </PanelBody>
    </Panel>
  )
}

function DiskRow({
  label,
  line,
  unit,
  hint,
  onReclaim,
}: {
  label: string
  line: DockerDiskUsageLine | undefined
  unit: string
  hint: string
  onReclaim?: () => void
}) {
  if (!line) return null
  const idle = line.total - line.active
  return (
    <li className="flex min-w-0 items-center gap-3 px-4 py-2.5">
      <span className="min-w-0 flex-1">
        <span className="block text-[13px] font-medium">{label}</span>
        <span className="block truncate text-xs text-muted-foreground">
          {line.total} {unit}
          {line.total === 1 ? "" : "s"}
          {line.total > 0 && `, ${line.active} in use`} · {hint}
        </span>
      </span>
      <span className="shrink-0 text-right">
        <span className="numeric block text-[13px]">{bytes(line.size)}</span>
        <span className="numeric block text-[11px] text-muted-foreground">
          {line.reclaimable > 0 ? `${bytes(line.reclaimable)} reclaimable` : "nothing to reclaim"}
        </span>
      </span>
      <span className="flex w-24 shrink-0 justify-end">
        {onReclaim && (
          <Button size="xs" variant="outline" onClick={onReclaim}>
            {idle > 0 && line.reclaimable === 0 ? "Remove" : "Reclaim"}
          </Button>
        )}
      </span>
    </li>
  )
}
