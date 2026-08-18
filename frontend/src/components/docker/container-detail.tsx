"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { get } from "@/lib/api"
import { bytes, duration, relativeTime, timestamp } from "@/lib/format"
import type { ContainerDetail, LogLine } from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { useAuth } from "@/hooks/use-auth"
import { LogViewer } from "@/components/log-viewer"
import { XtermPane } from "@/components/xterm-pane"
import { ErrorState, LoadingRows } from "@/components/state"
import { StatusBadge } from "@/components/status-dot"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

/** How many log lines the panel keeps before dropping the oldest. */
const LOG_LIMIT = 5000

export function ContainerDetailSheet({
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
    setDetail(undefined)
    setError(undefined)
    setTab("overview")
    if (!containerId) return
    const controller = new AbortController()
    get<ContainerDetail>(`/docker/containers/${containerId}`, undefined, controller.signal)
      .then(setDetail)
      .catch((err) => !controller.signal.aborted && setError(err))
    return () => controller.abort()
  }, [containerId])

  return (
    <Sheet open={containerId !== null} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-3xl">
        <SheetHeader className="border-b p-4">
          <SheetTitle className="flex items-center gap-2">
            {detail?.name ?? "Container"}
            {detail && <StatusBadge state={detail.state} />}
          </SheetTitle>
          <SheetDescription className="font-mono text-xs">
            {detail?.image ?? containerId}
          </SheetDescription>
        </SheetHeader>

        {error && <ErrorState error={error} className="m-4" />}
        {!detail && !error && <LoadingRows className="p-4" />}

        {detail && containerId && (
          <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col">
            <TabsList className="mx-4 mt-3 w-fit">
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="logs">Logs</TabsTrigger>
              <TabsTrigger value="env">Environment</TabsTrigger>
              <TabsTrigger value="mounts">Mounts</TabsTrigger>
              {can("terminal") && detail.state === "running" && (
                <TabsTrigger value="shell">Shell</TabsTrigger>
              )}
            </TabsList>

            <TabsContent value="overview" className="min-h-0 flex-1 p-4">
              <ScrollArea className="h-full pr-3">
                <OverviewFields detail={detail} />
              </ScrollArea>
            </TabsContent>

            <TabsContent value="logs" className="min-h-0 flex-1 p-4">
              <ContainerLogs containerId={containerId} active={tab === "logs"} />
            </TabsContent>

            <TabsContent value="env" className="min-h-0 flex-1 p-4">
              <ScrollArea className="h-full pr-3">
                <div className="space-y-1 font-mono text-xs">
                  {detail.env.map((line) => {
                    const [key, ...rest] = line.split("=")
                    return (
                      <div key={line} className="flex gap-2 rounded px-2 py-1 hover:bg-muted">
                        <span className="shrink-0 text-muted-foreground">{key}=</span>
                        <span className="break-all">{rest.join("=")}</span>
                      </div>
                    )
                  })}
                  {detail.env.length === 0 && (
                    <p className="text-muted-foreground">No environment variables set.</p>
                  )}
                </div>
              </ScrollArea>
            </TabsContent>

            <TabsContent value="mounts" className="min-h-0 flex-1 p-4">
              <ScrollArea className="h-full pr-3">
                <div className="space-y-3">
                  {detail.mounts.map((mount, i) => (
                    <div key={i} className="rounded-md border p-3 text-xs">
                      <div className="mb-1 flex items-center gap-2">
                        <Badge variant="outline">{mount.type}</Badge>
                        <Badge variant={mount.rw ? "default" : "secondary"}>
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
                    <p className="text-sm text-muted-foreground">No mounts.</p>
                  )}
                </div>
              </ScrollArea>
            </TabsContent>

            {can("terminal") && detail.state === "running" && (
              <TabsContent value="shell" className="min-h-0 flex-1 p-4">
                {tab === "shell" && (
                  <XtermPane
                    path={`/docker/containers/${containerId}/exec`}
                    query={{ rows: 30, cols: 100 }}
                    className="h-full"
                  />
                )}
              </TabsContent>
            )}
          </Tabs>
        )}
      </SheetContent>
    </Sheet>
  )
}

function OverviewFields({ detail }: { detail: ContainerDetail }) {
  const fields: [string, React.ReactNode][] = [
    ["Container ID", <span key="id" className="font-mono text-xs">{detail.id.slice(0, 20)}</span>],
    ["Image", <span key="img" className="font-mono text-xs">{detail.image}</span>],
    ["Command", <span key="cmd" className="font-mono text-xs">{detail.command || "—"}</span>],
    ["Created", timestamp(detail.createdAt)],
    ["Started", detail.startedAt ? `${timestamp(detail.startedAt)} (${duration(detail.uptimeSeconds)})` : "—"],
    ["Restart policy", detail.restartPolicy || "none"],
    ["Restarts", detail.restartCount],
    ["Exit code", detail.state === "running" ? "—" : detail.exitCode],
    ["Network mode", detail.networkMode],
    ["Working dir", detail.workingDir || "—"],
    ["User", detail.user || "default"],
    [
      "Privileged",
      detail.privileged ? (
        <Badge key="priv" variant="destructive">
          yes — full host access
        </Badge>
      ) : (
        "no"
      ),
    ],
  ]

  return (
    <div className="space-y-4">
      <dl className="grid grid-cols-[9rem_1fr] gap-x-4 gap-y-2 text-sm">
        {fields.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="break-all">{value}</dd>
          </div>
        ))}
      </dl>

      {detail.networkDetails.length > 0 && (
        <div>
          <h3 className="mb-2 text-sm font-medium">Networks</h3>
          <div className="space-y-2">
            {detail.networkDetails.map((net) => (
              <div key={net.networkId} className="rounded-md border p-3 text-xs">
                <div className="font-medium">{net.name}</div>
                <p className="font-mono text-muted-foreground">
                  {net.ipAddress || "no address"} · gateway {net.gateway || "—"}
                </p>
              </div>
            ))}
          </div>
        </div>
      )}

      {detail.ports.length > 0 && (
        <div>
          <h3 className="mb-2 text-sm font-medium">Ports</h3>
          <div className="flex flex-wrap gap-1.5">
            {detail.ports.map((p, i) => (
              <Badge key={i} variant="outline" className="font-mono text-xs">
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

  // Switching container resets the buffer, otherwise the next container's
  // logs would appear appended to the previous one's.
  useEffect(() => setLines([]), [containerId])

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
          size="sm"
          variant="ghost"
          className="h-7 px-2 text-xs"
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
