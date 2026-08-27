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

/**
 * The one place a toast is rendered.
 *
 * Three things about sonner's defaults had to be corrected rather than lived
 * with, and all three were visible in the same screenshot of one failed dump:
 *
 * **The close button sits at the top left by default**, which for a toaster
 * anchored to the top right puts it on the far side of the toast from every
 * other control on that edge of the screen, half outside the corner radius. It
 * moves to the trailing edge, where a close button on a right-aligned surface
 * belongs.
 *
 * **The icon is centred against the whole toast**, so a message with a title
 * and a description gets an icon floating between the two lines pointing at
 * neither. It aligns to the first line, which is the one it is about.
 *
 * **The description inherits full-strength foreground**, which gives a title
 * and its detail the same weight and makes a two-line toast read as two
 * unrelated sentences.
 */
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
      toastOptions={{
        classNames: {
          toast: "items-start gap-3 p-4 shadow-lg",
          // Nudged down by the difference between the icon box and the cap
          // height of the title beside it, so the two share a baseline rather
          // than the icon hanging above it.
          icon: "mt-px self-start",
          content: "gap-1",
          title: "text-[13px] font-medium leading-snug",
          description:
            "!text-muted-foreground text-[12px] leading-relaxed break-words group-data-[type=error]:!text-current group-data-[type=error]:opacity-80",
          closeButton:
            "!left-auto !right-2 !top-2 !translate-x-0 !translate-y-0 !border-0 !bg-transparent opacity-60 transition-opacity hover:opacity-100",
          actionButton: "text-xs",
          cancelButton: "text-xs",
        },
      }}
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
          "--border-radius": "var(--radius)",
          // Sonner positions the close button from these rather than from its
          // class, so the class alone is not enough to move it.
          "--toast-close-button-start": "auto",
          "--toast-close-button-end": "0.5rem",
          "--toast-close-button-transform": "none",
        } as React.CSSProperties
      }
      {...props}
    />
  )

  return fullscreen ? createPortal(toaster, fullscreen) : toaster
}

export { Toaster }
