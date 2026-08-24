"use client"

import { useMemo, useState } from "react"
import { useSearchParams } from "next/navigation"
import { Check, GitBranch as GitBranchIcon, RefreshCw } from "lucide-react"
import { get } from "@/lib/api"
import { relativeTime } from "@/lib/format"
import type { GitRepo } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { Page, PageHeader, SearchInput } from "@/components/page"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { AheadBehind } from "@/components/git/ahead-behind"
import { GitHelp } from "@/components/git/help"
import { RepoWorkspace } from "@/components/git/repo-workspace"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export default function GitPage() {
  const [selected, setSelected] = useState<GitRepo | null>(null)
  const [filter, setFilter] = useState("")

  const repos = usePoll(
    (signal) => get<{ available: boolean; repos: GitRepo[] }>("/git/", undefined, signal),
    60000,
  )

  // Deep link: a compose stack whose directory is a checkout links straight
  // here. Derived rather than pushed into state, so the workspace opens on the
  // render the repository list arrives without a flash of the list first.
  const requested = useSearchParams().get("repo")
  const [dismissedDeepLink, setDismissedDeepLink] = useState(false)
  const deepLinked =
    requested && !dismissedDeepLink
      ? ((repos.data?.repos ?? []).find((r) => r.path === requested) ?? null)
      : null
  const active = selected ?? deepLinked

  const visible = useMemo(() => {
    const list = repos.data?.repos ?? []
    const needle = filter.trim().toLowerCase()
    if (!needle) return list
    return list.filter(
      (r) =>
        r.name.toLowerCase().includes(needle) ||
        r.path.toLowerCase().includes(needle) ||
        r.branch.toLowerCase().includes(needle),
    )
  }, [repos.data, filter])

  const dirty = repos.data?.repos.filter((r) => r.dirty).length ?? 0

  // A selected repository takes the whole page: the working copy is a place to
  // work, not a panel to peek at, and it needs the room for the tree, the
  // changes and a diff side by side. Every hook above runs first so the branch
  // here never changes the hook order.
  if (active) {
    return (
      <RepoWorkspace
        repo={active}
        onBack={() => {
          setSelected(null)
          setDismissedDeepLink(true)
        }}
        onRepoChanged={() => repos.refresh()}
      />
    )
  }

  return (
    <Page>
      <PageHeader
        eyebrow="Access"
        title="Git"
        description="Every repository on this server — open one to browse its files, stage and commit changes, and manage branches"
        actions={
          <>
            <GitHelp />
            <Button variant="outline" size="sm" onClick={() => repos.refresh()}>
              <RefreshCw className="size-4" />
              Rescan
            </Button>
          </>
        }
      />

      {repos.error && <ErrorState error={repos.error} />}
      {repos.loading && !repos.data && <LoadingPanel />}

      {repos.data && !repos.data.available && (
        <EmptyState
          icon={GitBranchIcon}
          title="git is not installed on this host"
          description="Install git to manage repositories from here."
        />
      )}

      {repos.data?.available &&
        (repos.data.repos.length === 0 ? (
          <EmptyState
            icon={GitBranchIcon}
            title="No repositories found"
            description="Nothing under the configured git roots. Set JD_GIT_ROOTS to point at where your projects live."
          />
        ) : (
          <Panel>
            <PanelHeader
              icon={GitBranchIcon}
              title="Repositories"
              description={`${repos.data.repos.length} found${dirty ? ` · ${dirty} with uncommitted changes` : ""}`}
            />
            <PanelToolbar>
              <SearchInput
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="Filter by name, path or branch"
              />
            </PanelToolbar>
            <PanelBody flush>
              <Table containerClassName="max-h-[calc(100svh-20rem)]">
                <TableHeader className={stickyTableHeader}>
                  <TableRow>
                    <TableHead className="w-full">Repository</TableHead>
                    <TableHead>Branch</TableHead>
                    <TableHead>Working tree</TableHead>
                    <TableHead>Upstream</TableHead>
                    <TableHead>Last commit</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visible.map((repo) => (
                    <TableRow key={repo.path} className="group" onActivate={() => setSelected(repo)}>
                      <TableCell>
                        <div className="max-w-[22rem] min-w-0">
                          <button
                            className="truncate text-left text-[13px] font-medium hover:underline"
                            onClick={() => setSelected(repo)}
                          >
                            {repo.name}
                          </button>
                          <p
                            className="truncate font-mono text-[11px] text-muted-foreground"
                            title={repo.path}
                          >
                            {repo.path}
                          </p>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant={repo.detached ? "destructive" : "secondary"}
                          className="font-normal"
                        >
                          <GitBranchIcon className="size-3" />
                          {repo.branch || "—"}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        {repo.dirty ? (
                          <Badge variant="warning" className="font-normal">
                            {repo.changes} change{repo.changes === 1 ? "" : "s"}
                          </Badge>
                        ) : (
                          <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                            <Check className="size-3" /> clean
                          </span>
                        )}
                      </TableCell>
                      <TableCell>
                        <AheadBehind ahead={repo.ahead} behind={repo.behind} showSynced />
                      </TableCell>
                      <TableCell>
                        <div className="max-w-[20rem] min-w-0">
                          <p className="truncate text-xs" title={repo.subject}>
                            {repo.subject || "—"}
                          </p>
                          <p className="truncate text-[11px] text-muted-foreground">
                            {repo.author}
                            {repo.commitAt ? ` · ${relativeTime(repo.commitAt)}` : ""}
                          </p>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                  {visible.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={5} className="p-0">
                        <EmptyState icon={GitBranchIcon} title="No repository matches that filter" />
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </PanelBody>
          </Panel>
        ))}
    </Page>
  )
}
