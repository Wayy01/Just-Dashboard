"use client"

import { cn } from "@/lib/utils"
import type { ProbeResult } from "@/lib/types"
import { Badge } from "@/components/ui/badge"

/**
 * One probe's answer, rendered the same on every card: verdict, target,
 * duration, structured records as chips, then the tool's own text verbatim.
 */
export function ToolResult({ result }: { result: ProbeResult }) {
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={result.ok ? "success" : "destructive"} className="font-normal">
          {result.ok ? "answered" : "no answer"}
        </Badge>
        <span className="font-mono text-xs">{result.target}</span>
        <span className="text-[11px] text-muted-foreground">{result.duration}</span>
      </div>
      {result.records && result.records.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {result.records.map((r) => (
            <Badge key={r} variant="outline" className="font-mono font-normal">
              {r}
            </Badge>
          ))}
        </div>
      )}
      <pre
        className={cn(
          "max-h-80 overflow-auto rounded-lg border border-hairline bg-surface-sunken p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap",
          !result.ok && "text-muted-foreground",
        )}
      >
        {result.output || result.error || "No output."}
      </pre>
    </div>
  )
}
