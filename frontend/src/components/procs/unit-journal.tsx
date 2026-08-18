"use client"

import { useCallback, useEffect, useState } from "react"
import type { JournalEntry, LogLine } from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { LogViewer } from "@/components/log-viewer"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

const LOG_LIMIT = 5000

/** syslog priorities, mapped onto the viewer's level vocabulary. */
function levelFor(priority: number): string {
  if (priority <= 2) return "critical"
  if (priority === 3) return "error"
  if (priority === 4) return "warn"
  if (priority <= 6) return "info"
  return "debug"
}

export function UnitJournalSheet({
  unit,
  onOpenChange,
}: {
  unit: string | null
  onOpenChange: (open: boolean) => void
}) {
  const [lines, setLines] = useState<LogLine[]>([])

  useEffect(() => setLines([]), [unit])

  const onMessage = useCallback((envelope: Envelope) => {
    if (envelope.type !== "journal") return
    const batch = envelope.data as JournalEntry[]
    setLines((prev) => {
      const next = [
        ...prev,
        ...batch.map((e) => ({
          text: e.message,
          level: levelFor(e.priority),
          timestamp: e.timestamp,
          source: e.syslogIdentifier,
        })),
      ]
      return next.length > LOG_LIMIT ? next.slice(next.length - LOG_LIMIT) : next
    })
  }, [])

  useSocket(unit ? `/systemd/${encodeURIComponent(unit)}/journal/stream` : "", {
    onMessage,
    enabled: unit !== null,
    query: { lines: 300 },
  })

  return (
    <Sheet open={unit !== null} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-3xl">
        <SheetHeader className="border-b p-4">
          <SheetTitle>{unit}</SheetTitle>
          <SheetDescription>Live journal, newest at the bottom</SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 p-4">
          <LogViewer className="h-full" lines={lines} onClear={() => setLines([])} />
        </div>
      </SheetContent>
    </Sheet>
  )
}
