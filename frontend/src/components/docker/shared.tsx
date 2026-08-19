"use client"

import type { useConfirm } from "@/components/confirm-dialog"
import { Button } from "@/components/ui/button"

/**
 * Pieces every Docker tab needs.
 *
 * The tabs used to live in the page component alongside it — five independent
 * bodies, each with its own poll, dialogs and table, in one 880-line file.
 * They share nothing but these two, so they are five files now and the page is
 * the shell that arranges them.
 */

/** The confirm() opener from useConfirm, passed down from the page. */
export type ConfirmFn = ReturnType<typeof useConfirm>["confirm"]

export function IconActionButton({
  title,
  icon: Icon,
  onClick,
  destructive,
}: {
  title: string
  icon: React.ComponentType<{ className?: string }>
  onClick: () => void
  destructive?: boolean
}) {
  return (
    <Button
      size="icon"
      variant="ghost"
      title={title}
      aria-label={title}
      className={destructive ? "size-7 text-destructive" : "size-7"}
      onClick={onClick}
    >
      <Icon className="size-3.5" />
    </Button>
  )
}
