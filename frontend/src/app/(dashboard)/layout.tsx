"use client"

import { useEffect } from "react"
import { useRouter } from "next/navigation"
import { Activity } from "lucide-react"
import { useAuth } from "@/hooks/use-auth"
import { MetricsStream } from "@/hooks/use-metrics"
import { AppSidebar } from "@/components/app-sidebar"
import { TopBar } from "@/components/top-bar"
import { CommandPaletteProvider } from "@/components/command-palette"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  const { status, loading } = useAuth()
  const router = useRouter()

  // Redirecting here is a convenience, not a security control: every API call
  // behind this shell is independently authenticated by the server.
  useEffect(() => {
    if (!loading && !status?.authenticated) router.replace("/login")
  }, [loading, status, router])

  if (loading || !status?.authenticated) return <ShellSplash />

  return (
    <CommandPaletteProvider>
      <SidebarProvider style={{ "--sidebar-width": "15.5rem" } as React.CSSProperties}>
        {/* Owns the metrics socket for the whole shell, so the Overview charts
            and the top bar's vitals keep filling while you are on another
            page. Renders nothing. */}
        <MetricsStream />
        <AppSidebar />
        <SidebarInset className="h-svh min-w-0 overflow-hidden">
          <TopBar />
          {/* The scroll lives here rather than on the document, which is what
              keeps the top bar pinned and lets a page ask for the remaining
              height (`<Page fill>`) instead of growing past the viewport. */}
          <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden">{children}</div>
        </SidebarInset>
      </SidebarProvider>
    </CommandPaletteProvider>
  )
}

/**
 * What fills the window while the session probe is in flight.
 *
 * A bare spinner on an empty background reads as a broken page; the mark and
 * the product name say the app is starting, which is what is actually
 * happening — and it is one paint, not a layout that then reflows into the
 * shell.
 */
function ShellSplash() {
  return (
    <div className="auth-backdrop flex min-h-svh flex-col items-center justify-center gap-4 bg-background">
      <span className="flex size-11 items-center justify-center rounded-xl bg-primary text-primary-foreground">
        <Activity className="size-5" />
      </span>
      <div className="flex flex-col items-center gap-1.5">
        <p className="text-sm font-medium">Just Dashboard</p>
        <div className="h-0.5 w-24 overflow-hidden rounded-full bg-muted">
          <div className="h-full w-1/3 animate-[loading_1.2s_ease-in-out_infinite] rounded-full bg-primary" />
        </div>
      </div>
      <style>{`@keyframes loading{0%{transform:translateX(-100%)}100%{transform:translateX(300%)}}`}</style>
    </div>
  )
}
