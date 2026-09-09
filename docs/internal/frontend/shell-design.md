# Frontend shell and design system

The App Router currently has 48 `page.tsx` entry points, including nested database, Docker, proxy,
security, and deployment workflows plus `/login`. Most page modules are client components; the three
deployment detail/new wrappers remain server components and hand interaction to client components under
`components/deploy/`.

## The shell

`(dashboard)/layout.tsx` owns `CommandPaletteProvider`, `SelfUpdateProvider`, `SidebarProvider` +
`AppSidebar`, `TopBar`, and `MetricsStream` — which renders nothing and exists to hold the metrics socket
open for the whole shell, so Overview's charts and the top bar's vitals keep filling from other pages. Its
redirect to `/login` is convenience, not a control; every API call behind it is authenticated server-side.

**The scroll container is on the `SidebarInset`, not the document.** That is what pins the top bar and
lets a page ask for the remaining height (`<Page fill>`) instead of growing past the viewport.

`components/app-sidebar.tsx` exports the nav registry (`NAV`, `PERSONAL_NAV`) so `command-palette.tsx`
offers the same destinations without a second list. Items may carry a `capability`, and the sidebar hides
what the role cannot use. ⌘K covers every page plus the theme toggle.

## The design system

Two files define the visual language, and pages compose them rather than hand-rolling layout:

- `components/page.tsx` — `Page` (one measure, gutter and rhythm; `fill` for terminal and logs, whose
  content *is* the viewport), `PageHeader`, `Section`, `Toolbar`, `SearchInput`, `Metric`/`MetricStrip`,
  `DetailList`/`Detail`.
- `components/panel.tsx` — `Panel`, `PanelHeader`, `PanelToolbar`, `PanelBody`, `PanelFooter`, `Well`. A
  panel is *the* content block: a framed surface with a tinted header strip and a hairline, so it reads as
  "chrome, then content" and a toolbar or full-bleed table sits flush beneath without a second edge.

**Reach for `Panel`/`Page`, not raw `Card`**, and add a variant there rather than a one-off in a feature
page — before these existed, fourteen pages read as fourteen products. `components/state.tsx` does the
same for the non-happy paths (`Spinner`, `LoadingRows`, `LoadingPanel`, `EmptyState`, `ErrorState`,
`Notice`). `min-w-0` on the frame and its children is load-bearing: a wide table's intrinsic width would
otherwise widen the flex column and take the whole shell sideways instead of scrolling inside its panel.

**Anything meant to sit in front of what is behind it stands up off the page.** `raised` (a `@utility` in
`globals.css`) draws three lines from the `--raise-*` tokens — a light hairline on top, a dark lip below,
a shadow under — inverting on `:active`. The tokens are translucent black and white rather than colours,
because the same three lines sit on a white primary, a red destructive and a near-black outline alike;
`--control`/`--control-hover` are the shared resting face. Hover goes *darker* and is named per mode
rather than computed, since mixing in more `--foreground` lights the button up on dark mode. Light mode
is not the dark values scaled — the gloss sits on the variant's colour, not on the page. A surface
wanting a deeper shadow overrides the token (`[--raise-drop:var(--shadow-lg)]`) rather than adding a
`shadow-*` utility, which would win the cascade and delete the shine and the lip.

The same class covers cards: a card is a very large button nobody presses, and the earlier split (a
gradient `.card-sheen` on panels, three lines on buttons) made one claim in two visual languages.

Two deliberate exceptions:

- **The nav stays flat.** The lift works by making one thing stand out from what is behind it, which stops
  meaning anything when forty-nine rows claim it at once; a nav is a *list*. The current destination uses
  the sidebar accent fill and medium label weight at both navigation depths, keeping location visible
  without an inverted primary pill competing with the page. What survived the experiment is the spacing:
  `SidebarMenu`/`SidebarMenuSub` at `gap-2`.
- **Ghost and link buttons stay flat**, and so do inputs and textareas. Ghost is 142 of ~400 buttons — the
  quiet action at the end of a table row — and giving it a face turns every row into a strip of controls
  competing with its own data. A page where fields and buttons are equally raised has no hierarchy left.
  Where a control is a *toggle*, the **unpressed** state is the raised one; pressing puts it down.

`components/logo.tsx` is the wordmark and nothing else — "Just" in `text-primary`, "Dashboard" in the text
colour, the version as small muted text beside it. No mark, no tile, no strapline. It is the only
rendering of the product's name, so sidebar, sign-in and splash agree and a rename is one file. `LogoMark`
is the single letter the collapsed rail falls back to.

`components/ui/*` is generated shadcn/ui (new-york, zinc, 35 primitives) with its icons rewired to
the Heroicons vocabulary in `components/icons.tsx` — compose rather than
edit. Feature pieces live in `components/<feature>/`: `database/`, `docker/`, `files/`, `git/`, `logs/`,
`metrics/`, `packages/`, `procs/`, `proxy/`, `security/`, `terminal/`, `update/`.

## The editor is served from here, not from a CDN

`components/code-editor.tsx` wraps Monaco, and the line that matters is
`loader.config({ paths: { vs: "/monaco/vs" } })`. `@monaco-editor/react` otherwise fetches the editor from
`cdn.jsdelivr.net` at runtime — third-party JavaScript in the same origin as a session that drives the
Docker socket and a root shell, and a permanent spinner for an operator whose workstation has no egress,
which is the workstation this is meant to be reached from. `scripts/sync-monaco.mjs` copies
`monaco-editor/min/vs` into `public/monaco/vs` from `predev`/`prebuild` **and** explicitly in the
Dockerfile, because the image invokes next's entrypoint directly and never sees the npm hooks. The copy is
gitignored and excluded from eslint.

## Charts

`components/metrics/` is a third design-system file in all but name: every chart goes through it rather
than assembling its own recharts tree, which is how the old Overview page ended up with three tooltip
formats and no way to compare a moment across four charts. Adding a measurement should mean naming a
series.

- `metric-chart.tsx` — `MetricChart` + the `Series` descriptor. The x-axis is **numeric over time**, not a
  category axis of pre-formatted labels: a category axis spaces every bucket equally, which lies whenever
  the record has a hole in it. It owns the shared crosshair, drag-to-zoom, event markers, thresholds and
  the one tooltip listing every series at the hovered instant.
- `chart-panel.tsx` — the header/chart/legend shape and the empty state. A series with no numbers anywhere
  in the window is **dropped rather than drawn flat at zero**, which is what makes a kernel without PSI
  say so instead of reporting three healthy zeroes.
- `series-legend.tsx` — min/mean/max/last, plus **"At cursor"**: while the pointer is over any chart, every
  legend shows the value its series held at that instant. Max reads the peak column where a series has
  one, since the maximum of the *means* is exactly what a downsampled window hides.
- `sparkline.tsx` — a bare SVG path for a table cell, not recharts: forty containers would otherwise mount
  forty responsive containers and resize observers.
- `range-picker.tsx`, `health-panel.tsx` — the window control (pan and zoom-out appear only once a window
  has been dragged) and the verdict.

`lib/metrics-crosshair.ts` holds the hovered instant **outside React**, for the reason the live buffer is:
a pointer crossing a chart fires continuously, and a context above the router would re-render the terminal
and the log tail on every mousemove. The value is a **timestamp**, not a row index — charts on a page do
not share a row array.
