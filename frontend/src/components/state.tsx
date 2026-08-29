"use client"

import { AlertTriangle, Inbox, Loader2, PlugZap } from "lucide-react"
import { ApiError } from "@/lib/api"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cn("size-4 animate-spin", className)} />
}

export function LoadingRows({ rows = 5, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn("space-y-2", className)}>
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className="h-9 w-full rounded-md" />
      ))}
    </div>
  )
}

/**
 * The skeleton for a panel that is about to hold a table.
 *
 * Its first row is heavier than the rest so the placeholder has the same
 * silhouette as the thing arriving — a header strip over rows — rather than
 * an undifferentiated stack of grey bars that jumps when the data lands.
 */
export function LoadingPanel({ rows = 6, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn("raised min-w-0 overflow-hidden rounded-xl border bg-card", className)}>
      <div className="border-b border-hairline bg-surface-header px-4 py-2.5">
        <Skeleton className="h-4 w-40" />
      </div>
      <div className="divide-y divide-hairline">
        {Array.from({ length: rows }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 px-4 py-3">
            <Skeleton className="h-3.5 flex-1" style={{ maxWidth: `${34 + ((i * 13) % 26)}%` }} />
            <Skeleton className="h-3.5 w-16" />
            <Skeleton className="h-3.5 w-24" />
          </div>
        ))}
      </div>
    </div>
  )
}

export function EmptyState({
  title,
  description,
  icon: Icon = Inbox,
  action,
  className,
}: {
  title: string
  description?: React.ReactNode
  icon?: React.ComponentType<{ className?: string }>
  action?: React.ReactNode
  className?: string
}) {
  return (
    <div
      data-slot="empty-state"
      className={cn(
        "flex min-w-0 flex-col items-center justify-center gap-3 rounded-xl border border-dashed px-6 py-12 text-center",
        className,
      )}
    >
      <span className="flex size-10 items-center justify-center rounded-xl border border-hairline bg-surface-header text-muted-foreground">
        <Icon className="size-4.5" />
      </span>
      <div className="space-y-1">
        <p className="text-[13px] font-medium">{title}</p>
        {description && (
          <p className="mx-auto max-w-md text-xs leading-relaxed text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {action}
    </div>
  )
}

/**
 * Renders a fetch failure.
 *
 * A module that is simply not present on this host (no Docker socket, no
 * systemd, no fail2ban) is shown as information rather than an error, because
 * on a given machine that is a normal state and not something broken. It is
 * the same block either way so the page's shape does not change with the
 * severity — only its colour and its title do.
 */
export function ErrorState({ error, className }: { error: Error; className?: string }) {
  const api = error instanceof ApiError ? error : undefined
  const unavailable =
    api?.code === "docker_unavailable" ||
    api?.code === "not_installed" ||
    api?.code === "no_proxy" ||
    api?.code === "terminal_disabled"

  return (
    <div
      role="alert"
      className={cn(
        "flex min-w-0 items-start gap-3 rounded-xl border p-4",
        unavailable ? "border-hairline bg-card" : "border-destructive/30 bg-destructive/[0.06]",
        className,
      )}
    >
      <span
        className={cn(
          "flex size-8 shrink-0 items-center justify-center rounded-lg",
          unavailable ? "bg-muted text-muted-foreground" : "bg-destructive/12 text-destructive",
        )}
      >
        {unavailable ? <PlugZap className="size-4" /> : <AlertTriangle className="size-4" />}
      </span>
      <div className="min-w-0 space-y-0.5">
        <p className="text-[13px] font-medium">
          {unavailable ? "Not available on this host" : "Something went wrong"}
        </p>
        <p className="text-xs leading-relaxed break-words text-muted-foreground">{error.message}</p>
      </div>
    </div>
  )
}

/**
 * A short banner for a fact the operator needs before acting on the page —
 * lockout protection, an editor that validates before it saves, a shell that
 * runs as root. Deliberately quieter than ErrorState: it is context, not a
 * failure, and a page that shouts everything says nothing.
 */
export function Notice({
  title,
  icon: Icon,
  tone = "default",
  children,
  className,
}: {
  title: React.ReactNode
  icon?: React.ComponentType<{ className?: string }>
  tone?: "default" | "warning" | "danger" | "success"
  children?: React.ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        "flex min-w-0 items-start gap-3 rounded-xl border p-3.5",
        tone === "default" && "border-hairline bg-card",
        tone === "warning" && "border-warning/30 bg-warning/[0.07]",
        tone === "danger" && "border-destructive/30 bg-destructive/[0.06]",
        tone === "success" && "border-success/30 bg-success/[0.07]",
        className,
      )}
    >
      {Icon && (
        <span
          className={cn(
            "flex size-7 shrink-0 items-center justify-center rounded-lg",
            tone === "default" && "bg-muted text-muted-foreground",
            tone === "warning" && "bg-warning/15 text-warning",
            tone === "danger" && "bg-destructive/12 text-destructive",
            tone === "success" && "bg-success/15 text-success",
          )}
        >
          <Icon className="size-3.5" />
        </span>
      )}
      <div className="min-w-0 space-y-1">
        <p className="text-[13px] leading-tight font-medium">{title}</p>
        {children && (
          <div className="text-xs leading-relaxed text-muted-foreground">{children}</div>
        )}
      </div>
    </div>
  )
}
