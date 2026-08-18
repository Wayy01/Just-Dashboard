"use client"

import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

/**
 * An icon-only control in a row of them: restart, download, delete.
 *
 * The label lives in a tooltip rather than a native `title`, which the browser
 * holds back for about a second — a long time to hover over an unlabelled
 * button before finding out it deletes something. It also becomes the button's
 * accessible name, which a `title` only weakly provides.
 */
export function IconAction({
  label,
  className,
  ...props
}: React.ComponentProps<typeof Button> & { label: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={label}
          className={cn("[&_svg:not([class*='size-'])]:size-3.5", className)}
          {...props}
        />
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
