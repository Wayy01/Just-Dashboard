"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { ChevronRight, Cpu, MemoryStick, Monitor, Moon, Search, Sun } from "lucide-react"
import { cn } from "@/lib/utils"
import { percent } from "@/lib/format"
import { useMetrics } from "@/hooks/use-metrics"
import { useHealth } from "@/hooks/use-metrics-history"
import { HealthBadge } from "@/components/metrics/health-panel"
import { useTheme } from "@/hooks/use-theme"
import { THEMES } from "@/lib/themes"
import { navLocation } from "@/components/app-sidebar"
import { useCommandPalette } from "@/components/command-palette"
import { SidebarTrigger } from "@/components/ui/sidebar"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

/**
 * The one bar across the top of every page.
 *
 * It carries three things and nothing else: where you are, how the machine is
 * doing, and the two controls that are global rather than per-page. The vitals
 * are here rather than only on Overview because the answer to "is the box
 * healthy" should not require navigating away from whatever you are fixing —
 * the metrics socket is already open for the whole shell, so this costs
 * nothing to show.
 */
export function TopBar() {
  const pathname = usePathname()
  const here = navLocation(pathname)
  const palette = useCommandPalette()

  return (
    <header className="sticky top-0 z-20 flex h-14 shrink-0 items-center gap-2 border-b bg-background/85 px-3 backdrop-blur-md md:px-4">
      <SidebarTrigger className="-ml-0.5 size-8 text-muted-foreground" />

      <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1.5 text-[13px]">
        {here?.group && (
          <>
            <span className="hidden truncate text-muted-foreground sm:inline">{here.group}</span>
            <ChevronRight className="hidden size-3.5 shrink-0 text-muted-foreground/60 sm:inline" />
          </>
        )}
        <span className="truncate font-medium">{here?.title ?? "Just Dashboard"}</span>
      </nav>

      <span className="flex-1" />

      <Vitals />

      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="Search"
        className="text-muted-foreground md:hidden"
        onClick={palette.open}
      >
        <Search className="size-4" />
      </Button>

      <ThemeMenu />
    </header>
  )
}

/**
 * CPU, memory and the state of the metrics socket, in the space a page title
 * would waste.
 *
 * It degrades rather than disappears: before the first frame arrives there is
 * still a dot saying whether the socket is up, which is the difference between
 * "quiet" and "disconnected".
 */
function Vitals() {
  const { snapshot, connection } = useMetrics()
  // Polled slowly and shared by every page through the shell, so the verdict
  // follows you around rather than living only on Overview. A server that
  // started filling its disk while you were reading logs should say so from
  // wherever you are.
  const { health } = useHealth()
  const live = connection === "open"

  const steal = snapshot?.cpu.modes?.steal ?? 0

  return (
    <div className="mr-1 flex items-center gap-3">
      {health && health.status !== "ok" && (
        // Only when there is something to say. A permanent green badge in the
        // chrome is a badge nobody looks at, which makes it useless on the day
        // it turns red.
        <Link href="/" aria-label="Health findings" className="hidden sm:block">
          <HealthBadge status={health.status} />
        </Link>
      )}
      {snapshot && (
        <div className="hidden items-center gap-3 md:flex">
          <Reading
            icon={Cpu}
            label="CPU"
            value={snapshot.cpu.totalPercent}
            // Steal named right here rather than folded into the total: on a
            // VPS it is the one figure whose fix is not inside this machine.
            detail={steal >= 1 ? `${percent(steal, 0)} stolen by the hypervisor` : undefined}
            alarm={steal >= 5}
          />
          <Reading
            icon={MemoryStick}
            label="Memory"
            value={snapshot.memory.usedPercent}
            detail={
              snapshot.memory.total > 0
                ? `${percent((snapshot.memory.available / snapshot.memory.total) * 100, 0)} available`
                : undefined
            }
          />
        </div>
      )}
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            className={cn(
              "flex items-center gap-1.5 rounded-full border px-2 py-1 text-[11px] font-medium",
              live
                ? "border-success/25 bg-success/10 text-success"
                : "border-border bg-muted/50 text-muted-foreground",
            )}
          >
            <span
              className={cn(
                "size-1.5 rounded-full",
                live ? "animate-pulse bg-success" : "bg-muted-foreground",
              )}
            />
            <span className="hidden sm:inline">{live ? "Live" : connection}</span>
          </span>
        </TooltipTrigger>
        <TooltipContent>
          {live ? "Streaming metrics every 2 seconds" : `Metrics socket is ${connection}`}
        </TooltipContent>
      </Tooltip>
    </div>
  )
}

function Reading({
  icon: Icon,
  label,
  value,
  detail,
  alarm,
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: number
  /** The second fact the percentage on its own does not carry. */
  detail?: string
  /** Colours the figure regardless of how high the percentage itself is. */
  alarm?: boolean
}) {
  const hot = alarm || value >= 90
  const warm = !hot && value >= 75
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <Icon className="size-3.5" />
          <span
            className={cn(
              "numeric font-medium",
              hot ? "text-destructive" : warm ? "text-warning" : "text-foreground",
            )}
          >
            {percent(value)}
          </span>
        </span>
      </TooltipTrigger>
      <TooltipContent>
        {label} {percent(value)}
        {detail ? ` · ${detail}` : ""}
      </TooltipContent>
    </Tooltip>
  )
}

/**
 * The palette switcher, reachable from anywhere rather than only from the
 * Appearance page — twelve themes are a preference you tune while looking at
 * the screen you are tuning them for.
 */
function ThemeMenu() {
  const { themeId, mode, setTheme } = useTheme()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="Theme" className="text-muted-foreground">
          {mode === "dark" ? <Moon className="size-4" /> : <Sun className="size-4" />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuLabel className="eyebrow">Dark</DropdownMenuLabel>
        {THEMES.filter((t) => t.mode === "dark").map((theme) => (
          <ThemeItem
            key={theme.id}
            id={theme.id}
            name={theme.name}
            active={theme.id === themeId}
            onPick={setTheme}
          />
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuLabel className="eyebrow">Light</DropdownMenuLabel>
        {THEMES.filter((t) => t.mode === "light").map((theme) => (
          <ThemeItem
            key={theme.id}
            id={theme.id}
            name={theme.name}
            active={theme.id === themeId}
            onPick={setTheme}
          />
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <a href="/appearance">
            <Monitor className="size-4" />
            All appearance settings
          </a>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function ThemeItem({
  id,
  name,
  active,
  onPick,
}: {
  id: string
  name: string
  active: boolean
  onPick: (id: string) => void
}) {
  return (
    <DropdownMenuItem onSelect={() => onPick(id)} className="gap-2">
      {/* Each row is drawn in the palette it offers: the swatches carry the
          theme attribute, so they resolve to that theme's tokens inside a menu
          painted in the current one. */}
      <span
        data-theme={id}
        aria-hidden
        className="flex shrink-0 items-center gap-0.5 rounded border border-border bg-background p-0.5"
      >
        <span className="size-2.5 rounded-[2px] bg-primary" />
        <span className="size-2.5 rounded-[2px] bg-chart-3" />
        <span className="size-2.5 rounded-[2px] bg-card" />
      </span>
      <span className="flex-1 text-[13px]">{name}</span>
      {active && <span className="size-1.5 rounded-full bg-primary" />}
    </DropdownMenuItem>
  )
}
