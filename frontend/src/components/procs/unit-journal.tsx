"use client"

import { useCallback, useState } from "react"
import type { JournalEntry, LogLine } from "@/lib/types"
import { useSocket, type Envelope } from "@/hooks/use-socket"
import { ListOrdered } from "@/components/icons"
import { LogViewer } from "@/components/log-viewer"
import { SidePanel } from "@/components/side-panel"

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
  return (
    <SidePanel
      open={unit !== null}
      onOpenChange={onOpenChange}
      icon={ListOrdered}
      title={unit ?? "Unit"}
      description="Live journal, newest at the bottom"
      bodyClassName="flex min-h-0 flex-1 flex-col p-4"
    >
      {/* Keyed on the unit so switching units starts a clean buffer. */}
      {unit && <JournalStream key={unit} unit={unit} />}
    </SidePanel>
  )
}

function JournalStream({ unit }: { unit: string }) {
  const [lines, setLines] = useState<LogLine[]>([])

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

  useSocket(`/systemd/${encodeURIComponent(unit)}/journal/stream`, {
    onMessage,
    query: { lines: 300 },
  })

  return <LogViewer className="h-full" lines={lines} onClear={() => setLines([])} />
}
