import { cn } from "@/lib/utils"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"

export function StatCard({
  title,
  value,
  detail,
  icon: Icon,
  percent,
  tone = "default",
}: {
  title: string
  value: React.ReactNode
  detail?: React.ReactNode
  icon?: React.ComponentType<{ className?: string }>
  percent?: number
  tone?: "default" | "warning" | "danger"
}) {
  return (
    <Card className="gap-3">
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
        {Icon && <Icon className="size-4 text-muted-foreground" />}
      </CardHeader>
      <CardContent className="space-y-2">
        <div
          className={cn(
            "text-2xl font-semibold tabular-nums",
            tone === "warning" && "text-warning",
            tone === "danger" && "text-destructive",
          )}
        >
          {value}
        </div>
        {percent !== undefined && (
          <Progress value={Math.min(percent, 100)} className={cn("h-1.5", utilisationBar(tone))} />
        )}
        {detail && <p className="text-xs text-muted-foreground">{detail}</p>}
      </CardContent>
    </Card>
  )
}

/**
 * Bar colours for a utilisation tone. A figure that has gone amber next to a
 * bar that is still the default blue reads as two different measurements.
 */
export function utilisationBar(tone: "default" | "warning" | "danger") {
  if (tone === "danger")
    return "bg-destructive/20 [&>[data-slot=progress-indicator]]:bg-destructive"
  if (tone === "warning") return "bg-warning/20 [&>[data-slot=progress-indicator]]:bg-warning"
  return ""
}

/** Thresholds used consistently wherever a utilisation figure is coloured. */
export function utilisationTone(percent: number): "default" | "warning" | "danger" {
  if (percent >= 90) return "danger"
  if (percent >= 75) return "warning"
  return "default"
}
