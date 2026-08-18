"use client"

import { useCallback, useState } from "react"
import type { LogLine } from "@/lib/types"
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

export function PM2LogSheet({
  name,
  onOpenChange,
}: {
  name: string | null
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Sheet open={name !== null} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-3xl">
        <SheetHeader className="border-b p-4">
          <SheetTitle>{name}</SheetTitle>
          <SheetDescription>stdout and stderr, merged live</SheetDescription>
        </SheetHeader>
        {/* Keyed on the process, so switching to another one starts with a
            clean buffer instead of appending to the previous process's. */}
        {name && <PM2LogStream key={name} name={name} />}
      </SheetContent>
    </Sheet>
  )
}

function PM2LogStream({ name }: { name: string }) {
  const [lines, setLines] = useState<LogLine[]>([])

  const onMessage = useCallback((envelope: Envelope) => {
    if (envelope.type !== "logs") return
    const batch = envelope.data as { stream: string; text: string }[]
    setLines((prev) => {
      // stdout and stderr arrive interleaved on one socket; the stream tag is
      // what lets stderr be coloured without re-parsing the text.
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

  useSocket(`/pm2/${encodeURIComponent(name)}/logs/stream`, {
    onMessage,
    query: { lines: 300 },
  })

  return (
    <div className="min-h-0 flex-1 p-4">
      <LogViewer
        className="h-full"
        lines={lines}
        showTimestamps={false}
        onClear={() => setLines([])}
      />
    </div>
  )
}
