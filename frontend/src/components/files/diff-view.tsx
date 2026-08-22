"use client"

import { cn } from "@/lib/utils"

/**
 * A unified diff, coloured the usual way: additions green, removals red, hunk
 * headers in the accent, file/index lines muted.
 *
 * One renderer for every place a diff is shown — the git page, the terminal's
 * companion panel — so the colouring and the line handling stay identical
 * wherever a diff turns up rather than drifting into three near-copies.
 */
export function DiffView({ body, className }: { body: string; className?: string }) {
  return (
    <pre
      className={cn(
        "overflow-auto p-3 font-mono text-[11px] leading-relaxed sm:text-xs",
        className,
      )}
    >
      {body.split("\n").map((line, i) => {
        let cls = ""
        if (line.startsWith("+") && !line.startsWith("+++")) cls = "text-success"
        else if (line.startsWith("-") && !line.startsWith("---")) cls = "text-destructive"
        else if (line.startsWith("@@")) cls = "text-primary"
        else if (line.startsWith("diff ") || line.startsWith("index ")) cls = "text-muted-foreground"
        return (
          <div key={i} className={cn("whitespace-pre", cls)}>
            {line || " "}
          </div>
        )
      })}
    </pre>
  )
}
