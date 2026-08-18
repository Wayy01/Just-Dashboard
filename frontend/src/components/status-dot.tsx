import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"

type Tone = "running" | "stopped" | "warning" | "unknown"

const TONE_CLASS: Record<Tone, string> = {
  running: "bg-success",
  stopped: "bg-muted-foreground",
  warning: "bg-warning",
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
  return (
    <span className={cn("size-1.5 shrink-0 rounded-full", TONE_CLASS[toneFor(state)], className)} />
  )
}

const BADGE_VARIANT = {
  running: "success",
  warning: "warning",
  stopped: "secondary",
  unknown: "secondary",
} as const

export function StatusBadge({ state, label }: { state?: string; label?: string }) {
  return (
    <Badge variant={BADGE_VARIANT[toneFor(state)]} className="gap-1.5 font-normal">
      <StatusDot state={state} />
      {label ?? state ?? "unknown"}
    </Badge>
  )
}
