import { cn } from "@/lib/utils"

/**
 * The one content block in the dashboard.
 *
 * Every page is a stack of these: a framed surface with an optional header
 * strip, an optional toolbar under it, and a body. Before this existed each
 * page assembled its own out of Card/CardHeader/CardContent with whatever
 * padding and title size that page's author picked, which is why fourteen
 * pages read as fourteen products. A panel has exactly one look, and the props
 * decide what it contains rather than how it is drawn.
 *
 * The header is a strip with its own tint and a hairline under it, not a block
 * of padding sharing the body's ground — that is what makes a panel legible as
 * "chrome, then content" at a glance, and what lets a toolbar or a full-bleed
 * table sit flush beneath it without inventing a second edge.
 */
export function Panel({ className, children, ...props }: React.ComponentProps<"section">) {
  return (
    <section
      data-slot="panel"
      className={cn(
        "flex min-w-0 flex-col overflow-hidden rounded-xl border bg-card text-card-foreground",
        className,
      )}
      {...props}
    >
      {children}
    </section>
  )
}

export function PanelHeader({
  title,
  description,
  eyebrow,
  icon: Icon,
  actions,
  className,
  children,
}: {
  title?: React.ReactNode
  description?: React.ReactNode
  eyebrow?: React.ReactNode
  icon?: React.ComponentType<{ className?: string }>
  actions?: React.ReactNode
  className?: string
  children?: React.ReactNode
}) {
  return (
    <header
      data-slot="panel-header"
      className={cn(
        "flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b border-hairline bg-surface-header px-4 py-2.5",
        className,
      )}
    >
      <div className="flex min-w-0 items-center gap-2.5">
        {Icon && (
          <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary/12 text-primary">
            <Icon className="size-3.5" />
          </span>
        )}
        <div className="min-w-0">
          {eyebrow && <p className="eyebrow mb-0.5">{eyebrow}</p>}
          {title && <h2 className="truncate text-[13px] leading-tight font-medium">{title}</h2>}
          {description && (
            <p className="truncate text-xs leading-tight text-muted-foreground">{description}</p>
          )}
        </div>
      </div>
      {children}
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-1.5">{actions}</div>}
    </header>
  )
}

/**
 * A strip of filters directly under a panel header.
 *
 * Kept on the header's ground rather than the body's, so search inputs and
 * segmented controls read as part of the frame and the body below is only the
 * data. Also the reason a filter row never scrolls away with the rows it
 * filters.
 */
export function PanelToolbar({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="panel-toolbar"
      className={cn(
        "flex flex-wrap items-center gap-2 border-b border-hairline bg-surface-header/60 px-3 py-2",
        className,
      )}
      {...props}
    />
  )
}

/**
 * `flush` drops the padding for a body that is one full-bleed table or list,
 * and is also what the alignment rule in globals.css keys off so those tables
 * still line their outer columns up with the header's text.
 */
export function PanelBody({
  className,
  flush,
  scroll,
  ...props
}: React.ComponentProps<"div"> & { flush?: boolean; scroll?: boolean }) {
  return (
    <div
      data-slot="panel-body"
      data-flush={flush ? "" : undefined}
      className={cn(
        "min-w-0",
        flush ? "" : "p-4",
        scroll && "min-h-0 flex-1 overflow-auto",
        className,
      )}
      {...props}
    />
  )
}

export function PanelFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="panel-footer"
      className={cn(
        // mt-auto so a row of panels stretched to a common height still ends
        // with their footers on the same line rather than mid-card.
        "mt-auto flex flex-wrap items-center gap-2 border-t border-hairline bg-surface-header/60 px-4 py-2.5",
        className,
      )}
      {...props}
    />
  )
}

/**
 * A recessed well: command output, a log tail, a diff, a stored secret.
 *
 * Its ground is mixed towards the page background rather than being a flat
 * black, which is the only version of this that survives a light palette.
 */
export function Well({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "overflow-auto rounded-lg border border-hairline bg-surface-sunken p-3 font-mono text-xs leading-relaxed",
        className,
      )}
      {...props}
    />
  )
}
