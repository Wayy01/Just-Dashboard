"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import {
  ArrowCircleUp,
  Box,
  Copy,
  Download,
  Eye,
  EyeOff,
  Pencil,
  ShieldOff,
  Warning,
} from "@/components/icons"
import { notify } from "@/lib/toast"
import { get, post, ApiError } from "@/lib/api"
import { duration, timestamp } from "@/lib/format"
import { useViewState } from "@/lib/view-state"
import type {
  ContainerDetail,
  ContainerSpec,
  DockerDiagnosis,
  FileChange,
  LogLine,
} from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { LogViewer } from "@/components/log-viewer"
import { XtermPane } from "@/components/xterm-pane"
import { ErrorState, LoadingRows, Notice, Spinner } from "@/components/state"
import { Status } from "@/components/status-dot"
import { ContainerUsage } from "@/components/docker/container-usage"
import { ContainerFindings } from "@/components/docker/diagnosis-panel"
import { PortLink } from "@/components/docker/shared"
import { Hint, Term } from "@/components/docker/explain"
import type { ConfirmFn } from "@/components/docker/shared"
import { SidePanel } from "@/components/side-panel"
import { Detail, DetailList } from "@/components/page"
import { Well } from "@/components/panel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

/** How many log lines the panel keeps before dropping the oldest. */
const LOG_LIMIT = 5000

export function ContainerDetailSheet({
  containerId,
  onOpenChange,
  diagnosis,
  confirm,
  onChanged,
  onDuplicate,
}: {
  containerId: string | null
  onOpenChange: (open: boolean) => void
  /** The page's one diagnosis pass, filtered to this container rather than refetched. */
  diagnosis?: DockerDiagnosis
  confirm?: ConfirmFn
  onChanged?: () => void
  /** Opens the create form pre-filled from this container. */
  onDuplicate?: (spec: ContainerSpec) => void
}) {
  return (
    <ContainerDetailPanel
      // Keyed on the container so selecting another one starts fresh rather
      // than briefly showing the previous container's detail.
      key={containerId ?? "none"}
      containerId={containerId}
      onOpenChange={onOpenChange}
      diagnosis={diagnosis}
      confirm={confirm}
      onChanged={onChanged}
      onDuplicate={onDuplicate}
    />
  )
}

function ContainerDetailPanel({
  containerId,
  onOpenChange,
  diagnosis,
  confirm,
  onChanged,
  onDuplicate,
}: {
  containerId: string | null
  onOpenChange: (open: boolean) => void
  diagnosis?: DockerDiagnosis
  confirm?: ConfirmFn
  onChanged?: () => void
  onDuplicate?: (spec: ContainerSpec) => void
}) {
  const { can } = useAuth()
  const [detail, setDetail] = useState<ContainerDetail>()
  const [error, setError] = useState<Error>()
  // Which tab a container opens on. Somebody watching a deploy wants Logs
  // every time, and reopening on Overview is a click paid per container.
  const [tab, setTab] = useViewState("docker.container.tab", "overview")
  const [reloads, setReloads] = useState(0)

  useEffect(() => {
    if (!containerId) return
    const controller = new AbortController()
    get<ContainerDetail>(`/docker/containers/${containerId}`, undefined, controller.signal)
      .then(setDetail)
      .catch((err) => !controller.signal.aborted && setError(err))
    return () => controller.abort()
  }, [containerId, reloads])

  const shell = can("terminal") && detail?.state === "running"

  return (
    <SidePanel
      open={containerId !== null}
      onOpenChange={onOpenChange}
      icon={Box}
      title={
        <>
          {detail?.name ?? "Container"}
          {detail && <Status state={detail.state} />}
        </>
      }
      description={detail?.image ?? containerId ?? undefined}
      bodyClassName="flex min-h-0 flex-1 flex-col p-4"
      actions={
        detail && (
          <ContainerActions
            detail={detail}
            confirm={confirm}
            onDuplicate={onDuplicate}
            onChanged={() => {
              setReloads((n) => n + 1)
              onChanged?.()
            }}
          />
        )
      }
    >
      {error && <ErrorState error={error} />}
      {!detail && !error && <LoadingRows />}

      {detail && (
        <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-3">
          <TabsList className="w-fit shrink-0">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="usage">Usage</TabsTrigger>
            <TabsTrigger value="logs">Logs</TabsTrigger>
            <TabsTrigger value="env">Environment</TabsTrigger>
            <TabsTrigger value="mounts">Storage</TabsTrigger>
            <TabsTrigger value="inspect">Inspect</TabsTrigger>
            {shell && <TabsTrigger value="shell">Shell</TabsTrigger>}
          </TabsList>

          <TabsContent value="overview" className="min-h-0 flex-1 space-y-4 overflow-y-auto">
            {/* What is wrong with this container, before the facts about it. */}
            <ContainerFindings diagnosis={diagnosis} containerId={detail.id} />
            <OverviewFields detail={detail} />
            <Reachability containerId={detail.id} />
          </TabsContent>

          {/* Recorded history rather than a live feed: the point is the spike
              that happened while nobody had this panel open. The limits sit
              above the charts because this is where somebody realises theirs
              are wrong. */}
          <TabsContent value="usage" className="min-h-0 flex-1 space-y-3 overflow-y-auto">
            <ResourceLimitsEditor detail={detail} onSaved={() => setReloads((n) => n + 1)} />
            <ContainerUsage containerId={detail.id} name={detail.name} />
          </TabsContent>

          <TabsContent value="logs" className="min-h-0 flex-1">
            <ContainerLogs containerId={detail.id} active={tab === "logs"} />
          </TabsContent>

          <TabsContent value="env" className="min-h-0 flex-1">
            <EnvironmentList env={detail.env} />
          </TabsContent>

          <TabsContent value="mounts" className="min-h-0 flex-1 space-y-3 overflow-y-auto">
            <WritableLayer containerId={detail.id} />
            {detail.mounts.map((mount, i) => (
              <div key={i} className="rounded-lg border border-hairline p-3 text-xs">
                <div className="mb-1.5 flex items-center gap-2">
                  <Badge variant="outline" className="font-normal">
                    {mount.type}
                  </Badge>
                  <Badge variant={mount.rw ? "default" : "secondary"} className="font-normal">
                    {mount.rw ? "read-write" : "read-only"}
                  </Badge>
                </div>
                <p className="font-mono break-all">
                  <span className="text-muted-foreground">{mount.source}</span>
                  {" → "}
                  {mount.destination}
                </p>
              </div>
            ))}
            {detail.mounts.length === 0 && (
              <Hint>
                Nothing is mounted, so everything this container writes lives in{" "}
                <Term name="writableLayer">its own filesystem</Term> and is destroyed when it is
                replaced.
              </Hint>
            )}
          </TabsContent>

          <TabsContent value="inspect" className="min-h-0 flex-1">
            {tab === "inspect" && <RawInspect containerId={detail.id} />}
          </TabsContent>

          {shell && (
            <TabsContent value="shell" className="min-h-0 flex-1">
              {tab === "shell" && <ContainerShell detail={detail} />}
            </TabsContent>
          )}
        </Tabs>
      )}
    </SidePanel>
  )
}

/**
 * A shell *inside* the container — which is the whole point of it, and the
 * thing most easily mistaken for the Terminal page.
 *
 * The two answer different questions. This one lands wherever the image says:
 * as the image's USER, in its WORKDIR. For most images that is root in `/` or
 * `/app`, and that is not a bug to be fixed — a container shell that quietly
 * became a host login would leave no way to look inside a container at all.
 * The Terminal page is the host; this is the box running on it.
 *
 * Docker already honours the image's user by default, so "default" sends no
 * user at all rather than guessing one. Root is offered because the common
 * reason to open this at all is that something needs installing or reading in
 * an image that deliberately runs unprivileged.
 */
function ContainerShell({ detail }: { detail: ContainerDetail }) {
  const [asRoot, setAsRoot] = useState(false)

  // Most images declare no USER, so the container already runs as root and
  // there is no second account to offer. The toggle used to be drawn anyway,
  // from `detail.user || "root"` — which rendered two buttons both labelled
  // "root" that switched between a request with no user and a request for
  // root, i.e. between the same thing twice.
  const imageUser = detail.user.trim()
  const runsAsRoot =
    imageUser === "" || imageUser === "root" || imageUser.startsWith("0:") || imageUser === "0"
  const account = asRoot || runsAsRoot ? "root" : imageUser

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <span className="min-w-0 flex-1">
          Inside <span className="font-mono text-foreground">{detail.name}</span>, not on the host —
          the Terminal page is the server itself.
        </span>
        {runsAsRoot ? (
          // Worth stating rather than leaving blank: "which account am I" is
          // the first thing you need to know in a container shell, and this
          // one is the answer most people assume without checking.
          <span className="shrink-0">
            as <span className="font-mono text-foreground">root</span> — this image sets no user
          </span>
        ) : (
          <ToggleGroup
            type="single"
            size="sm"
            variant="outline"
            value={asRoot ? "root" : "default"}
            onValueChange={(v) => v && setAsRoot(v === "root")}
          >
            <ToggleGroupItem value="default" className="px-2 text-[11px]">
              {imageUser}
            </ToggleGroupItem>
            <ToggleGroupItem value="root" className="px-2 text-[11px]">
              root
            </ToggleGroupItem>
          </ToggleGroup>
        )}
      </div>
      <XtermPane
        // Keyed on the account, so switching it opens a new exec rather than
        // leaving you in the previous one with a stale label above it.
        key={account}
        path={`/docker/containers/${detail.id}/exec`}
        // No user at all when the image's own is wanted: Docker already
        // honours it, and naming it would override a `user:group` form with
        // just the user half.
        query={{ rows: 30, cols: 100, user: asRoot && !runsAsRoot ? "root" : undefined }}
        className="min-h-0 flex-1"
        subtitle={`${account}@${detail.name} · container shell`}
      />
    </div>
  )
}

/**
 * Names that conventionally hold a credential. The server already withholds
 * these values from anyone below system.admin; this list is what decides
 * whether an admin's copy is printed on screen or kept behind a click, so it
 * errs towards hiding — a needless extra click costs less than a key read over
 * someone's shoulder or captured in a screen share.
 */
const SECRET_ENV_HINTS = [
  "SECRET",
  "PASSWORD",
  "PASSWD",
  "TOKEN",
  "CREDENTIAL",
  "PRIVATE",
  "SALT",
  "SIGNATURE",
  "CIPHER",
  "APIKEY",
  "API_KEY",
  "AUTH",
  "DSN",
  "_KEY",
  "KEY_",
]

function isSecretEnvKey(name: string) {
  const upper = name.toUpperCase()
  return upper === "KEY" || SECRET_ENV_HINTS.some((hint) => upper.includes(hint))
}

function EnvironmentList({ env }: { env: string[] }) {
  const [revealed, setRevealed] = useState<Record<string, boolean>>({})
  const rows = useMemo(
    () =>
      env.map((line) => {
        const eq = line.indexOf("=")
        const name = eq === -1 ? line : line.slice(0, eq)
        const value = eq === -1 ? "" : line.slice(eq + 1)
        return { line, name, value, secret: isSecretEnvKey(name) }
      }),
    [env],
  )
  const secretCount = rows.filter((r) => r.secret).length

  if (rows.length === 0) {
    return <p className="text-xs text-muted-foreground">No environment variables set.</p>
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      {secretCount > 0 && (
        <p className="flex items-start gap-2 text-xs text-muted-foreground">
          <ShieldOff className="mt-px size-3.5 shrink-0" />
          <span>
            {secretCount} {secretCount === 1 ? "value looks" : "values look"} like a credential and
            {secretCount === 1 ? " is" : " are"} hidden. Reveal only when nobody is watching your
            screen.
          </span>
        </p>
      )}
      <div className="min-h-0 flex-1 space-y-0.5 overflow-y-auto font-mono text-xs">
        {rows.map((row) => {
          const show = !row.secret || revealed[row.name]
          return (
            <div
              key={row.line}
              className="flex items-start gap-2 rounded-md px-2 py-1 hover:bg-[var(--row-hover)]"
            >
              <span className="shrink-0 text-muted-foreground">{row.name}=</span>
              {show ? (
                <span className="break-all">{row.value}</span>
              ) : (
                <span className="text-muted-foreground select-none">••••••••••••</span>
              )}
              {row.secret && (
                <Button
                  size="xs"
                  variant="ghost"
                  className="ml-auto shrink-0 font-normal"
                  aria-label={`${revealed[row.name] ? "Hide" : "Reveal"} ${row.name}`}
                  onClick={() => setRevealed((prev) => ({ ...prev, [row.name]: !prev[row.name] }))}
                >
                  {revealed[row.name] ? <EyeOff /> : <Eye />}
                  {revealed[row.name] ? "Hide" : "Reveal"}
                </Button>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function OverviewFields({ detail }: { detail: ContainerDetail }) {
  return (
    <div className="space-y-5">
      <DetailList>
        <Detail label="Container ID">
          <span className="font-mono break-all">{detail.id.slice(0, 20)}</span>
        </Detail>
        <Detail label="Image">
          <span className="font-mono break-all">{detail.image}</span>
        </Detail>
        <Detail label="Command">
          <span className="font-mono break-all">{detail.command || "—"}</span>
        </Detail>
        <Detail label="Created">{timestamp(detail.createdAt)}</Detail>
        <Detail label="Started">
          {detail.startedAt
            ? `${timestamp(detail.startedAt)} (${duration(detail.uptimeSeconds)})`
            : "—"}
        </Detail>
        <Detail label="Restart policy">{detail.restartPolicy || "none"}</Detail>
        <Detail label="Restarts">{detail.restartCount}</Detail>
        <Detail label="Exit code">{detail.state === "running" ? "—" : detail.exitCode}</Detail>
        <Detail label="Network mode">{detail.networkMode}</Detail>
        <Detail label="Working dir">{detail.workingDir || "—"}</Detail>
        <Detail label="User">{detail.user || "default"}</Detail>
        <Detail label="Privileged">
          {detail.privileged ? (
            <Badge variant="destructive" className="font-normal">
              yes — full host access
            </Badge>
          ) : (
            "no"
          )}
        </Detail>
      </DetailList>

      {detail.networkDetails.length > 0 && (
        <div className="space-y-2">
          <p className="eyebrow">Networks</p>
          {detail.networkDetails.map((net) => (
            <div key={net.networkId} className="rounded-lg border border-hairline p-3 text-xs">
              <div className="font-medium">{net.name}</div>
              <p className="font-mono text-muted-foreground">
                {net.ipAddress || "no address"} · gateway {net.gateway || "—"}
              </p>
            </div>
          ))}
        </div>
      )}

      {detail.ports.length > 0 && (
        <div className="space-y-2">
          <p className="eyebrow">Ports</p>
          <div className="flex flex-wrap gap-1.5">
            {/*
              A published port on a known address becomes a link. Obvious once
              seen and absent everywhere: the panel knows the address and the
              port, and an operator's next move after "it is running on 3000"
              is always to open it.
            */}
            {detail.ports.map((p, i) =>
              p.publicPort ? (
                <PortLink key={i} ip={p.ip} port={p.publicPort} target={p.privatePort} />
              ) : (
                <Badge key={i} variant="outline" className="font-mono text-[11px] font-normal">
                  {p.privatePort}/{p.type} · inside only
                </Badge>
              ),
            )}
          </div>
          {detail.ports.some((p) => p.publicPort && (!p.ip || p.ip === "0.0.0.0")) && (
            <Hint>
              A port published on every interface is reachable from anywhere that can route to this
              server. Docker writes it as a NAT rule, which is consulted before the firewall&apos;s
              own.
            </Hint>
          )}
        </div>
      )}
    </div>
  )
}

function ContainerLogs({ containerId, active }: { containerId: string; active: boolean }) {
  const [lines, setLines] = useState<LogLine[]>([])
  const [timestamps, setTimestamps] = useState(true)

  const onMessage = useCallback((envelope: Envelope) => {
    if (envelope.type !== "logs") return
    const batch = envelope.data as { stream: string; text: string }[]
    setLines((prev) => {
      const next = [
        ...prev,
        ...batch.map((l) => ({
          text: l.text,
          level: l.stream === "stderr" ? "error" : undefined,
        })),
      ]
      return next.length > LOG_LIMIT ? next.slice(next.length - LOG_LIMIT) : next
    })
  }, [])

  const query = useMemo(
    () => ({ tail: 500, timestamps: timestamps ? "true" : "false" }),
    [timestamps],
  )
  const { state } = useSocket(`/docker/containers/${containerId}/logs/stream`, {
    onMessage,
    enabled: active,
    query,
  })

  return (
    <LogViewer
      className="h-full"
      lines={lines}
      showTimestamps={false}
      onClear={() => setLines([])}
      emptyMessage={state === "open" ? "No output yet." : "Connecting…"}
      toolbar={
        <>
          <Button
            size="xs"
            variant="ghost"
            onClick={() => {
              setLines([])
              setTimestamps((t) => !t)
            }}
          >
            {timestamps ? "Hide times" : "Show times"}
          </Button>
          {/*
            Saved from what is already in the browser rather than re-fetched.
            The pane holds the tail it was given plus everything since; asking
            the server for a file would be a second, differently-truncated copy
            of the same thing, and this is what the reader is actually looking
            at.
          */}
          <Button
            size="xs"
            variant="ghost"
            onClick={() => downloadLines(containerId, lines)}
            disabled={lines.length === 0}
          >
            <Download className="size-3" />
            Save
          </Button>
        </>
      }
    />
  )
}

/**
 * The actions that change what a container *is*, rather than what it is doing.
 *
 * Docker has no notion of editing a container: every field but a handful of
 * resource limits is fixed at creation, and the universal workaround is to
 * destroy and recreate. Every UI in this class therefore either omits these
 * entirely or hides them behind a "duplicate" button that quietly leaves the
 * original running. Here they are named for what they do, and the server does
 * the destroy-and-recreate with the original parked aside until the
 * replacement is up.
 */
function ContainerActions({
  detail,
  confirm,
  onChanged,
  onDuplicate,
}: {
  detail: ContainerDetail
  confirm?: ConfirmFn
  onChanged: () => void
  onDuplicate?: (spec: ContainerSpec) => void
}) {
  const { can } = useAuth()
  const [busy, setBusy] = useState(false)
  const composeManaged = Boolean(detail.composeStack)

  const duplicate = async () => {
    setBusy(true)
    try {
      const spec = await get<ContainerSpec>(`/docker/containers/${detail.id}/spec`)
      // A copy under the same name would collide, and Docker's error for that
      // is a 409 the operator has to decode. Naming it here is friendlier and
      // is what they were going to type anyway.
      onDuplicate?.({ ...spec, name: `${spec.name}-copy`, start: true })
    } catch (err) {
      notify.error("Could not read this container's settings", err)
    } finally {
      setBusy(false)
    }
  }

  const update = () =>
    confirm?.({
      title: "Update this container",
      confirmLabel: "Update",
      description: (
        <>
          <p>
            Pulls a newer <b>{detail.image}</b> and replaces <b>{detail.name}</b> with a container
            built from it, keeping every setting it has now.
          </p>
          <p>
            Its volumes come with it. Anything written inside the container rather than into a
            volume does not — that is destroyed with the old container.
          </p>
        </>
      ),
      action: async (phrase) => {
        await post(
          `/docker/containers/${detail.id}/recreate`,
          { pullLatest: true },
          { confirm: phrase },
        )
        onChanged()
      },
    })

  return (
    <>
      {can("destructive") && confirm && !composeManaged && (
        <Button size="sm" variant="outline" onClick={update} disabled={busy}>
          <ArrowCircleUp className="size-3.5" />
          Update
        </Button>
      )}
      {composeManaged && (
        <Badge variant="outline" className="font-normal">
          part of {detail.composeStack} — update it from its stack
        </Badge>
      )}
      {can("service.control") && onDuplicate && (
        <Button size="sm" variant="outline" onClick={duplicate} disabled={busy}>
          {busy ? <Spinner className="size-3.5" /> : <Copy className="size-3.5" />}
          Duplicate
        </Button>
      )}
      {can("service.control") && <RenameButton detail={detail} onRenamed={onChanged} />}
    </>
  )
}

function RenameButton({ detail, onRenamed }: { detail: ContainerDetail; onRenamed: () => void }) {
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(detail.name)

  const save = async () => {
    try {
      await post(`/docker/containers/${detail.id}/rename`, { name })
      notify.success(`Renamed to ${name}`)
      setEditing(false)
      onRenamed()
    } catch (err) {
      const message = err instanceof ApiError ? err.message : String(err)
      notify.error("Could not rename it", message)
    }
  }

  if (!editing) {
    return (
      <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
        <Pencil className="size-3.5" />
        Rename
      </Button>
    )
  }
  return (
    <span className="flex items-center gap-1.5">
      <Input
        autoFocus
        value={name}
        spellCheck={false}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") save()
          if (e.key === "Escape") setEditing(false)
        }}
        className="h-8 w-40 font-mono text-xs"
      />
      <Button size="xs" onClick={save} disabled={!name.trim() || name === detail.name}>
        Save
      </Button>
      <Button size="xs" variant="ghost" onClick={() => setEditing(false)}>
        Cancel
      </Button>
    </span>
  )
}

/**
 * What the container has written to itself.
 *
 * Docker exposes this and nothing shows it, which is why "I updated the image
 * and lost my database" is a genre of support question rather than a rare
 * accident: every path here is data in the container's own filesystem, not in
 * a volume, and it is destroyed the next time the container is recreated.
 *
 * The list is filtered to what a person would recognise as their data. A
 * running container touches hundreds of paths under /tmp, /run and /var/log
 * as a matter of course, and showing those buries the one directory that
 * matters in noise.
 */
function WritableLayer({ containerId }: { containerId: string }) {
  const { data, loading } = usePoll<FileChange[]>(
    (signal) => get<FileChange[]>(`/docker/containers/${containerId}/changes`, undefined, signal),
    0,
    [containerId],
  )

  const interesting = useMemo(() => {
    const noise =
      /^\/(tmp|run|proc|sys|dev|var\/(run|log|cache|tmp|lib\/(apt|dpkg))|etc\/(hosts|hostname|resolv\.conf|mtab))/
    return (data ?? []).filter((c) => c.kind !== "deleted" && !noise.test(c.path))
  }, [data])

  // Directories with children collapse to the directory: fifty files under
  // /var/lib/postgresql/data is one fact, not fifty.
  const roots = useMemo(() => {
    const out: string[] = []
    for (const change of interesting) {
      if (!out.some((r) => change.path === r || change.path.startsWith(r + "/"))) {
        out.push(change.path)
      }
    }
    return out.slice(0, 40)
  }, [interesting])

  if (loading || roots.length === 0) return null

  return (
    <Notice title="Written inside the container" icon={Warning} tone="warning">
      <p>
        These paths are in <Term name="writableLayer">the container&apos;s own filesystem</Term>{" "}
        rather than a volume. They are not backed up, and they are destroyed the next time this
        container is recreated — which includes every image update.
      </p>
      <div className="mt-2 max-h-32 space-y-0.5 overflow-auto font-mono text-[11px]">
        {roots.map((path) => (
          <div key={path} className="truncate">
            {path}
          </div>
        ))}
      </div>
    </Notice>
  )
}

/**
 * The Engine's own inspect output, unmodified.
 *
 * Every panel here is a chosen subset of something, and eventually somebody
 * needs the field nobody chose. This is also the check on the rest of the
 * page: an operator who suspects the dashboard is misreporting something can
 * see what it was reading. Credential-shaped environment values are masked on
 * the server for anyone below system.admin, exactly as they are in the typed
 * view — the raw route is not a way around that.
 */
function RawInspect({ containerId }: { containerId: string }) {
  const { data, error, loading } = usePoll<Record<string, unknown>>(
    (signal) =>
      get<Record<string, unknown>>(`/docker/containers/${containerId}/raw`, undefined, signal),
    0,
    [containerId],
  )
  const text = useMemo(() => (data ? JSON.stringify(data, null, 2) : ""), [data])

  if (error) return <ErrorState error={error} />
  if (loading) return <LoadingRows />
  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="flex items-center gap-2">
        <Hint className="flex-1">
          What Docker itself reports about this container. Everything above is a reading of this.
        </Hint>
        <Button
          size="xs"
          variant="outline"
          onClick={() =>
            navigator.clipboard.writeText(text).then(
              () => notify.success("Copied"),
              () => notify.error("Could not copy"),
            )
          }
        >
          <Copy className="size-3" />
          Copy
        </Button>
      </div>
      <Well className="min-h-0 flex-1 whitespace-pre">{text}</Well>
    </div>
  )
}

/**
 * Where this container is actually reachable from.
 *
 * A published port and a reverse-proxy site pointing at it are two facts the
 * dashboard already holds in two different panels. Joining them is what turns
 * "it is running on port 3000" into "it is at https://app.example.com" — and
 * nothing else in this class of tool can do it, because nothing else manages
 * the proxy in front of the containers.
 *
 * The other half is the warning: a port published on every interface is
 * reachable directly, bypassing whatever the proxy in front of it enforces.
 */
function Reachability({ containerId }: { containerId: string }) {
  const { data } = usePoll<
    {
      hostIp?: string
      hostPort: number
      containerPort: number
      public: boolean
      vhost?: string
      url?: string
      tls?: boolean
    }[]
  >((signal) => get(`/docker/containers/${containerId}/routes`, undefined, signal), 0, [
    containerId,
  ])
  if (!data || data.length === 0) return null

  return (
    <section className="space-y-2">
      <p className="eyebrow">Reachable at</p>
      <div className="space-y-1">
        {data.map((route) => (
          <div
            key={`${route.hostIp}-${route.hostPort}`}
            className="flex flex-wrap items-center gap-2 rounded-md border border-hairline px-2.5 py-1.5 text-xs"
          >
            <span className="font-mono text-[11px] text-muted-foreground">
              {route.hostIp || "0.0.0.0"}:{route.hostPort} → {route.containerPort}
            </span>
            {route.url ? (
              <a
                href={route.url}
                target="_blank"
                rel="noreferrer"
                className="font-medium text-primary hover:underline"
              >
                {route.url}
              </a>
            ) : (
              <span className="text-muted-foreground">
                {route.public ? "no proxy site points here" : "this server only"}
              </span>
            )}
            {route.vhost && (
              <Badge variant="outline" className="font-normal">
                via {route.vhost}
                {route.tls ? " · https" : ""}
              </Badge>
            )}
            {route.public && (
              <Badge variant="warning" className="font-normal">
                open to every interface
              </Badge>
            )}
          </div>
        ))}
      </div>
      {data.some((r) => r.public && r.vhost) && (
        <Hint>
          This port is published on every interface <em>and</em> proxied. Anything reaching the port
          directly skips whatever the proxy site enforces in front of it — binding it to 127.0.0.1
          leaves the proxy as the only way in.
        </Hint>
      )}
    </section>
  )
}

/** Saves the lines currently on screen as a text file. */
function downloadLines(containerId: string, lines: LogLine[]) {
  const blob = new Blob([lines.map((l) => l.text).join("\n")], { type: "text/plain" })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement("a")
  anchor.href = url
  anchor.download = `${containerId.slice(0, 12)}-logs.txt`
  anchor.click()
  URL.revokeObjectURL(url)
}

/**
 * The limits, editable in place.
 *
 * Almost nothing about a container can be changed after it is created — this
 * is the exception, and it is worth surfacing separately for that reason: an
 * operator who set a memory limit and got the number wrong should not have to
 * destroy the container to fix it. Docker applies the change to the running
 * cgroup immediately.
 *
 * It sits next to the usage charts because that is where the mistake becomes
 * visible: a container repeatedly touching its ceiling, or one with no ceiling
 * at all climbing towards the host's.
 */
function ResourceLimitsEditor({
  detail,
  onSaved,
}: {
  detail: ContainerDetail
  onSaved: () => void
}) {
  const { can } = useAuth()
  const [memory, setMemory] = useState("")
  const [cpus, setCpus] = useState("")
  const [busy, setBusy] = useState(false)
  const [open, setOpen] = useState(false)

  if (!can("service.control")) return null

  const save = async () => {
    setBusy(true)
    try {
      const res = await post<{ warnings: string[] }>(
        `/docker/containers/${detail.id}/resources`,
        {
          memoryMb: Number(memory) || undefined,
          cpus: Number(cpus) || undefined,
        },
        {},
      )
      if (res.warnings?.length) {
        notify.warning("Applied, with a caveat", { description: res.warnings[0] })
      } else {
        notify.success("Limits updated")
      }
      setOpen(false)
      onSaved()
    } catch (err) {
      const message = err instanceof ApiError ? err.message : String(err)
      notify.error("Could not change the limits", message)
    } finally {
      setBusy(false)
    }
  }

  if (!open) {
    return (
      <div className="flex flex-wrap items-center gap-2 rounded-lg border border-hairline px-3 py-2">
        <span className="min-w-0 flex-1 text-xs text-muted-foreground">
          <Term name="memoryLimit">Limits</Term> can be changed without recreating this container —
          the one part of its configuration Docker will edit in place.
        </span>
        <Button size="xs" variant="outline" onClick={() => setOpen(true)}>
          <Pencil className="size-3" />
          Change limits
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-2 rounded-lg border border-hairline p-3">
      <div className="flex flex-wrap items-end gap-3">
        <div className="w-32">
          <label className="text-[10px] text-muted-foreground" htmlFor="limit-memory">
            Memory (MB)
          </label>
          <Input
            id="limit-memory"
            type="number"
            value={memory}
            placeholder="unlimited"
            onChange={(e) => setMemory(e.target.value)}
            className="h-8 text-xs"
          />
        </div>
        <div className="w-28">
          <label className="text-[10px] text-muted-foreground" htmlFor="limit-cpus">
            CPU cores
          </label>
          <Input
            id="limit-cpus"
            type="number"
            step="0.5"
            value={cpus}
            placeholder="unlimited"
            onChange={(e) => setCpus(e.target.value)}
            className="h-8 text-xs"
          />
        </div>
        <Button size="sm" onClick={save} disabled={busy}>
          {busy && <Spinner className="size-3.5" />}
          Apply
        </Button>
        <Button size="sm" variant="ghost" onClick={() => setOpen(false)} disabled={busy}>
          Cancel
        </Button>
      </div>
      <Hint>
        Leaving a field empty means no change. A memory limit is what makes the kernel kill this
        container rather than choosing a victim across the whole server; the trade is that it will
        be killed when it exceeds it.
      </Hint>
    </div>
  )
}
