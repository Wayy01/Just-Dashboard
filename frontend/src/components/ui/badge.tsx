import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Slot } from "radix-ui"

import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-full border border-transparent px-2 py-0.5 text-xs font-medium whitespace-nowrap transition-[color,box-shadow] focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 [&>svg]:pointer-events-none [&>svg]:size-3",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground [a&]:hover:bg-primary/90",
        secondary: "bg-secondary text-secondary-foreground [a&]:hover:bg-secondary/90",
        destructive:
          "bg-destructive text-white focus-visible:ring-destructive/20 dark:bg-destructive/60 dark:focus-visible:ring-destructive/40 [a&]:hover:bg-destructive/90",
        // The tinted variants below are for *tags* — a fixed property of the
        // thing they sit on: "self-signed", "staging", "modified", "no
        // rotation", a security-flagged package. They are not the app's status
        // language: a live running/stopped/failed state, or a health/posture
        // verdict, goes through `Status` in `components/status-dot.tsx`, which
        // is a bare dot and a word with no pill around it. Keep that split —
        // reaching for `variant="critical"` to mark something's *state* puts
        // two vocabularies on screen for one idea.
        success: "border-success/25 bg-success/15 text-success [a&]:hover:bg-success/25",
        warning: "border-warning/25 bg-warning/15 text-warning [a&]:hover:bg-warning/25",
        critical: "border-destructive/25 bg-destructive/15 text-destructive [a&]:hover:bg-destructive/25",
        notice: "border-border bg-muted/50 text-muted-foreground [a&]:hover:bg-muted/70",
        outline:
          "border-border text-foreground [a&]:hover:bg-accent [a&]:hover:text-accent-foreground",
        ghost: "[a&]:hover:bg-accent [a&]:hover:text-accent-foreground",
        link: "text-primary underline-offset-4 [a&]:hover:underline",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
)

function Badge({
  className,
  variant = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"span"> & VariantProps<typeof badgeVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "span"

  return (
    <Comp
      data-slot="badge"
      data-variant={variant}
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    />
  )
}

export { Badge, badgeVariants }
