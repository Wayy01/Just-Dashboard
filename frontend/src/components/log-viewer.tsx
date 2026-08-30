"use client"

import { useEffect, useRef, useState } from "react"
import { ChevronDoubleDown, MagnifyingGlass, Pause, Play } from "@/components/icons"
import { cn } from "@/lib/utils"
import { clock } from "@/lib/format"
import type { LogLine } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"

const LEVEL_CLASS: Record<string, string> = {
  critical: "text-destructive",
  error: "text-destructive",
  warn: "text-warning",
  info: "text-foreground",
  debug: "text-muted-foreground",
}

/**
 * Terminal-style log pane. It follows the tail automatically but stops the
 * moment the reader scrolls up — nothing is more frustrating than losing the
 * line you were reading to an autoscroll.
 */
export function LogViewer({
  lines,
  className,
  emptyMessage = "Waiting for output…",
  onClear,
  toolbar,
  showTimestamps = true,
}: {
  lines: LogLine[]
  className?: string
  emptyMessage?: string
  onClear?: () => void
  toolbar?: React.ReactNode
  showTimestamps?: boolean
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [following, setFollowing] = useState(true)
  const [filter, setFilter] = useState("")

  useEffect(() => {
    if (!following) return
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [lines, following])

  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    setFollowing(atBottom)
  }

  const needle = filter.toLowerCase()
  const visible = needle ? lines.filter((l) => l.text.toLowerCase().includes(needle)) : lines

  return (
    // bg-surface-sunken rather than a flat black: this pane appears inside a
    // light palette too, where a black rectangle is a hole in the page rather
    // than a terminal.
    <div
      className={cn(
        "flex min-w-0 flex-col overflow-hidden rounded-xl border bg-surface-sunken",
        className,
      )}
    >
      <div className="flex flex-wrap items-center gap-2 border-b border-hairline bg-surface-header px-2.5 py-2">
        <div className="relative min-w-40 flex-1">
          <MagnifyingGlass className="absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter these lines"
            className="h-7 pl-7 text-xs"
          />
        </div>
        {toolbar}
        <Badge variant="outline" className="numeric text-[10px] font-normal">
          {visible.length} lines
        </Badge>
        <Button
          size="sm"
          variant={following ? "secondary" : "ghost"}
          className="h-7 gap-1.5 px-2 text-xs"
          onClick={() => {
            setFollowing((f) => !f)
            if (!following && scrollRef.current) {
              scrollRef.current.scrollTop = scrollRef.current.scrollHeight
            }
          }}
        >
          {following ? <Pause className="size-3" /> : <Play className="size-3" />}
          {following ? "Following" : "Paused"}
        </Button>
        {onClear && (
          <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={onClear}>
            Clear
          </Button>
        )}
      </div>

      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="min-h-0 flex-1 overflow-auto p-3 font-mono text-xs leading-relaxed"
      >
        {visible.length === 0 ? (
          <p className="text-muted-foreground">{emptyMessage}</p>
        ) : (
          visible.map((line, i) => (
            <div key={i} className="flex gap-3 whitespace-pre-wrap break-all">
              {showTimestamps && (
                <span className="shrink-0 select-none text-muted-foreground/60">
                  {line.timestamp ? clock(line.timestamp) : ""}
                </span>
              )}
              <span className={cn("flex-1", line.level && LEVEL_CLASS[line.level])}>
                {line.text}
              </span>
            </div>
          ))
        )}
      </div>

      {!following && (
        <button
          className="flex items-center justify-center gap-1.5 border-t border-hairline bg-surface-header py-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
          onClick={() => {
            setFollowing(true)
            if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight
          }}
        >
          <ChevronDoubleDown className="size-3" />
          Jump to latest
        </button>
      )}
    </div>
  )
}
