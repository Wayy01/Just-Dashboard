"use client"

import { useState } from "react"
import {
  Copy,
  Eye,
  EyeOff,
  GitBranch,
  History,
  KeyRound,
  Plus,
  Rocket,
  RotateCcw,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import { del, get, post, put } from "@/lib/api"
import { relativeTime, shortSha, timestamp } from "@/lib/format"
import type { DeployCommit, DeployProject, DeployRun, EnvVar } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { StatusBadge } from "@/components/status-dot"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

export default function DeployPage() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [detailFor, setDetailFor] = useState<DeployProject | null>(null)
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<DeployProject[]>("/deploy/", undefined, signal),
    10000,
  )

  const deploy = async (project: DeployProject) => {
    try {
      await post(`/deploy/${project.id}/run`)
      toast.success(`Deploying ${project.name}`, { description: "Watch progress in the history." })
      refresh()
    } catch (err) {
      toast.error("Could not start deployment", { description: String(err) })
    }
  }

  return (
    <>
      <PageHeader
        title="Deployments"
        description="Git pull plus container rebuild, triggered by hand or by a signed webhook"
        actions={can("system.admin") && <ProjectDialog onDone={refresh} />}
      />

      {loading && <LoadingRows />}
      {error && <ErrorState error={error} />}
      {data?.length === 0 && (
        <EmptyState
          icon={Rocket}
          title="No projects configured"
          description="Point a project at a git checkout with a compose file, then deploy it from here or from CI."
        />
      )}

      <div className="grid items-start gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        {data?.map((project) => (
          <Card key={project.id}>
            <CardHeader>
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <CardTitle className="truncate text-base">{project.name}</CardTitle>
                  <CardDescription className="truncate font-mono text-xs">
                    {project.repoPath}
                  </CardDescription>
                </div>
                <Badge variant={project.enabled ? "default" : "secondary"}>
                  {project.enabled ? "hook live" : "hook off"}
                </Badge>
              </div>
            </CardHeader>
            <CardContent className="space-y-3">
              <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
                <dt className="text-muted-foreground">Branch</dt>
                <dd className="flex items-center gap-1 font-mono">
                  <GitBranch className="size-3" />
                  {project.branch}
                </dd>
                <dt className="text-muted-foreground">At commit</dt>
                <dd className="font-mono">
                  {shortSha(project.currentSha)}
                  {project.dirty && (
                    <Badge variant="destructive" className="ml-1 text-[10px]">
                      dirty
                    </Badge>
                  )}
                </dd>
                <dt className="text-muted-foreground">Env vars</dt>
                <dd>{project.envVarCount}</dd>
                <dt className="text-muted-foreground">Last deploy</dt>
                <dd>
                  {project.lastRun ? (
                    <span className="flex items-center gap-1.5">
                      <StatusBadge state={project.lastRun.status} />
                      {relativeTime(project.lastRun.startedAt)}
                    </span>
                  ) : (
                    "never"
                  )}
                </dd>
              </dl>

              <div className="flex flex-wrap gap-2">
                <Button size="sm" variant="outline" onClick={() => setDetailFor(project)}>
                  <History className="size-3.5" />
                  History
                </Button>
                {can("service.control") && (
                  <Button size="sm" onClick={() => deploy(project)}>
                    <Rocket className="size-3.5" />
                    Deploy
                  </Button>
                )}
                {can("system.admin") && (
                  <>
                    <HookDialog project={project} />
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-destructive"
                      onClick={() =>
                        confirm({
                          title: "Delete project",
                          phrase: project.name,
                          confirmLabel: "Delete",
                          description: (
                            <p>
                              Removes <b>{project.name}</b> and its webhook. The checkout on disk
                              and its running containers are left alone.
                            </p>
                          ),
                          action: async (c) => {
                            await del(`/deploy/${project.id}`, { confirm: c })
                            refresh()
                          },
                        })
                      }
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </>
                )}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      <ProjectSheet
        project={detailFor}
        onOpenChange={(o) => !o && setDetailFor(null)}
        onChanged={refresh}
      />
      {dialog}
    </>
  )
}

function HookDialog({ project }: { project: DeployProject }) {
  const [secret, setSecret] = useState<string | null>(null)
  const url =
    typeof window !== "undefined"
      ? `${window.location.origin}${project.hookUrl}`
      : (project.hookUrl ?? "")

  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <KeyRound className="size-3.5" />
          Webhook
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Webhook for {project.name}</DialogTitle>
          <DialogDescription>
            POST here to deploy. Requests must carry an HMAC-SHA256 signature of the raw body in
            X-Hub-Signature-256, which is the format GitHub sends by default.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label>URL</Label>
            <div className="flex gap-2">
              <Input readOnly value={url} className="font-mono text-xs" />
              <Button
                size="icon"
                variant="outline"
                onClick={() => {
                  navigator.clipboard.writeText(url)
                  toast.success("Copied")
                }}
              >
                <Copy className="size-4" />
              </Button>
            </div>
          </div>
          {secret ? (
            <Alert>
              <KeyRound className="size-4" />
              <AlertTitle>New secret — copy it now</AlertTitle>
              <AlertDescription className="font-mono text-xs break-all">{secret}</AlertDescription>
            </Alert>
          ) : (
            <p className="text-xs text-muted-foreground">
              The existing secret is stored encrypted and cannot be shown again. Rotating issues a
              new one and immediately invalidates the old.
            </p>
          )}
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={async () => {
              try {
                const res = await post<{ secret: string }>(`/deploy/${project.id}/rotate-secret`)
                setSecret(res.secret)
              } catch (err) {
                toast.error("Could not rotate", { description: String(err) })
              }
            }}
          >
            Rotate secret
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ProjectSheet({
  project,
  onOpenChange,
  onChanged,
}: {
  project: DeployProject | null
  onOpenChange: (open: boolean) => void
  onChanged: () => void
}) {
  const [logFor, setLogFor] = useState<DeployRun | null>(null)

  return (
    <Sheet open={project !== null} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full overflow-auto sm:max-w-3xl">
        <SheetHeader>
          <SheetTitle>{project?.name}</SheetTitle>
          <SheetDescription className="font-mono text-xs">{project?.repoPath}</SheetDescription>
        </SheetHeader>
        {project && (
          <Tabs defaultValue="runs" className="px-4">
            <TabsList>
              <TabsTrigger value="runs">Runs</TabsTrigger>
              <TabsTrigger value="rollback">Rollback</TabsTrigger>
              <TabsTrigger value="env">Environment</TabsTrigger>
            </TabsList>
            <TabsContent value="runs" className="space-y-3">
              <RunsTable project={project} onSelect={setLogFor} />
              {logFor && (
                <div className="space-y-1">
                  <p className="text-sm font-medium">
                    Log · {shortSha(logFor.fromCommit)} → {shortSha(logFor.toCommit)}
                  </p>
                  <pre className="max-h-96 overflow-auto rounded-md border bg-black/40 p-3 font-mono text-xs">
                    {logFor.log || "No output recorded."}
                  </pre>
                </div>
              )}
            </TabsContent>
            <TabsContent value="rollback">
              <RollbackTab project={project} onDone={onChanged} />
            </TabsContent>
            <TabsContent value="env">
              <EnvTab project={project} onChanged={onChanged} />
            </TabsContent>
          </Tabs>
        )}
      </SheetContent>
    </Sheet>
  )
}

function RunsTable({
  project,
  onSelect,
}: {
  project: DeployProject
  onSelect: (run: DeployRun) => void
}) {
  const { data, loading } = usePoll(
    (signal) =>
      get<{ runs: DeployRun[]; running: boolean }>(`/deploy/${project.id}/runs`, undefined, signal),
    5000,
    [project.id],
  )
  if (loading) return <LoadingRows />
  if (!data?.runs.length) return <EmptyState icon={History} title="No deployments yet" />

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Started</TableHead>
          <TableHead>Trigger</TableHead>
          <TableHead>Commit</TableHead>
          <TableHead>Status</TableHead>
          <TableHead className="w-px" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {data.runs.map((run) => (
          <TableRow key={run.id}>
            <TableCell className="text-xs">
              <div>{timestamp(run.startedAt)}</div>
              <p className="text-[11px] text-muted-foreground">{run.duration ?? "running"}</p>
            </TableCell>
            <TableCell className="text-xs">
              {run.trigger}
              {run.actor && <p className="text-[11px] text-muted-foreground">{run.actor}</p>}
            </TableCell>
            <TableCell className="font-mono text-xs">
              {shortSha(run.fromCommit)} → {shortSha(run.toCommit)}
            </TableCell>
            <TableCell>
              <StatusBadge state={run.status} />
            </TableCell>
            <TableCell>
              <Button
                size="sm"
                variant="ghost"
                className="h-7 px-2 text-xs"
                onClick={() => onSelect(run)}
              >
                Log
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function RollbackTab({ project, onDone }: { project: DeployProject; onDone: () => void }) {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const { data, error, loading } = usePoll(
    (signal) => get<DeployCommit[]>(`/deploy/${project.id}/commits`, { limit: 25 }, signal),
    0,
    [project.id],
  )

  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />

  return (
    <>
      <div className="space-y-2">
        <p className="text-sm text-muted-foreground">
          Rolling back re-runs the same pipeline against an older commit, so it is exercised by
          exactly the code path that deploys.
        </p>
        {data?.map((commit) => (
          <div
            key={commit.sha}
            className="flex items-center justify-between gap-3 rounded-md border p-3"
          >
            <div className="min-w-0">
              <p className="truncate text-sm">{commit.subject}</p>
              <p className="font-mono text-[11px] text-muted-foreground">
                {commit.short} · {commit.author} · {relativeTime(commit.date)}
              </p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {commit.sha === project.currentSha ? (
                <Badge>current</Badge>
              ) : (
                can("destructive") && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() =>
                      confirm({
                        title: "Roll back",
                        phrase: project.name,
                        confirmLabel: "Roll back",
                        description: (
                          <>
                            <p>
                              <b>{project.name}</b> is reset to {commit.short} and rebuilt. Whatever
                              is serving now is replaced.
                            </p>
                            <p className="text-xs text-muted-foreground">{commit.subject}</p>
                          </>
                        ),
                        action: async (c) => {
                          await post(
                            `/deploy/${project.id}/rollback`,
                            { commit: commit.sha },
                            { confirm: c },
                          )
                          onDone()
                        },
                      })
                    }
                  >
                    <RotateCcw className="size-3.5" />
                    Roll back
                  </Button>
                )
              )}
            </div>
          </div>
        ))}
      </div>
      {dialog}
    </>
  )
}

function EnvTab({ project, onChanged }: { project: DeployProject; onChanged: () => void }) {
  const { can } = useAuth()
  const [revealed, setRevealed] = useState<EnvVar[] | null>(null)
  const [key, setKey] = useState("")
  const [value, setValue] = useState("")
  const { data, loading, refresh } = usePoll(
    (signal) => get<EnvVar[]>(`/deploy/${project.id}/env`, undefined, signal),
    0,
    [project.id],
  )

  const vars = revealed ?? data ?? []

  const save = async () => {
    try {
      await put(`/deploy/${project.id}/env`, { key, value })
      toast.success(`${key} saved`)
      setKey("")
      setValue("")
      setRevealed(null)
      refresh()
      onChanged()
    } catch (err) {
      toast.error("Could not save", { description: String(err) })
    }
  }

  return (
    <div className="space-y-4">
      <Alert>
        <KeyRound className="size-4" />
        <AlertTitle>Encrypted at rest</AlertTitle>
        <AlertDescription>
          These are written into the project&apos;s .env at deploy time. Revealing them is a
          separate action that is recorded in the audit log.
        </AlertDescription>
      </Alert>

      {loading && <LoadingRows />}

      <div className="space-y-1">
        {vars.map((v) => (
          <div
            key={v.key}
            className="flex items-center justify-between gap-3 rounded-md border px-3 py-2"
          >
            <span className="font-mono text-xs">{v.key}</span>
            <div className="flex items-center gap-2">
              <span className="font-mono text-xs text-muted-foreground">{v.value ?? v.masked}</span>
              {can("system.admin") && (
                <Button
                  size="icon"
                  variant="ghost"
                  className="size-7 text-destructive"
                  onClick={async () => {
                    await del(`/deploy/${project.id}/env/${encodeURIComponent(v.key)}`)
                    setRevealed(null)
                    refresh()
                    onChanged()
                  }}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              )}
            </div>
          </div>
        ))}
        {vars.length === 0 && !loading && (
          <p className="text-sm text-muted-foreground">No variables set.</p>
        )}
      </div>

      {can("system.admin") && (
        <>
          <Button
            size="sm"
            variant="outline"
            onClick={async () => {
              if (revealed) {
                setRevealed(null)
                return
              }
              try {
                setRevealed(await get<EnvVar[]>(`/deploy/${project.id}/env/reveal`))
              } catch (err) {
                toast.error("Could not reveal", { description: String(err) })
              }
            }}
          >
            {revealed ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
            {revealed ? "Hide values" : "Reveal values"}
          </Button>

          <div className="flex gap-2 border-t pt-4">
            <Input
              value={key}
              onChange={(e) => setKey(e.target.value.toUpperCase())}
              placeholder="DATABASE_URL"
              className="w-56 font-mono text-xs"
            />
            <Input
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="value"
              className="flex-1 font-mono text-xs"
              type="password"
            />
            <Button onClick={save} disabled={!key}>
              Set
            </Button>
          </div>
        </>
      )}
    </div>
  )
}

function ProjectDialog({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [repoPath, setRepoPath] = useState("")
  const [branch, setBranch] = useState("main")
  const [composeFile, setComposeFile] = useState("docker-compose.yml")
  const [preCommand, setPreCommand] = useState("")
  const [postCommand, setPostCommand] = useState("")
  const [enabled, setEnabled] = useState(true)
  const [secret, setSecret] = useState<string | null>(null)

  const create = async () => {
    try {
      const res = await post<{ secret: string }>("/deploy/", {
        name,
        repoPath,
        branch,
        composeFile,
        preCommand,
        postCommand,
        enabled,
      })
      setSecret(res.secret)
      onDone()
    } catch (err) {
      toast.error("Could not create project", { description: String(err) })
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (!o) setSecret(null)
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus className="size-4" />
          New project
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>New deploy project</DialogTitle>
        </DialogHeader>

        {secret ? (
          <Alert>
            <KeyRound className="size-4" />
            <AlertTitle>Webhook secret — shown once</AlertTitle>
            <AlertDescription className="font-mono text-xs break-all">{secret}</AlertDescription>
          </Alert>
        ) : (
          <div className="grid gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="p-name">Name</Label>
              <Input id="p-name" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="p-path">Repository path</Label>
              <Input
                id="p-path"
                value={repoPath}
                onChange={(e) => setRepoPath(e.target.value)}
                className="font-mono text-sm"
                placeholder="/srv/my-app"
              />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="p-branch">Branch</Label>
                <Input id="p-branch" value={branch} onChange={(e) => setBranch(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="p-compose">Compose file</Label>
                <Input
                  id="p-compose"
                  value={composeFile}
                  onChange={(e) => setComposeFile(e.target.value)}
                  className="font-mono text-xs"
                />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="p-pre">Pre-deploy command</Label>
              <Input
                id="p-pre"
                value={preCommand}
                onChange={(e) => setPreCommand(e.target.value)}
                className="font-mono text-xs"
                placeholder="bun install && bun run build"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="p-post">Post-deploy command</Label>
              <Input
                id="p-post"
                value={postCommand}
                onChange={(e) => setPostCommand(e.target.value)}
                className="font-mono text-xs"
              />
            </div>
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={enabled} onCheckedChange={setEnabled} />
              Accept webhook deployments
            </label>
          </div>
        )}

        <DialogFooter>
          {secret ? (
            <Button onClick={() => setOpen(false)}>Done</Button>
          ) : (
            <Button onClick={create} disabled={!name || !repoPath}>
              Create
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
