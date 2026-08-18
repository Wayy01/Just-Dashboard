"use client"

import { AlertCircle, Inbox, Loader2 } from "lucide-react"
import { ApiError } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

export function LoadingRows({ rows = 5, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn("space-y-2", className)}>
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </div>
  )
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cn("size-4 animate-spin", className)} />
}

export function EmptyState({
  title,
  description,
  icon: Icon = Inbox,
  action,
}: {
  title: string
  description?: string
  icon?: React.ComponentType<{ className?: string }>
  action?: React.ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed py-12 text-center">
      <Icon className="size-8 text-muted-foreground" />
      <div className="space-y-1">
        <p className="text-sm font-medium">{title}</p>
        {description && <p className="max-w-md text-sm text-muted-foreground">{description}</p>}
      </div>
      {action}
    </div>
  )
}

/**
 * Renders a fetch failure. A module that is simply not present on this host
 * (no Docker socket, no systemd) is shown as information rather than an error,
 * because on a given machine that is a normal state and not something broken.
 */
export function ErrorState({ error, className }: { error: Error; className?: string }) {
  const api = error instanceof ApiError ? error : undefined
  const unavailable =
    api?.code === "docker_unavailable" ||
    api?.code === "not_installed" ||
    api?.code === "no_proxy" ||
    api?.code === "terminal_disabled"

  return (
    <Alert variant={unavailable ? "default" : "destructive"} className={className}>
      <AlertCircle className="size-4" />
      <AlertTitle>{unavailable ? "Not available on this host" : "Something went wrong"}</AlertTitle>
      <AlertDescription>{error.message}</AlertDescription>
    </Alert>
  )
}
