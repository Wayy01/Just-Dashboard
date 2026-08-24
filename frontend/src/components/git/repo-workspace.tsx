"use client"

import { useCallback, useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import {
  ArrowDownToLine,
  ArrowLeft,
  ArrowUpFromLine,
  GitBranch as GitBranchIcon,
  MoreHorizontal,
  RefreshCw,
  RotateCcw,
  Undo2,
} from "lucide-react"
import { notify } from "@/lib/toast"
import { get, post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { GitFileChange, GitRepo, GitResult, GitStatus } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { FileTree, type ConfirmRequest as TreeConfirmRequest } from "@/components/files/file-tree"
import { AheadBehind } from "@/components/git/ahead-behind"
import { GitHelp } from "@/components/git/help"
import { ChangesPanel } from "@/components/git/changes-panel"
import { HistoryPanel } from "@/components/git/history-panel"
import { BranchesPanel } from "@/components/git/branches-panel"
import { PreviewPanel, type GitPreview } from "@/components/git/preview-panel"
import { Panel } from "@/components/panel"
import { Page } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

/**
 * One repository, as a place to work rather than a report to read.
 *
 * Three columns, the shape every git client that people actually enjoy has
 * converged on: the file tree on the left, what-changed and history in the
 * middle, and whatever you last clicked — a diff or a file — filling the right.
 * It is deliberately not a modal sheet: a working copy is somewhere you spend
 * time, and the point of folding the tree in here is that reading a file, or
 * its diff, never bounces you to another page and loses your place.
 *
 * Everything destructive goes through the shared typed-confirmation dialog, and
 * every unfamiliar word carries its meaning one hover away (see help.tsx), so
 * the same screen serves someone committing for the first time and someone who
 * has done it ten thousand times.
 */
export function RepoWorkspace({
  repo,
  onBack,
  onRepoChanged,
}: {
  repo: GitRepo
  onBack: () => void
  onRepoChanged: () => void
}) {
  const { can } = useAuth()
  const router = useRouter()
  const { confirm, dialog } = useConfirm()
  const [busy, setBusy] = useState<string>()
  const [tab, setTab] = useState("changes")
  const [preview, setPreview] = useState<GitPreview | null>(null)

  const canControl = can("service.control")
  const canDestruct = can("destructive")
  const canWrite = can("file.write")
  const q = { path: repo.path }

  const status = usePoll(
    (signal) => get<GitStatus>("/git/status", { path: repo.path }, signal),
    12000,
    [repo.path],
  )
  const head = status.data?.repo ?? repo
  const changeCount = status.data?.files.length ?? 0

  const run = useCallback(
    async (label: string, fn: () => Promise<GitResult>) => {
      setBusy(label)
      try {
        const res = await fn()
        notify.success(label, { description: res.output?.split("\n").slice(0, 3).join("\n") })
        status.refresh()
        onRepoChanged()
        return res
      } catch (err) {
        notify.error(`${label} failed`, err)
        throw err
      } finally {
        setBusy(undefined)
      }
    },
    [status, onRepoChanged],
  )

  // The tree badges changed files; git reports them relative to the repo root.
  const statusMap = useMemo(() => {
    const map: Record<string, GitFileChange> = {}
    for (const f of status.data?.files ?? []) map[`${repo.path.replace(/\/$/, "")}/${f.path}`] = f
    return map
  }, [repo.path, status.data])

  // The tree speaks the fullscreen-safe ConfirmRequest shape; here it just maps
  // onto the same typed-confirmation dialog everything else uses.
  const treeConfirm = (req: TreeConfirmRequest) =>
    confirm({
      title: req.title,
      description: req.body,
      confirmLabel: req.confirmLabel,
      action: async () => {
        await req.run()
      },
    })

  return (
    <Page fill className="gap-2 px-2 py-2 md:px-3 md:py-3">
      {/* Header: where you are, what branch, and the network actions. */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 rounded-lg border bg-card px-2.5 py-1.5">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              size="sm"
              variant="ghost"
              className="size-7 shrink-0 p-0 text-muted-foreground hover:text-foreground"
              aria-label="Back to repositories"
              onClick={onBack}
            >
              <ArrowLeft className="size-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Back to all repositories</TooltipContent>
        </Tooltip>
        <div className="min-w-0">
          <p className="truncate text-[13px] font-medium" title={repo.path}>
            {repo.name}
          </p>
          <p className="truncate font-mono text-[10px] text-muted-foreground" title={repo.path}>
            {repo.path}
            {head.remote ? ` · ${head.remote}` : ""}
          </p>
        </div>
        <Tooltip>
          <TooltipTrigger asChild>
            <button onClick={() => setTab("branches")} className="shrink-0">
              <Badge variant={head.detached ? "destructive" : "secondary"} className="font-normal">
                <GitBranchIcon className="size-3" />
                {head.branch || "—"}
              </Badge>
            </button>
          </TooltipTrigger>
          <TooltipContent>
            {head.detached
              ? "HEAD is detached — switch or manage branches"
              : "Switch or manage branches"}
          </TooltipContent>
        </Tooltip>
        <AheadBehind ahead={head.ahead} behind={head.behind} />

        <span className="flex-1" />

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              size="sm"
              variant="outline"
              disabled={!!busy}
              onClick={() =>
                run("Fetched", () =>
                  post<GitResult>("/git/fetch", undefined, { query: { ...q, prune: true } }),
                )
              }
            >
              <RefreshCw className={cn("size-4", busy === "Fetched" && "animate-spin")} />
              Fetch
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            Check the remote for new commits without changing your files
          </TooltipContent>
        </Tooltip>
        {canControl && (
          <>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={!!busy}
                  onClick={() =>
                    run("Pulled", () => post<GitResult>("/git/pull", undefined, { query: q }))
                  }
                >
                  <ArrowDownToLine className="size-4" />
                  Pull
                  {head.behind > 0 && (
                    <span className="numeric ml-0.5 rounded bg-warning/20 px-1 text-[10px] text-warning">
                      {head.behind}
                    </span>
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                Bring the latest committed changes down from the remote
              </TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="sm"
                  disabled={!!busy}
                  onClick={() =>
                    run("Pushed", () => post<GitResult>("/git/push", undefined, { query: q }))
                  }
                >
                  <ArrowUpFromLine className="size-4" />
                  Push
                  {head.ahead > 0 && (
                    <span className="numeric ml-0.5 rounded bg-primary-foreground/20 px-1 text-[10px]">
                      {head.ahead}
                    </span>
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>Send your committed changes up to the remote</TooltipContent>
            </Tooltip>
            <StashMenu
              busy={busy}
              stashes={status.data?.stashes ?? 0}
              clean={status.data?.clean}
              onStash={() => run("Stashed", () => post<GitResult>("/git/stash", {}, { query: q }))}
              onPop={() =>
                run("Stash popped", () =>
                  post<GitResult>("/git/stash/pop", undefined, { query: q }),
                )
              }
            />
          </>
        )}
        <GitHelp />
      </div>

      {/* The three columns. They stack on small screens, each keeping a usable height. */}
      <div className="flex min-h-0 flex-1 flex-col gap-2 lg:flex-row">
        <Panel className="min-h-[13rem] lg:h-auto lg:min-h-0 lg:w-60 lg:shrink-0 xl:w-64">
          <FileTree
            // Remount on a branch switch: a different branch can be a different
            // set of files, and a cached tree would keep showing the old one.
            key={head.branch}
            root={repo.path}
            statusMap={statusMap}
            canWrite={canWrite}
            canDelete={canDestruct}
            activeFile={preview?.kind === "file" ? preview.path : undefined}
            onOpenFile={(path) => setPreview({ kind: "file", path })}
            onConfirm={treeConfirm}
            onChanged={() => status.refresh()}
            onOpenInFiles={(path) => router.push(`/files?path=${encodeURIComponent(path)}`)}
          />
        </Panel>

        <Panel className="flex min-h-[16rem] flex-col lg:h-auto lg:min-h-0 lg:w-[23rem] lg:shrink-0">
          <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-0">
            <TabsList className="m-2 w-auto shrink-0 self-start">
              <TabsTrigger value="changes">
                Changes
                {changeCount > 0 && (
                  <span className="numeric ml-1.5 rounded bg-warning/20 px-1 text-[10px] text-warning">
                    {changeCount}
                  </span>
                )}
              </TabsTrigger>
              <TabsTrigger value="history">History</TabsTrigger>
              <TabsTrigger value="branches">Branches</TabsTrigger>
            </TabsList>
            <TabsContent value="changes" className="min-h-0 flex-1">
              <ChangesPanel
                repoPath={repo.path}
                status={status}
                busy={busy}
                canControl={canControl}
                canDestruct={canDestruct}
                run={run}
                confirm={confirm}
                onSelectDiff={setPreview}
                activePath={preview?.kind === "diff" ? preview.title : undefined}
              />
            </TabsContent>
            <TabsContent value="history" className="min-h-0 flex-1">
              {tab === "history" && (
                <HistoryPanel
                  repoPath={repo.path}
                  canDestruct={canDestruct}
                  confirm={confirm}
                  onSelectDiff={setPreview}
                  onChanged={() => {
                    status.refresh()
                    onRepoChanged()
                  }}
                />
              )}
            </TabsContent>
            <TabsContent value="branches" className="min-h-0 flex-1">
              {tab === "branches" && (
                <BranchesPanel
                  repoPath={repo.path}
                  busy={busy}
                  canControl={canControl}
                  canDestruct={canDestruct}
                  run={run}
                  confirm={confirm}
                  onChanged={() => {
                    status.refresh()
                    onRepoChanged()
                  }}
                />
              )}
            </TabsContent>
          </Tabs>
        </Panel>

        <Panel className="flex min-h-[18rem] flex-1 flex-col lg:h-auto lg:min-h-0">
          <PreviewPanel
            preview={preview}
            canWrite={canWrite}
            onClose={() => setPreview(null)}
            onChanged={() => status.refresh()}
          />
        </Panel>
      </div>
      {dialog}
    </Page>
  )
}

function StashMenu({
  busy,
  stashes,
  clean,
  onStash,
  onPop,
}: {
  busy?: string
  stashes: number
  clean?: boolean
  onStash: () => void
  onPop: () => void
}) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button
              size="sm"
              variant="outline"
              disabled={!!busy}
              aria-label="More git actions"
              className="px-2"
            >
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>Stash and other actions</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuLabel className="text-xs">Set changes aside</DropdownMenuLabel>
        <DropdownMenuItem disabled={clean} onSelect={onStash}>
          <RotateCcw className="size-3.5" />
          Stash changes
        </DropdownMenuItem>
        {stashes > 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={onPop}>
              <Undo2 className="size-3.5" />
              Pop latest stash ({stashes})
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
