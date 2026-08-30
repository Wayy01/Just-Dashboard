"use client"

import { Check, DesktopDevice, Moon, Sun } from "@/components/icons"
import { useTheme } from "@/hooks/use-theme"
import type { ThemeMode } from "@/lib/themes"
import { cn } from "@/lib/utils"
import { Page, PageHeader, Section } from "@/components/page"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Badge } from "@/components/ui/badge"

export default function AppearancePage() {
  const { mode, setMode } = useTheme()

  return (
    <Page>
      <PageHeader
        eyebrow="You"
        title="Appearance"
        description="Light or dark. It applies immediately and is remembered in this browser."
        actions={
          <Badge variant="outline" className="gap-1.5 font-normal">
            {mode === "dark" ? <Moon className="size-3" /> : <Sun className="size-3" />}
            {mode === "dark" ? "Dark" : "Light"}
          </Badge>
        }
      />

      <Section
        title="Mode"
        description="Just Dashboard's own palette, in the two modes it ships."
      >
        <div className="grid gap-4 sm:grid-cols-2 [&>*]:min-w-0">
          <ModeOption mode="dark" active={mode === "dark"} onPick={setMode} />
          <ModeOption mode="light" active={mode === "light"} onPick={setMode} />
        </div>
      </Section>

      <Panel>
        <PanelHeader icon={DesktopDevice} title="Where this is stored" />
        <PanelBody>
          <p className="max-w-3xl text-xs leading-relaxed text-muted-foreground">
            The choice lives in this browser&apos;s local storage, not on your account — the same
            server can look one way on your laptop and another on your phone. It survives reloads,
            sign-outs and dashboard restarts, and it is applied before the page paints, so there is
            no flash of the previous mode. It is also reachable from the top bar and from the
            command palette (⌘K), so you can flip it without leaving the page you are reading.
          </p>
        </PanelBody>
      </Panel>
    </Page>
  )
}

const MODE_COPY: Record<ThemeMode, { label: string; description: string }> = {
  dark: { label: "Dark", description: "For a room with the lights off, and for reading a graph at 3am." },
  light: { label: "Light", description: "For daylight and for screenshots that end up in a ticket." },
}

function ModeOption({
  mode,
  active,
  onPick,
}: {
  mode: ThemeMode
  active: boolean
  onPick: (mode: ThemeMode) => void
}) {
  const copy = MODE_COPY[mode]
  return (
    <button
      type="button"
      onClick={() => onPick(mode)}
      aria-pressed={active}
      className={cn(
        "raised group min-w-0 rounded-xl border bg-card p-2 text-left transition-all",
        "focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
        active
          ? "border-primary ring-[3px] ring-primary/25"
          : "hover:border-primary/40 hover:bg-accent/40",
      )}
    >
      <ModePreview mode={mode} />
      <div className="min-w-0 px-1.5 pt-2.5 pb-1">
        <div className="flex items-center gap-1.5">
          {mode === "dark" ? (
            <Moon className="size-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <Sun className="size-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate text-[13px] font-medium">{copy.label}</span>
          {active && <Check className="size-3.5 shrink-0 text-primary" />}
        </div>
        <p className="text-xs text-muted-foreground">{copy.description}</p>
      </div>
    </button>
  )
}

/**
 * A miniature of the dashboard drawn in the mode it is offering.
 *
 * `.light`/`.dark` is all it takes: both are classes, not `:root`, precisely
 * so an element that isn't <html> can force one directly — the tokens inside
 * this element resolve to that mode's values while the page around it, in
 * whichever mode it is actually in, stays as it is.
 */
function ModePreview({ mode }: { mode: ThemeMode }) {
  return (
    <div
      className={cn("flex h-32 overflow-hidden rounded-lg border bg-background", mode)}
      aria-hidden
    >
      <div className="flex w-9 shrink-0 flex-col gap-1.5 border-r bg-sidebar p-1.5">
        <div className="h-2 rounded-sm bg-sidebar-primary" />
        <div className="h-1.5 rounded-sm bg-sidebar-foreground/25" />
        <div className="h-1.5 rounded-sm bg-sidebar-accent" />
        <div className="h-1.5 w-3/4 rounded-sm bg-sidebar-foreground/25" />
        <div className="h-1.5 w-2/3 rounded-sm bg-sidebar-foreground/25" />
      </div>

      <div className="flex min-w-0 flex-1 flex-col gap-1.5 p-2">
        <div className="flex items-center gap-1.5">
          <div className="h-1.5 w-10 rounded-full bg-foreground/80" />
          <div className="h-1.5 w-6 rounded-full bg-muted-foreground/50" />
          <span className="ml-auto h-2 w-2 rounded-full bg-success" />
        </div>

        <div className="flex gap-1.5">
          <Tile percent={62} tone="bg-primary" />
          <Tile percent={81} tone="bg-warning" />
          <Tile percent={94} tone="bg-destructive" />
        </div>

        <div className="min-h-0 flex-1 rounded-md border bg-card p-1">
          <Sparkline />
        </div>

        <div className="flex items-center gap-1">
          <div className="h-2.5 w-8 rounded-sm bg-primary" />
          <div className="h-2.5 w-6 rounded-sm bg-secondary" />
          <div className="ml-auto flex gap-0.5">
            {["bg-chart-1", "bg-chart-2", "bg-chart-3", "bg-chart-4", "bg-chart-5"].map((c) => (
              <span key={c} className={cn("size-2 rounded-full", c)} />
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

function Tile({ percent, tone }: { percent: number; tone: string }) {
  return (
    <div className="flex-1 space-y-1 rounded-md border bg-card p-1">
      <div className="h-1 w-2/3 rounded-full bg-muted-foreground/40" />
      <div className="h-1 rounded-full bg-muted">
        <div className={cn("h-1 rounded-full", tone)} style={{ width: `${percent}%` }} />
      </div>
    </div>
  )
}

function Sparkline() {
  return (
    <svg viewBox="0 0 100 28" preserveAspectRatio="none" className="h-full w-full">
      <path
        d="M0 22 L14 15 L28 19 L42 8 L56 13 L70 5 L84 11 L100 6 L100 28 L0 28 Z"
        fill="var(--chart-1)"
        fillOpacity="0.18"
      />
      <path
        d="M0 22 L14 15 L28 19 L42 8 L56 13 L70 5 L84 11 L100 6"
        fill="none"
        stroke="var(--chart-1)"
        strokeWidth="1.5"
        vectorEffect="non-scaling-stroke"
      />
      <path
        d="M0 26 L14 24 L28 25 L42 21 L56 23 L70 18 L84 22 L100 19"
        fill="none"
        stroke="var(--chart-2)"
        strokeWidth="1.5"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}
