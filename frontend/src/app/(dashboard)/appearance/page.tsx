"use client"

import { Check, Monitor, Moon, Sun } from "lucide-react"
import { useTheme } from "@/hooks/use-theme"
import { THEMES, type Theme } from "@/lib/themes"
import { cn } from "@/lib/utils"
import { Page, PageHeader, Section } from "@/components/page"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Badge } from "@/components/ui/badge"

export default function AppearancePage() {
  const { themeId, theme, setTheme } = useTheme()

  const dark = THEMES.filter((t) => t.mode === "dark")
  const light = THEMES.filter((t) => t.mode === "light")

  return (
    <Page>
      <PageHeader
        eyebrow="You"
        title="Appearance"
        description="Pick a palette for Just Dashboard. It applies immediately and is remembered in this browser."
        actions={
          <Badge variant="outline" className="gap-1.5 font-normal">
            {theme.mode === "dark" ? <Moon className="size-3" /> : <Sun className="size-3" />}
            {theme.name}
          </Badge>
        }
      />

      <Section
        title={
          <span className="flex items-center gap-2">
            <Moon className="size-3.5 text-muted-foreground" />
            Dark
          </span>
        }
        description="For a room with the lights off, and for reading a graph at 3am."
      >
        <ThemeGrid themes={dark} active={themeId} onPick={setTheme} />
      </Section>

      <Section
        title={
          <span className="flex items-center gap-2">
            <Sun className="size-3.5 text-muted-foreground" />
            Light
          </span>
        }
        description="For daylight and for screenshots that end up in a ticket."
      >
        <ThemeGrid themes={light} active={themeId} onPick={setTheme} />
      </Section>

      <Panel>
        <PanelHeader icon={Monitor} title="Where this is stored" />
        <PanelBody>
          <p className="max-w-3xl text-xs leading-relaxed text-muted-foreground">
            The choice lives in this browser&apos;s local storage, not on your account — the same
            server can look one way on your laptop and another on your phone. It survives reloads,
            sign-outs and dashboard restarts, and it is applied before the page paints, so there is
            no flash of the previous palette. The palette is also reachable from the top bar and
            from the command palette (⌘K), so you can try one without leaving the page you are
            reading.
          </p>
        </PanelBody>
      </Panel>
    </Page>
  )
}

function ThemeGrid({
  themes,
  active,
  onPick,
}: {
  themes: Theme[]
  active: string
  onPick: (id: string) => void
}) {
  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 [&>*]:min-w-0">
      {themes.map((theme) => (
        <ThemeOption
          key={theme.id}
          theme={theme}
          active={theme.id === active}
          onPick={() => onPick(theme.id)}
        />
      ))}
    </div>
  )
}

function ThemeOption({
  theme,
  active,
  onPick,
}: {
  theme: Theme
  active: boolean
  onPick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onPick}
      aria-pressed={active}
      className={cn(
        "group min-w-0 rounded-xl border bg-card p-2 text-left transition-all",
        "focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
        active
          ? "border-primary ring-[3px] ring-primary/25"
          : "hover:border-primary/40 hover:bg-accent/40",
      )}
    >
      <ThemePreview theme={theme} />
      <div className="min-w-0 px-1.5 pt-2.5 pb-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-[13px] font-medium">{theme.name}</span>
          {active && <Check className="size-3.5 shrink-0 text-primary" />}
        </div>
        <p className="text-xs text-muted-foreground">{theme.description}</p>
      </div>
    </button>
  )
}

/**
 * A miniature of the dashboard drawn in the theme it is offering.
 *
 * The `data-theme` attribute is all it takes: the palettes are defined against
 * a bare attribute selector, so the tokens inside this element resolve to that
 * theme's values while the page around it stays as it is. Nothing here uses a
 * `dark:` variant, which is what keeps a light preview readable inside a dark
 * page.
 */
function ThemePreview({ theme }: { theme: Theme }) {
  return (
    <div
      data-theme={theme.id}
      aria-hidden
      className="flex h-32 overflow-hidden rounded-lg border bg-background"
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
