"use client"

import { useCallback, useMemo, useState } from "react"
import { ChartActivity, Rss, Status, Stop } from "@/components/icons"
import { cn } from "@/lib/utils"
import { relativeTime, timestamp } from "@/lib/format"
import type { DockerEvent, DockerEventFeed } from "@/lib/types"
import { get } from "@/lib/api"
import { usePoll } from "@/hooks/use-poll"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { SearchInput } from "@/components/page"
import { Hint } from "@/components/docker/explain"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

/**
 * What the daemon did, including while nobody was looking.
 *
 * Docker emits an event for everything it does and keeps none of them:
 * `docker events` shows you what happens from the moment you run it, so the
 * answer to "why did this restart at 04:00" is nowhere. The dashboard is a
 * long-running process already connected to the thing producing the record,
 * so it listens and keeps the recent past — the same argument the metrics
 * recorder makes about samples.
 *
 * Two of these events are worth the whole feature on their own: `oom`, which
 * is the only place the kernel killing a container is written down, and
 * `health_status: unhealthy`, which is where "it went bad and came back"
 * leaves a trace that the current state cannot.
 */

const KINDS = [
  { id: "container", label: "Containers" },
  { id: "image", label: "Images" },
  { id: "volume", label: "Volumes" },
  { id: "network", label: "Networks" },
]

export function EventsTab() {
  const [kinds, setKinds] = useState<string[]>(["container"])
  const [search, setSearch] = useState("")
  const [live, setLive] = useState<DockerEvent[]>([])

  const query = useMemo(
    () => ({ limit: 300, kinds: kinds.join(","), search }),
    [kinds, search],
  )

  const { data, error, loading } = usePoll<DockerEventFeed>(
    (signal) => get<DockerEventFeed>("/docker/events", query, signal),
    // The socket below carries anything new; this is the backfill and the
    // filter, so it only needs to re-run when the filter changes.
    0,
    [JSON.stringify(query)],
  )

  // The live feed is unfiltered on the wire — the filters are cheap in the
  // browser once the batch is small, and re-subscribing on every keystroke
  // would drop the socket four times a word.
  const onMessage = useCallback((envelope: Envelope) => {
    if (envelope.type !== "events") return
    const batch = envelope.data as DockerEvent[]
    setLive((prev) => [...batch].concat(prev).slice(0, 500))
  }, [])
  useSocket("/docker/events/stream", { onMessage })

  if (loading && !data) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  const wanted = new Set(kinds)
  const needle = search.trim().toLowerCase()
  const merged = dedupe([...live, ...(data?.events ?? [])]).filter(
    (e) =>
      (wanted.size === 0 || wanted.has(e.type)) &&
      (!needle || `${e.message} ${e.name} ${e.image ?? ""}`.toLowerCase().includes(needle)),
  )

  return (
    <Panel>
      <PanelHeader
        icon={ChartActivity}
        title="Events"
        description={
          data?.listening
            ? `Listening since ${relativeTime(data.since)} · ${data.buffered} kept`
            : "Not connected to the daemon's event stream"
        }
        actions={
          <Badge variant={data?.listening ? "success" : "secondary"} className="gap-1.5 font-normal">
            <Rss className="size-3" />
            {data?.listening ? "live" : "offline"}
          </Badge>
        }
      />
      <PanelToolbar>
        <SearchInput
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Filter events"
        />
        <div className="flex flex-wrap gap-1">
          {KINDS.map((k) => (
            <Button
              key={k.id}
              size="xs"
              variant={kinds.includes(k.id) ? "secondary" : "ghost"}
              onClick={() =>
                setKinds((prev) =>
                  prev.includes(k.id) ? prev.filter((x) => x !== k.id) : [...prev, k.id],
                )
              }
            >
              {k.label}
            </Button>
          ))}
        </div>
      </PanelToolbar>
      <PanelBody flush>
        {merged.length === 0 ? (
          <EmptyState
            icon={ChartActivity}
            title="Nothing yet"
            description={
              data?.listening
                ? "The daemon has done nothing worth recording since the dashboard started listening. Events appear here as they happen."
                : "The dashboard is not connected to Docker's event stream, so nothing is being kept."
            }
          />
        ) : (
          <div className="max-h-[calc(100svh-26rem)] overflow-auto">
            {merged.map((event, i) => (
              <EventRow key={`${event.time}-${event.id}-${i}`} event={event} />
            ))}
          </div>
        )}
      </PanelBody>
      {merged.length > 0 && (
        <div className="border-t border-hairline bg-surface-header/60 px-4 py-2">
          <Hint>
            Kept in memory and bounded, so this is the recent past rather than a permanent record.
            Everything the dashboard itself did is in the audit log, which survives a restart.
          </Hint>
        </div>
      )}
    </Panel>
  )
}

/**
 * The socket sends the buffered past on connect and the poll fetches the same
 * buffer, so the two overlap by design. Identity is the timestamp plus the
 * object: Docker's own event ids are the object's, not the event's.
 */
function dedupe(events: DockerEvent[]): DockerEvent[] {
  const seen = new Set<string>()
  const out: DockerEvent[] = []
  for (const event of events) {
    const key = `${event.time}|${event.type}|${event.action}|${event.id ?? event.name}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push(event)
  }
  return out.sort((a, b) => b.time.localeCompare(a.time))
}

const LEVEL_ICON = {
  error: { icon: Stop, tone: "text-destructive" },
  notice: { icon: Status, tone: "text-primary" },
  info: { icon: Status, tone: "text-muted-foreground/50" },
} as const

function EventRow({ event }: { event: DockerEvent }) {
  const meta = LEVEL_ICON[event.level] ?? LEVEL_ICON.info
  const Icon = meta.icon
  return (
    <div className="flex min-w-0 items-baseline gap-3 border-b border-hairline px-4 py-1.5 text-xs last:border-0 hover:bg-[var(--row-hover)]">
      <Icon className={cn("size-2.5 shrink-0 translate-y-0.5", meta.tone)} />
      <span className="w-32 shrink-0 font-mono text-[11px] text-muted-foreground">
        {timestamp(event.time)}
      </span>
      <span className="min-w-0 flex-1 break-words">{event.message}</span>
      {event.stack && (
        <Badge variant="outline" className="shrink-0 text-[10px] font-normal">
          {event.stack}
        </Badge>
      )}
    </div>
  )
}
