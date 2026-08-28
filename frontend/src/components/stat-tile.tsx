import { cn } from "@/lib/utils"

/**
 * A single headline figure.
 *
 * The order is fixed — name, number, meter, detail — because a row of these is
 * read as a table: the eye lands on one column of numbers, not on four cards
 * that each start somewhere different. The name is small caps above the
 * figure rather than beside it, which is what lets the figure be the largest
 * thing in the tile without the label competing for the same line.
 */
export function StatTile({
  label,
  value,
  hint,
  icon: Icon,
  meter,
  tone = "default",
  trailing,
  className,
}: {
  label: React.ReactNode
  value: React.ReactNode
  hint?: React.ReactNode
  icon?: React.ComponentType<{ className?: string }>
  /** 0–100. Draws the utilisation bar under the figure. */
  meter?: number
  tone?: Tone
  /** A badge or delta chip pinned to the right of the figure. */
  trailing?: React.ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        "card-sheen flex min-w-0 flex-col gap-2.5 rounded-xl border bg-card p-4 text-card-foreground",
        className,
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        {Icon && <Icon className="size-3.5 shrink-0 text-muted-foreground" />}
        <p className="eyebrow truncate">{label}</p>
      </div>

      <div className="flex min-w-0 items-baseline justify-between gap-2">
        <span
          className={cn(
            "numeric truncate text-2xl leading-none font-semibold",
            tone === "warning" && "text-warning",
            tone === "danger" && "text-destructive",
            tone === "success" && "text-success",
          )}
        >
          {value}
        </span>
        {trailing && <span className="shrink-0">{trailing}</span>}
      </div>

      {meter !== undefined && (
        <div className="h-1 w-full overflow-hidden rounded-full bg-muted">
          <div
            className={cn("h-full rounded-full transition-[width]", meterFill(tone))}
            style={{ width: `${Math.max(0, Math.min(meter, 100))}%` }}
          />
        </div>
      )}

      {hint && <p className="truncate text-[11px] text-muted-foreground">{hint}</p>}
    </div>
  )
}

export type Tone = "default" | "success" | "warning" | "danger"

function meterFill(tone: Tone) {
  if (tone === "danger") return "bg-destructive"
  if (tone === "warning") return "bg-warning"
  if (tone === "success") return "bg-success"
  return "bg-primary"
}

/**
 * Bar colours for a utilisation tone on a shadcn Progress, where the fill is a
 * child slot. A figure that has gone amber next to a bar that is still the
 * default blue reads as two different measurements.
 */
export function utilisationBar(tone: Tone) {
  if (tone === "danger")
    return "bg-destructive/20 [&>[data-slot=progress-indicator]]:bg-destructive"
  if (tone === "warning") return "bg-warning/20 [&>[data-slot=progress-indicator]]:bg-warning"
  if (tone === "success") return "bg-success/20 [&>[data-slot=progress-indicator]]:bg-success"
  return ""
}

/** Thresholds used consistently wherever a utilisation figure is coloured. */
export function utilisationTone(percent: number): Tone {
  if (percent >= 90) return "danger"
  if (percent >= 75) return "warning"
  return "default"
}
