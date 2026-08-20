"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { Box, Eye, EyeOff, ShieldAlert } from "lucide-react"
import { get } from "@/lib/api"
import { duration, timestamp } from "@/lib/format"
import type { ContainerDetail, LogLine } from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { useAuth } from "@/hooks/use-auth"
import { LogViewer } from "@/components/log-viewer"
import { XtermPane } from "@/components/xterm-pane"
import { ErrorState, LoadingRows } from "@/components/state"
import { StatusBadge } from "@/components/status-dot"
import { SidePanel } from "@/components/side-panel"
import { Detail, DetailList } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

/** How many log lines the panel keeps before dropping the oldest. */
const LOG_LIMIT = 5000

export function ContainerDetailSheet({
  containerId,
  onOpenChange,
}: {
  containerId: string | null
  onOpenChange: (open: boolean) => void
}) {
  return (
    <ContainerDetailPanel
      // Keyed on the container so selecting another one starts fresh rather
      // than briefly showing the previous container's detail.
      key={containerId ?? "none"}
      containerId={containerId}
      onOpenChange={onOpenChange}
    />
  )
}

function ContainerDetailPanel({
  containerId,
  onOpenChange,
}: {
  containerId: string | null
  onOpenChange: (open: boolean) => void
}) {
  const { can } = useAuth()
  const [detail, setDetail] = useState<ContainerDetail>()
  const [error, setError] = useState<Error>()
  const [tab, setTab] = useState("overview")

  useEffect(() => {
    if (!containerId) return
    const controller = new AbortController()
    get<ContainerDetail>(`/docker/containers/${containerId}`, undefined, controller.signal)
      .then(setDetail)
      .catch((err) => !controller.signal.aborted && setError(err))
    return () => controller.abort()
  }, [containerId])

  const shell = can("terminal") && detail?.state === "running"

  return (
    <SidePanel
      open={containerId !== null}
      onOpenChange={onOpenChange}
      icon={Box}
      title={
        <>
          {detail?.name ?? "Container"}
          {detail && <StatusBadge state={detail.state} />}
        </>
      }
      description={detail?.image ?? containerId ?? undefined}
      bodyClassName="flex min-h-0 flex-1 flex-col p-4"
    >
      {error && <ErrorState error={error} />}
      {!detail && !error && <LoadingRows />}

      {detail && (
        <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-3">
          <TabsList className="w-fit shrink-0">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="logs">Logs</TabsTrigger>
            <TabsTrigger value="env">Environment</TabsTrigger>
            <TabsTrigger value="mounts">Mounts</TabsTrigger>
            {shell && <TabsTrigger value="shell">Shell</TabsTrigger>}
          </TabsList>

          <TabsContent value="overview" className="min-h-0 flex-1 overflow-y-auto">
            <OverviewFields detail={detail} />
          </TabsContent>

          <TabsContent value="logs" className="min-h-0 flex-1">
            <ContainerLogs containerId={detail.id} active={tab === "logs"} />
          </TabsContent>

          <TabsContent value="env" className="min-h-0 flex-1">
            <EnvironmentList env={detail.env} />
          </TabsContent>

          <TabsContent value="mounts" className="min-h-0 flex-1 space-y-2 overflow-y-auto">
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
              <p className="text-xs text-muted-foreground">No mounts.</p>
            )}
          </TabsContent>

          {shell && (
            <TabsContent value="shell" className="min-h-0 flex-1">
              {tab === "shell" && (
                <XtermPane
                  path={`/docker/containers/${detail.id}/exec`}
                  query={{ rows: 30, cols: 100 }}
                  className="h-full"
                />
              )}
            </TabsContent>
          )}
        </Tabs>
      )}
    </SidePanel>
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
          <ShieldAlert className="mt-px size-3.5 shrink-0" />
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
            {detail.ports.map((p, i) => (
              <Badge key={i} variant="outline" className="font-mono text-[11px] font-normal">
                {p.publicPort ? `${p.ip || "0.0.0.0"}:${p.publicPort} → ` : ""}
                {p.privatePort}/{p.type}
              </Badge>
            ))}
          </div>
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
      }
    />
  )
}
