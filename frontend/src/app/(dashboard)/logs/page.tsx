"use client"

import { useCallback, useMemo, useState } from "react"
import { Download, FileText, RefreshCw, ScrollText } from "lucide-react"
import { notify } from "@/lib/toast"
import { downloadUrl, get } from "@/lib/api"
import { bytes, relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { JournalEntry, LogLine, LogSource } from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { usePoll } from "@/hooks/use-poll"
import { Page, PageHeader, SearchInput } from "@/components/page"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { LogViewer } from "@/components/log-viewer"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
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
  const [picked, setPicked] = useState<LogSource | null>(null)
  const [levels, setLevels] = useState<string[]>([])
  const [grep, setGrep] = useState("")
  const [appliedGrep, setAppliedGrep] = useState("")

  const sources = usePoll((signal) => get<LogSource[]>("/logs/sources", undefined, signal), 60000)

  // The first source is streaming before you choose one. Landing on an empty
  // pane and a "pick something" sign wastes the visit: nine times out of ten
  // the answer is in syslog, and the operator can switch in one click if it is
  // not. Derived rather than stored, so no effect has to sync it.
  const selected = picked ?? sources.data?.[0] ?? null

  return (
    <Page fill>
      <PageHeader
        eyebrow="Server"
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

      <div className="grid min-h-0 flex-1 gap-4 lg:grid-cols-[17rem_minmax(0,1fr)] [&>*]:min-w-0">
        <SourceList sources={sources} selected={selected} onSelect={setPicked} />

        <div className="flex min-h-0 min-w-0 flex-col gap-3">
          <Panel className="shrink-0">
            <PanelToolbar className="border-b-0">
              <form
                className="flex min-w-56 flex-1 gap-2"
                onSubmit={(e) => {
                  e.preventDefault()
                  setAppliedGrep(grep)
                }}
              >
                <Input
                  value={grep}
                  onChange={(e) => setGrep(e.target.value)}
                  placeholder="Server-side grep — matched before the lines are sent"
                  className="h-8 flex-1 text-[13px]"
                />
                <Button type="submit" size="sm" variant="secondary">
                  Apply
                </Button>
              </form>
              <ToggleGroup
                type="multiple"
                value={levels}
                onValueChange={setLevels}
                variant="outline"
                size="sm"
              >
                {LEVELS.map((level) => (
                  <ToggleGroupItem key={level} value={level} className="px-2 text-[11px]">
                    {level}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </PanelToolbar>
          </Panel>

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
              className="flex-1"
              icon={ScrollText}
              title="Pick a log source"
              description="Files, container output, PM2 processes and the systemd journal all stream here."
            />
          )}
        </div>
      </div>
    </Page>
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
    <LogViewer
      // The pane owns the remaining height rather than growing with its
      // content: the filters above stay put and the scrolling happens inside.
      className="min-h-0 flex-1"
      lines={lines}
      onClear={() => setLines([])}
      emptyMessage={state === "open" ? "Waiting for new lines…" : "Connecting…"}
      toolbar={
        <span
          className={cn(
            "flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] font-medium",
            state === "open"
              ? "border-success/25 bg-success/10 text-success"
              : "border-border text-muted-foreground",
          )}
        >
          <span
            className={cn(
              "size-1.5 rounded-full",
              state === "open" ? "bg-success" : "bg-muted-foreground",
            )}
          />
          {state}
        </span>
      }
    />
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
  const [filter, setFilter] = useState("")

  const grouped = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    const map = new Map<string, LogSource[]>()
    for (const s of sources.data ?? []) {
      if (needle && !s.label.toLowerCase().includes(needle) && !s.kind.includes(needle)) continue
      const list = map.get(s.kind) ?? []
      list.push(s)
      map.set(s.kind, list)
    }
    return [...map.entries()]
  }, [sources.data, filter])

  return (
    // Below lg the grid stacks, and an uncapped source list would take half
    // the window from the lines you came to read. Capped here rather than made
    // scrollable-by-the-page, because the page is now exactly the viewport.
    <Panel className="max-h-56 min-h-0 lg:max-h-full">
      <PanelHeader
        icon={ScrollText}
        title="Sources"
        description={`${sources.data?.length ?? 0} available`}
      />
      <PanelToolbar>
        <SearchInput
          containerClassName="w-full"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter sources"
        />
      </PanelToolbar>
      <PanelBody flush scroll className="p-2">
        {sources.loading && <LoadingRows className="p-2" />}
        {sources.error && <ErrorState error={sources.error} className="m-2" />}
        <div className="space-y-3">
          {grouped.map(([kind, items]) => (
            <div key={kind}>
              <p className="eyebrow mb-1 px-2">{kind}</p>
              <div className="space-y-0.5">
                {items.map((source) => (
                  <button
                    key={source.id}
                    onClick={() => onSelect(source)}
                    // The selected source says so the same way the active nav
                    // item does — one "you are here" language for both.
                    className={cn(
                      "flex w-full min-w-0 flex-col rounded-md px-2 py-1.5 text-left transition-colors",
                      selected?.id === source.id
                        ? "bg-primary/12 font-medium text-foreground"
                        : "hover:bg-accent",
                    )}
                  >
                    <span className="truncate text-[13px]">{source.label}</span>
                    {source.size !== undefined && source.size > 0 && (
                      <span className="truncate text-[11px] text-muted-foreground">
                        {bytes(source.size)} · {relativeTime(source.modified)}
                      </span>
                    )}
                  </button>
                ))}
              </div>
            </div>
          ))}
          {!sources.loading && grouped.length === 0 && (
            <p className="px-2 py-6 text-center text-xs text-muted-foreground">
              Nothing matches that filter.
            </p>
          )}
        </div>
      </PanelBody>
    </Panel>
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
          <p className="text-xs leading-relaxed text-muted-foreground">
            Leave both empty to export the whole file. Lines without a parseable timestamp are kept,
            since they continue the record above them.
          </p>
        </div>
        <DialogFooter>
          <Button asChild onClick={() => notify.success("Export started")}>
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
