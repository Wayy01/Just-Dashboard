"use client"

import { useCallback, useMemo, useState } from "react"
import { Download, FileText, RefreshCw, ScrollText } from "lucide-react"
import { toast } from "sonner"
import { downloadUrl, get } from "@/lib/api"
import { bytes, relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { JournalEntry, LogLine, LogSource } from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { usePoll } from "@/hooks/use-poll"
import { PageHeader } from "@/components/page-header"
import { LogViewer } from "@/components/log-viewer"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"

const LOG_LIMIT = 8000
const LEVELS = ["critical", "error", "warn", "info", "debug"] as const

export default function LogsPage() {
  const [selected, setSelected] = useState<LogSource | null>(null)
  const [levels, setLevels] = useState<string[]>([])
  const [grep, setGrep] = useState("")
  const [appliedGrep, setAppliedGrep] = useState("")

  const sources = usePoll((signal) => get<LogSource[]>("/logs/sources", undefined, signal), 60000)

  return (
    <>
      <PageHeader
        title="Logs"
        description="System, nginx, container, PM2 and application logs in one live view"
        actions={
          <>
            {selected?.path && <DownloadDialog source={selected} />}
            <Button variant="outline" size="sm" onClick={() => sources.refresh()}>
              <RefreshCw className="size-4" />
              Rescan
            </Button>
          </>
        }
      />

      <div className="grid min-h-0 flex-1 gap-4 lg:grid-cols-[18rem_1fr] [&>*]:min-w-0">
        <SourceList sources={sources} selected={selected} onSelect={setSelected} />

        <div className="flex min-h-0 flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <form
              className="flex min-w-64 flex-1 gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                setAppliedGrep(grep)
              }}
            >
              <Input
                value={grep}
                onChange={(e) => setGrep(e.target.value)}
                placeholder="Server-side grep — matched before the lines are sent"
                className="flex-1"
              />
              <Button type="submit" variant="secondary">
                Apply
              </Button>
            </form>
            <ToggleGroup type="multiple" value={levels} onValueChange={setLevels} variant="outline">
              {LEVELS.map((level) => (
                <ToggleGroupItem key={level} value={level} className="px-2 text-xs">
                  {level}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>

          {selected ? (
            // Keyed on everything that changes what the socket delivers, so a
            // new filter starts a clean buffer instead of mixing results.
            <LogStream
              key={`${selected.id}|${appliedGrep}|${levels.join(",")}`}
              source={selected}
              grep={appliedGrep}
              levels={levels}
            />
          ) : (
            <EmptyState
              icon={ScrollText}
              title="Pick a log source"
              description="Files, container output, PM2 processes and the systemd journal all stream here."
            />
          )}
        </div>
      </div>
    </>
  )
}

function LogStream({
  source,
  grep,
  levels,
}: {
  source: LogSource
  grep: string
  levels: string[]
}) {
  const [lines, setLines] = useState<LogLine[]>([])

  const onMessage = useCallback((envelope: Envelope) => {
    if (envelope.type === "logs") {
      const batch = envelope.data as LogLine[]
      setLines((prev) => {
        const next = [...prev, ...batch]
        return next.length > LOG_LIMIT ? next.slice(next.length - LOG_LIMIT) : next
      })
    } else if (envelope.type === "journal") {
      const batch = envelope.data as JournalEntry[]
      setLines((prev) => {
        const next = [
          ...prev,
          ...batch.map((e) => ({
            text: e.message,
            timestamp: e.timestamp,
            level: e.priority <= 3 ? "error" : e.priority === 4 ? "warn" : "info",
            source: e.syslogIdentifier,
          })),
        ]
        return next.length > LOG_LIMIT ? next.slice(next.length - LOG_LIMIT) : next
      })
    }
  }, [])

  const query = useMemo(
    () => ({
      source: source.id,
      lines: 400,
      q: grep || undefined,
      levels: levels.length ? levels.join(",") : undefined,
    }),
    [source.id, grep, levels],
  )

  const { state } = useSocket("/logs/stream", { onMessage, query })

  return (
    <>
      <Badge variant={state === "open" ? "default" : "secondary"} className="w-fit">
        {state}
      </Badge>
      <LogViewer
        // The app shell grows with its content, so without a cap a long tail
        // stretches the page and scrolls the filters out of reach. Bounding it
        // here keeps the toolbar fixed and the scrolling inside the pane.
        className="max-h-[calc(100svh-18rem)] min-h-[24rem] flex-1"
        lines={lines}
        onClear={() => setLines([])}
        emptyMessage={state === "open" ? "Waiting for new lines…" : "Connecting…"}
      />
    </>
  )
}

function SourceList({
  sources,
  selected,
  onSelect,
}: {
  sources: ReturnType<typeof usePoll<LogSource[]>>
  selected: LogSource | null
  onSelect: (source: LogSource) => void
}) {
  const grouped = useMemo(() => {
    const map = new Map<string, LogSource[]>()
    for (const s of sources.data ?? []) {
      const list = map.get(s.kind) ?? []
      list.push(s)
      map.set(s.kind, list)
    }
    return [...map.entries()]
  }, [sources.data])

  return (
    <Card className="min-h-0">
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Sources</CardTitle>
        <CardDescription>{sources.data?.length ?? 0} available</CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        <ScrollArea className="h-[calc(100vh-20rem)]">
          {sources.loading && <LoadingRows className="p-4" />}
          {sources.error && <ErrorState error={sources.error} className="m-4" />}
          <div className="space-y-4 p-3">
            {grouped.map(([kind, items]) => (
              <div key={kind}>
                <p className="mb-1 px-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                  {kind}
                </p>
                <div className="space-y-0.5">
                  {items.map((source) => (
                    <button
                      key={source.id}
                      onClick={() => onSelect(source)}
                      // The selected source says so the same way the active nav
                      // item does — one "you are here" language for both.
                      className={cn(
                        "flex w-full flex-col rounded-md px-2 py-1.5 text-left transition-colors",
                        selected?.id === source.id
                          ? "bg-primary/12 font-medium text-foreground"
                          : "hover:bg-accent",
                      )}
                    >
                      <span className="truncate text-sm">{source.label}</span>
                      {source.size !== undefined && source.size > 0 && (
                        <span className="text-[11px] text-muted-foreground">
                          {bytes(source.size)} · {relativeTime(source.modified)}
                        </span>
                      )}
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </ScrollArea>
      </CardContent>
    </Card>
  )
}

function DownloadDialog({ source }: { source: LogSource }) {
  const [since, setSince] = useState("")
  const [until, setUntil] = useState("")

  const href = downloadUrl("/logs/download", {
    path: source.path,
    since: since ? new Date(since).toISOString() : undefined,
    until: until ? new Date(until).toISOString() : undefined,
  })

  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <Download className="size-4" />
          Export
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Export {source.label}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="since">From</Label>
            <Input
              id="since"
              type="datetime-local"
              value={since}
              onChange={(e) => setSince(e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="until">To</Label>
            <Input
              id="until"
              type="datetime-local"
              value={until}
              onChange={(e) => setUntil(e.target.value)}
            />
          </div>
          <p className="text-xs text-muted-foreground">
            Leave both empty to export the whole file. Lines without a parseable timestamp are kept,
            since they continue the record above them.
          </p>
        </div>
        <DialogFooter>
          <Button asChild onClick={() => toast.success("Export started")}>
            <a href={href} download>
              <FileText className="size-4" />
              Download
            </a>
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
