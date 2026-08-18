"use client"

import { useCallback, useMemo, useState } from "react"
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  Check,
  GitBranch as GitBranchIcon,
  GitCommitHorizontal,
  History,
  RefreshCw,
  RotateCcw,
  Search,
  Undo2,
} from "lucide-react"
import { toast } from "sonner"
import { get, post } from "@/lib/api"
import { relativeTime, timestamp } from "@/lib/format"
import type { GitBranch, GitCommit, GitRepo, GitResult, GitStatus } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
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

  return (
    <>
      <PageHeader
        title="Git"
        description="Repositories on this server — branch, working tree, history and remotes"
        actions={
          <Button variant="outline" size="sm" onClick={() => repos.refresh()}>
            <RefreshCw className="size-4" />
            Rescan
          </Button>
        }
      />

      {repos.error && <ErrorState error={repos.error} />}
      {repos.loading && !repos.data && <LoadingRows />}

      {repos.data && !repos.data.available && (
        <EmptyState
          icon={GitBranchIcon}
          title="git is not installed on this host"
          description="Install git to manage repositories from here."
        />
      )}

      {repos.data?.available && (
        <>
          <div className="relative max-w-sm">
            <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter by name, path or branch"
              className="pl-8"
            />
          </div>

          {visible.length === 0 ? (
            <EmptyState
              icon={GitBranchIcon}
              title={filter ? "No repository matches that filter" : "No repositories found"}
              description={
                filter
                  ? undefined
                  : "Nothing under the configured git roots. Set VPSD_GIT_ROOTS to point at where your projects live."
              }
            />
          ) : (
            <Card>
              <CardContent className="p-0">
                <Table containerClassName="max-h-[calc(100svh-22rem)]">
                  <TableHeader className={stickyTableHeader}>
                    <TableRow>
                      <TableHead>Repository</TableHead>
                      <TableHead>Branch</TableHead>
                      <TableHead>Working tree</TableHead>
                      <TableHead>Upstream</TableHead>
                      <TableHead>Last commit</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {visible.map((repo) => (
                      <TableRow
                        key={repo.path}
                        className="group"
                        onActivate={() => setSelected(repo)}
                      >
                        <TableCell>
                          <div className="max-w-[22rem] min-w-0">
                            <button
                              className="truncate text-left font-medium hover:underline"
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
                          <Badge variant={repo.detached ? "destructive" : "secondary"}>
                            <GitBranchIcon className="size-3" />
                            {repo.branch || "—"}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          {repo.dirty ? (
                            <Badge variant="default">
                              {repo.changes} change{repo.changes === 1 ? "" : "s"}
                            </Badge>
                          ) : (
                            <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                              <Check className="size-3" /> clean
                            </span>
                          )}
                        </TableCell>
                        <TableCell>
                          <AheadBehind ahead={repo.ahead} behind={repo.behind} />
                        </TableCell>
                        <TableCell>
                          <div className="max-w-[20rem] min-w-0">
                            <p className="truncate text-xs" title={repo.subject}>
                              {repo.subject || "—"}
                            </p>
                            <p className="text-[11px] text-muted-foreground">
                              {repo.author}
                              {repo.commitAt ? ` · ${relativeTime(repo.commitAt)}` : ""}
                            </p>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}
        </>
      )}

      <RepoSheet
        repo={selected}
        onOpenChange={(open) => !open && setSelected(null)}
        onChanged={() => repos.refresh()}
      />
    </>
  )
}

function AheadBehind({ ahead, behind }: { ahead: number; behind: number }) {
  if (!ahead && !behind) {
    return <span className="text-xs text-muted-foreground">in sync</span>
  }
  return (
    <div className="flex items-center gap-1.5 font-mono text-xs tabular-nums">
      {ahead > 0 && (
        <span className="inline-flex items-center gap-0.5 text-success" title="commits to push">
          <ArrowUpFromLine className="size-3" />
          {ahead}
        </span>
      )}
      {behind > 0 && (
        <span className="inline-flex items-center gap-0.5 text-warning" title="commits to pull">
          <ArrowDownToLine className="size-3" />
          {behind}
        </span>
      )}
    </div>
  )
}

function RepoSheet({
  repo,
  onOpenChange,
  onChanged,
}: {
  repo: GitRepo | null
  onOpenChange: (open: boolean) => void
  onChanged: () => void
}) {
  return (
    <Sheet open={repo !== null} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-3xl">
        {/* Keyed on the path so switching repositories starts clean rather
            than briefly showing the previous one's history. */}
        {repo && <RepoBody key={repo.path} repo={repo} onChanged={onChanged} />}
      </SheetContent>
    </Sheet>
  )
}

function RepoBody({ repo, onChanged }: { repo: GitRepo; onChanged: () => void }) {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [tab, setTab] = useState("status")
  const [busy, setBusy] = useState<string>()

  const status = usePoll(
    (signal) => get<GitStatus>("/git/status", { path: repo.path }, signal),
    15000,
    [repo.path],
  )

  const run = useCallback(
    async (label: string, fn: () => Promise<GitResult>) => {
      setBusy(label)
      try {
        const res = await fn()
        toast.success(label, { description: res.output.split("\n").slice(0, 3).join("\n") })
        status.refresh()
        onChanged()
      } catch (err) {
        toast.error(`${label} failed`, { description: String(err) })
      } finally {
        setBusy(undefined)
      }
    },
    [status, onChanged],
  )

  const head = status.data?.repo ?? repo

  return (
    <>
      {dialog}
      <SheetHeader className="border-b p-4">
        <SheetTitle className="flex flex-wrap items-center gap-2">
          {repo.name}
          <Badge variant={head.detached ? "destructive" : "secondary"}>
            <GitBranchIcon className="size-3" />
            {head.branch}
          </Badge>
          <AheadBehind ahead={head.ahead} behind={head.behind} />
        </SheetTitle>
        <SheetDescription className="font-mono text-xs break-all">
          {repo.path}
          {head.remote ? ` · ${head.remote}` : ""}
        </SheetDescription>
      </SheetHeader>

      <div className="flex flex-wrap gap-2 border-b p-3">
        <Button
          size="sm"
          variant="outline"
          disabled={!!busy}
          onClick={() =>
            run("Fetched", () =>
              post<GitResult>("/git/fetch", undefined, { query: { path: repo.path, prune: true } }),
            )
          }
        >
          <RefreshCw className="size-4" />
          Fetch
        </Button>
        {can("service.control") && (
          <>
            <Button
              size="sm"
              variant="outline"
              disabled={!!busy}
              onClick={() =>
                run("Pulled", () =>
                  post<GitResult>("/git/pull", undefined, { query: { path: repo.path } }),
                )
              }
            >
              <ArrowDownToLine className="size-4" />
              Pull
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={!!busy}
              onClick={() =>
                run("Pushed", () =>
                  post<GitResult>("/git/push", undefined, { query: { path: repo.path } }),
                )
              }
            >
              <ArrowUpFromLine className="size-4" />
              Push
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={!!busy || status.data?.clean}
              onClick={() =>
                run("Stashed", () =>
                  post<GitResult>("/git/stash", {}, { query: { path: repo.path } }),
                )
              }
            >
              <Undo2 className="size-4" />
              Stash
            </Button>
            {(status.data?.stashes ?? 0) > 0 && (
              <Button
                size="sm"
                variant="outline"
                disabled={!!busy}
                onClick={() =>
                  run("Stash popped", () =>
                    post<GitResult>("/git/stash/pop", undefined, { query: { path: repo.path } }),
                  )
                }
              >
                <RotateCcw className="size-4" />
                Pop stash ({status.data?.stashes})
              </Button>
            )}
          </>
        )}
      </div>

      <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col">
        <TabsList className="mx-4 mt-3 w-fit">
          <TabsTrigger value="status">Working tree</TabsTrigger>
          <TabsTrigger value="log">History</TabsTrigger>
          <TabsTrigger value="branches">Branches</TabsTrigger>
        </TabsList>

        <TabsContent value="status" className="min-h-0 flex-1 p-4">
          <WorkingTree
            repo={repo}
            status={status}
            busy={busy}
            confirm={confirm}
            onDone={() => {
              status.refresh()
              onChanged()
            }}
          />
        </TabsContent>

        <TabsContent value="log" className="min-h-0 flex-1 p-4">
          {tab === "log" && <HistoryTab path={repo.path} />}
        </TabsContent>

        <TabsContent value="branches" className="min-h-0 flex-1 p-4">
          {tab === "branches" && (
            <BranchesTab
              path={repo.path}
              busy={busy}
              onCheckout={(ref) =>
                run(`Switched to ${ref}`, () =>
                  post<GitResult>("/git/checkout", { ref }, { query: { path: repo.path } }),
                )
              }
              onCreate={(ref) =>
                run(`Created ${ref}`, () =>
                  post<GitResult>("/git/branch", { ref }, { query: { path: repo.path } }),
                )
              }
            />
          )}
        </TabsContent>
      </Tabs>
    </>
  )
}

function WorkingTree({
  repo,
  status,
  busy,
  confirm,
  onDone,
}: {
  repo: GitRepo
  status: ReturnType<typeof usePoll<GitStatus>>
  busy?: string
  confirm: ReturnType<typeof useConfirm>["confirm"]
  onDone: () => void
}) {
  const { can } = useAuth()
  const [diff, setDiff] = useState<{ file: string; body: string } | null>(null)

  if (status.error) return <ErrorState error={status.error} />
  if (status.loading && !status.data) return <LoadingRows />
  if (status.data?.clean) {
    return <EmptyState icon={Check} title="Working tree clean" description="Nothing to commit." />
  }

  const showDiff = async (file: string) => {
    try {
      const res = await get<{ diff: string }>("/git/diff", { path: repo.path, file })
      setDiff({ file, body: res.diff || "No textual diff (binary file or no changes)." })
    } catch (err) {
      toast.error("Could not read the diff", { description: String(err) })
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      <ScrollArea className="min-h-0 flex-1 pr-3">
        <div className="space-y-1">
          {status.data?.files.map((f) => (
            <div
              key={f.path}
              className="flex items-center gap-2 rounded px-2 py-1.5 hover:bg-muted"
            >
              <Badge
                variant={f.label === "untracked" ? "outline" : "secondary"}
                className="w-24 justify-center"
              >
                {f.label}
              </Badge>
              <button
                className="min-w-0 flex-1 truncate text-left font-mono text-xs hover:underline"
                title={f.path}
                onClick={() => showDiff(f.path)}
              >
                {f.path}
              </button>
              {can("destructive") && f.label !== "untracked" && (
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={!!busy}
                  className="h-6 shrink-0 px-2 text-[11px] text-destructive"
                  onClick={() =>
                    confirm({
                      title: "Discard changes",
                      phrase: "discard changes",
                      confirmLabel: "Discard",
                      description: (
                        <p className="text-destructive">
                          Restores <b>{f.path}</b> to its committed state. The current contents are
                          not recoverable.
                        </p>
                      ),
                      action: async (c) => {
                        await post(
                          "/git/discard",
                          { file: f.path },
                          { confirm: c, query: { path: repo.path } },
                        )
                        onDone()
                      },
                    })
                  }
                >
                  Discard
                </Button>
              )}
            </div>
          ))}
        </div>
      </ScrollArea>

      <Sheet open={diff !== null} onOpenChange={(open) => !open && setDiff(null)}>
        <SheetContent side="bottom" className="h-[70svh] p-0">
          <SheetHeader className="border-b p-4">
            <SheetTitle className="font-mono text-sm break-all">{diff?.file}</SheetTitle>
          </SheetHeader>
          <ScrollArea className="h-[calc(70svh-5rem)]">
            <DiffBody body={diff?.body ?? ""} />
          </ScrollArea>
        </SheetContent>
      </Sheet>
    </div>
  )
}

/** Renders a unified diff with the usual colouring for added and removed lines. */
function DiffBody({ body }: { body: string }) {
  return (
    <pre className="overflow-x-auto p-4 font-mono text-xs leading-relaxed">
      {body.split("\n").map((line, i) => {
        let cls = ""
        if (line.startsWith("+") && !line.startsWith("+++")) cls = "text-success"
        else if (line.startsWith("-") && !line.startsWith("---")) cls = "text-destructive"
        else if (line.startsWith("@@")) cls = "text-primary"
        else if (line.startsWith("diff ") || line.startsWith("index "))
          cls = "text-muted-foreground"
        return (
          <div key={i} className={cls}>
            {line || " "}
          </div>
        )
      })}
    </pre>
  )
}

function HistoryTab({ path }: { path: string }) {
  const [open, setOpen] = useState<GitCommit | null>(null)
  const [diff, setDiff] = useState("")
  const log = usePoll((signal) => get<GitCommit[]>("/git/log", { path, limit: 100 }, signal), 0, [
    path,
  ])

  if (log.error) return <ErrorState error={log.error} />
  if (log.loading && !log.data) return <LoadingRows />
  if (!log.data?.length) {
    return <EmptyState icon={History} title="No commits yet" />
  }

  const show = async (c: GitCommit) => {
    setOpen(c)
    setDiff("Loading…")
    try {
      const res = await get<{ diff: string }>("/git/diff", { path, ref: c.sha })
      setDiff(res.diff)
    } catch (err) {
      setDiff(String(err))
    }
  }

  return (
    <>
      <ScrollArea className="h-full pr-3">
        <div className="space-y-1">
          {log.data.map((c) => (
            <button
              key={c.sha}
              onClick={() => show(c)}
              className="flex w-full items-start gap-3 rounded px-2 py-2 text-left hover:bg-muted"
            >
              <GitCommitHorizontal className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm">{c.subject}</p>
                <p className="truncate text-[11px] text-muted-foreground">
                  <span className="font-mono">{c.short}</span> · {c.author} · {relativeTime(c.at)}
                  {c.isMerge ? " · merge" : ""}
                </p>
              </div>
              {(c.insertions > 0 || c.deletions > 0) && (
                <span className="shrink-0 font-mono text-[11px] tabular-nums">
                  <span className="text-success">+{c.insertions}</span>{" "}
                  <span className="text-destructive">−{c.deletions}</span>
                </span>
              )}
            </button>
          ))}
        </div>
      </ScrollArea>

      <Sheet open={open !== null} onOpenChange={(o) => !o && setOpen(null)}>
        <SheetContent side="bottom" className="h-[70svh] p-0">
          <SheetHeader className="border-b p-4">
            <SheetTitle className="text-sm">{open?.subject}</SheetTitle>
            <SheetDescription className="font-mono text-xs">
              {open?.short} · {open?.author} · {open ? timestamp(open.at) : ""}
            </SheetDescription>
          </SheetHeader>
          <ScrollArea className="h-[calc(70svh-6rem)]">
            <DiffBody body={diff} />
          </ScrollArea>
        </SheetContent>
      </Sheet>
    </>
  )
}

function BranchesTab({
  path,
  busy,
  onCheckout,
  onCreate,
}: {
  path: string
  busy?: string
  onCheckout: (ref: string) => void
  onCreate: (ref: string) => void
}) {
  const { can } = useAuth()
  const [newBranch, setNewBranch] = useState("")
  const branches = usePoll((signal) => get<GitBranch[]>("/git/branches", { path }, signal), 0, [
    path,
  ])

  if (branches.error) return <ErrorState error={branches.error} />
  if (branches.loading && !branches.data) return <LoadingRows />

  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      {can("service.control") && (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            const name = newBranch.trim()
            if (!name) return
            onCreate(name)
            setNewBranch("")
          }}
        >
          <Input
            value={newBranch}
            onChange={(e) => setNewBranch(e.target.value)}
            placeholder="New branch from HEAD"
            className="h-8 text-sm"
          />
          <Button type="submit" size="sm" variant="outline" disabled={!!busy || !newBranch.trim()}>
            Create
          </Button>
        </form>
      )}
      <ScrollArea className="min-h-0 flex-1 pr-3">
        <div className="space-y-1">
          {branches.data?.map((b) => (
            <div
              key={b.name}
              className="flex items-center gap-2 rounded px-2 py-1.5 hover:bg-muted"
            >
              <GitBranchIcon
                className={`size-3.5 shrink-0 ${b.current ? "text-success" : "text-muted-foreground"}`}
              />
              <div className="min-w-0 flex-1">
                <p className="truncate font-mono text-xs">
                  {b.name}
                  {b.current && <span className="ml-2 text-[10px] text-success">current</span>}
                </p>
                {b.subject && (
                  <p className="truncate text-[11px] text-muted-foreground">{b.subject}</p>
                )}
              </div>
              <AheadBehind ahead={b.ahead} behind={b.behind} />
              {can("service.control") && !b.current && (
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={!!busy}
                  className="h-6 shrink-0 px-2 text-[11px]"
                  onClick={() => onCheckout(b.name)}
                >
                  Switch
                </Button>
              )}
            </div>
          ))}
        </div>
      </ScrollArea>
    </div>
  )
}
