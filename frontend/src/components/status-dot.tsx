import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"

type Tone = "running" | "stopped" | "warning" | "unknown"

const TONE_CLASS: Record<Tone, string> = {
  running: "bg-emerald-400",
  stopped: "bg-muted-foreground",
  warning: "bg-amber-400",
  unknown: "bg-muted-foreground",
}

/** Maps the many state vocabularies (docker, systemd, pm2) onto one signal. */
export function toneFor(state: string | undefined): Tone {
  switch (state?.toLowerCase()) {
    case "running":
    case "active":
    case "online":
    case "enabled":
    case "success":
      return "running"
    case "paused":
    case "restarting":
    case "activating":
    case "deactivating":
    case "launching":
    case "stopping":
      return "warning"
    case "failed":
    case "errored":
    case "dead":
      return "warning"
    case "exited":
    case "stopped":
    case "inactive":
    case "created":
      return "stopped"
    default:
      return "unknown"
  }
}

export function StatusDot({ state, className }: { state?: string; className?: string }) {
  return <span className={cn("size-1.5 shrink-0 rounded-full", TONE_CLASS[toneFor(state)], className)} />
}

export function StatusBadge({ state, label }: { state?: string; label?: string }) {
  const tone = toneFor(state)
  return (
    <Badge
      variant={tone === "running" ? "default" : tone === "warning" ? "destructive" : "secondary"}
      className="gap-1.5 font-normal"
    >
      <StatusDot state={state} className={tone === "running" ? "bg-emerald-900" : undefined} />
      {label ?? state ?? "unknown"}
    </Badge>
  )
}
