"use client"

import { createPortal } from "react-dom"
import {
  CircleCheckIcon,
  InfoIcon,
  Loader2Icon,
  OctagonXIcon,
  TriangleAlertIcon,
} from "lucide-react"
import { useTheme } from "@/hooks/use-theme"
import { usePortalContainer } from "@/lib/portal-container"
import { Toaster as Sonner, type ToasterProps } from "sonner"

const Toaster = ({ ...props }: ToasterProps) => {
  // Sonner paints its own surface, so it has to be told which way the active
  // palette leans or a light theme gets black toasts.
  const { mode } = useTheme()
  // Sonner renders where it is mounted rather than through a portal, and this
  // is mounted in the root layout — outside whatever element is in the
  // browser's fullscreen, which is the only thing the compositor paints. So
  // while something is fullscreen the toaster moves inside it; otherwise every
  // "Saved" and every "Could not save" from a fullscreen workspace is silent.
  // The move remounts it, which sonner survives: live toasts live in its own
  // store, not in the element.
  const fullscreen = usePortalContainer()

  const toaster = (
    <Sonner
      theme={mode}
      className="toaster group"
      icons={{
        success: <CircleCheckIcon className="size-4" />,
        info: <InfoIcon className="size-4" />,
        warning: <TriangleAlertIcon className="size-4" />,
        error: <OctagonXIcon className="size-4" />,
        loading: <Loader2Icon className="size-4 animate-spin" />,
      }}
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
          "--border-radius": "var(--radius)",
        } as React.CSSProperties
      }
      {...props}
    />
  )

  return fullscreen ? createPortal(toaster, fullscreen) : toaster
}

export { Toaster }
