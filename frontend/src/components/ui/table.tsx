"use client"

import * as React from "react"

import { cn } from "@/lib/utils"

function Table({
  className,
  containerClassName,
  ...props
}: React.ComponentProps<"table"> & {
  /**
   * Applied to the scroll container rather than the table. A long table can
   * cap its height here so the page stays a sane length and the filters above
   * it stay on screen, instead of scrolling away after a few hundred rows.
   */
  containerClassName?: string
}) {
  return (
    <div
      data-slot="table-container"
      className={cn("scroll-affordance relative w-full overflow-auto", containerClassName)}
    >
      <table
        data-slot="table"
        className={cn("w-full caption-bottom text-sm", className)}
        {...props}
      />
    </div>
  )
}

function TableHeader({ className, ...props }: React.ComponentProps<"thead">) {
  return <thead data-slot="table-header" className={cn("[&_tr]:border-b", className)} {...props} />
}

/** Header classes for a table inside a capped-height container: the column
 *  names stay put while the rows scroll under them. The background comes from
 *  the rule in globals.css, so a sticky header and a static one match. */
const stickyTableHeader = "sticky top-0 z-10 [&_tr]:border-b"

function TableBody({ className, ...props }: React.ComponentProps<"tbody">) {
  return (
    <tbody
      data-slot="table-body"
      className={cn("[&_tr:last-child]:border-0", className)}
      {...props}
    />
  )
}

function TableFooter({ className, ...props }: React.ComponentProps<"tfoot">) {
  return (
    <tfoot
      data-slot="table-footer"
      className={cn("border-t bg-muted/50 font-medium [&>tr]:last:border-b-0", className)}
      {...props}
    />
  )
}

/** Anything inside a row that owns its own click. */
const INTERACTIVE =
  "a, button, input, select, textarea, label, [role='checkbox'], [role='menuitem'], [contenteditable='true']"

function TableRow({
  className,
  onActivate,
  onClick,
  onKeyDown,
  ...props
}: React.ComponentProps<"tr"> & {
  /**
   * Makes the whole row the hit target for its primary action, rather than
   * asking for a click on the few characters of the name. A click that landed
   * on a control inside the row is left to that control, so the row's gesture
   * and its buttons never fight over the same press.
   */
  onActivate?: () => void
}) {
  return (
    <tr
      data-slot="table-row"
      tabIndex={onActivate ? 0 : undefined}
      className={cn(
        "border-b border-hairline transition-colors hover:bg-[var(--row-hover)] has-aria-expanded:bg-[var(--row-hover)] data-[state=selected]:bg-[var(--row-hover)]",
        onActivate && "cursor-pointer outline-none focus-visible:bg-[var(--row-hover)]",
        className,
      )}
      onClick={(event) => {
        onClick?.(event)
        if (!onActivate || event.defaultPrevented) return
        if ((event.target as HTMLElement).closest(INTERACTIVE)) return
        onActivate()
      }}
      onKeyDown={(event) => {
        onKeyDown?.(event)
        if (!onActivate || event.defaultPrevented) return
        if (event.key !== "Enter" || event.target !== event.currentTarget) return
        event.preventDefault()
        onActivate()
      }}
      {...props}
    />
  )
}

function TableHead({ className, ...props }: React.ComponentProps<"th">) {
  return (
    <th
      data-slot="table-head"
      className={cn(
        "h-9 px-3 text-left align-middle text-[11px] font-semibold tracking-[0.08em] whitespace-nowrap text-muted-foreground uppercase [&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]",
        className,
      )}
      {...props}
    />
  )
}

function TableCell({ className, ...props }: React.ComponentProps<"td">) {
  return (
    <td
      data-slot="table-cell"
      className={cn(
        "px-3 py-2.5 align-middle whitespace-nowrap [&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]",
        className,
      )}
      {...props}
    />
  )
}

function TableCaption({ className, ...props }: React.ComponentProps<"caption">) {
  return (
    <caption
      data-slot="table-caption"
      className={cn("mt-4 text-sm text-muted-foreground", className)}
      {...props}
    />
  )
}

export {
  stickyTableHeader,
  Table,
  TableHeader,
  TableBody,
  TableFooter,
  TableHead,
  TableRow,
  TableCell,
  TableCaption,
}
