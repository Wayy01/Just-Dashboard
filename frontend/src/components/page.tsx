import { cn } from "@/lib/utils"
import { Input } from "@/components/ui/input"
import { Search } from "lucide-react"

/**
 * The frame every page renders into.
 *
 * One measure, one gutter, one vertical rhythm, set here rather than by each
 * page picking its own `gap-4`/`gap-6`/`space-y-4`. `min-w-0` is load-bearing
 * on both the frame and its children: a wide table's intrinsic width would
 * otherwise widen the flex column and take the whole shell sideways with it,
 * instead of scrolling inside the panel that owns it.
 *
 * `fill` is for the pages whose content *is* the viewport — the terminal and
 * the log stream — where the pane has to take the remaining height rather than
 * the page growing past the bottom of the window.
 *
 * It has to be a *definite* height, not `min-h-full`, and that distinction is
 * the whole bug it exists to prevent. A minimum is a floor the box may exceed,
 * so the page stayed content-sized: every `min-h-0 flex-1` beneath it then
 * measured against a parent that grows to fit, which is the opposite of what
 * those classes are asking for. A log pane sized itself to eight thousand
 * lines, and a terminal ratcheted — xterm's fit addon reads the box, the box
 * came from xterm's own rows, so the pane could grow but never shrink back.
 * `h-full` resolves against the shell's scroll container, which does have a
 * definite height, and `overflow-hidden` keeps that promise: a fill page is
 * exactly the space it was handed, and the scrolling happens inside the pane
 * that owns the content.
 */
export function Page({
  className,
  fill,
  ...props
}: React.ComponentProps<"div"> & { fill?: boolean }) {
  return (
    <div
      data-slot="page"
      className={cn(
        "mx-auto flex w-full min-w-0 max-w-[1600px] flex-col gap-4 px-4 py-4 md:gap-5 md:px-6 md:py-5",
        fill && "h-full min-h-0 overflow-hidden",
        className,
      )}
      {...props}
    />
  )
}

/**
 * The title band at the top of a page: where it sits in the product, what it
 * is called, what it is, and what you can do to it.
 *
 * The eyebrow repeats the nav group rather than the page name, so the band
 * answers "where am I" without restating the sidebar item directly above it.
 */
export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
  className,
}: {
  eyebrow?: React.ReactNode
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  className?: string
}) {
  return (
    <div
      data-slot="page-header"
      className={cn("flex min-w-0 flex-wrap items-end justify-between gap-x-6 gap-y-3", className)}
    >
      <div className="min-w-0 space-y-1">
        {eyebrow && <p className="eyebrow">{eyebrow}</p>}
        <h1 className="truncate text-xl leading-tight font-semibold">{title}</h1>
        {description && (
          <div className="text-[13px] leading-snug text-muted-foreground">{description}</div>
        )}
      </div>
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
    </div>
  )
}

/**
 * A labelled group of panels inside a page, for the pages that hold more than
 * one idea (appearance, account, certificates).
 */
export function Section({
  title,
  description,
  actions,
  className,
  children,
}: {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  className?: string
  children: React.ReactNode
}) {
  return (
    <section className={cn("flex min-w-0 flex-col gap-3", className)}>
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <div className="min-w-0">
          <h2 className="text-[13px] font-medium">{title}</h2>
          {description && <p className="text-xs text-muted-foreground">{description}</p>}
        </div>
        {actions}
      </div>
      {children}
    </section>
  )
}

/** A row of filters and actions that stands on its own, outside a panel. */
export function Toolbar({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={cn("flex min-w-0 flex-wrap items-center gap-2", className)} {...props} />
}

/**
 * The filter box, which appeared in six pages as the same three elements
 * assembled slightly differently each time — a different width, a different
 * icon offset, sometimes no icon at all.
 */
export function SearchInput({
  className,
  containerClassName,
  ...props
}: React.ComponentProps<typeof Input> & { containerClassName?: string }) {
  return (
    <div className={cn("relative w-full sm:w-72", containerClassName)}>
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
      <Input className={cn("h-8 pl-8 text-[13px]", className)} {...props} />
    </div>
  )
}

/**
 * One figure with its name, for a strip of them under a chart or in a card
 * header. The name goes above the number, small and quiet: a column of these
 * scans as a table of values rather than a paragraph of labels.
 */
export function Metric({
  label,
  value,
  hint,
  className,
}: {
  label: React.ReactNode
  value: React.ReactNode
  hint?: React.ReactNode
  className?: string
}) {
  return (
    <div className={cn("min-w-0", className)}>
      <p className="eyebrow truncate">{label}</p>
      <p className="numeric mt-0.5 truncate text-sm font-medium">{value}</p>
      {hint && <p className="truncate text-[11px] text-muted-foreground">{hint}</p>}
    </div>
  )
}

/** A horizontal run of Metrics, separated by rules rather than by gap alone. */
export function MetricStrip({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "flex flex-wrap gap-x-6 gap-y-3 [&>*]:min-w-0 [&>*+*]:border-l [&>*+*]:border-hairline [&>*+*]:pl-6",
        className,
      )}
      {...props}
    />
  )
}

/**
 * A table cell's primary column, rendered as the row's way into its detail
 * view — default text colour, underline only on hover.
 *
 * `Button`'s `variant="link"` doesn't fit here: it colours the text primary
 * and always underlines, which reads as navigation rather than "this is the
 * name of the thing the row is about". Thirteen tables across the app were
 * each retyping the same three classes for exactly this button before this
 * existed.
 */
export function RowLink({
  onClick,
  mono,
  className,
  children,
}: {
  onClick: () => void
  mono?: boolean
  className?: string
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "truncate text-left text-[13px] font-medium hover:underline",
        mono && "font-mono text-xs",
        className,
      )}
    >
      {children}
    </button>
  )
}

/**
 * A label/value pair in a stacked list — the replacement for the `<dl>` grids
 * that each card used to hand-roll with its own column widths.
 */
export function DetailList({ className, ...props }: React.ComponentProps<"dl">) {
  return (
    <dl
      className={cn("grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-4 gap-y-1.5", className)}
      {...props}
    />
  )
}

export function Detail({
  label,
  children,
  className,
}: {
  label: React.ReactNode
  children: React.ReactNode
  className?: string
}) {
  return (
    <>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn("min-w-0 text-xs", className)}>{children}</dd>
    </>
  )
}
