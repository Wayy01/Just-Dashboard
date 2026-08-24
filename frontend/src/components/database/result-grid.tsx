"use client"

import { useState } from "react"
import { ArrowDown, ArrowUp, ChevronsUpDown, Copy, ExternalLink, Pencil, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import type { DbForeignKey, QueryResult } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

/**
 * The one result grid every database view renders through, so a query result, a
 * table browse and an edited row all read the same. It keeps the read-only shape
 * from the original page and adds the two things a browser needs to become an
 * editor: per-row actions (when the caller can identify a row by primary key)
 * and a click-to-expand cell, because a JSON blob or a paragraph of text
 * truncated to a table cell is the one value you most need to see in full.
 */
export function ResultGrid({
  result,
  onEdit,
  onDelete,
  sort,
  onSort,
  foreignKeys,
  onFollow,
  selection,
  onSelectionChange,
  className,
  maxHeightClass = "max-h-[calc(100svh-22rem)]",
}: {
  result: QueryResult
  onEdit?: (row: Record<string, unknown>) => void
  onDelete?: (row: Record<string, unknown>) => void
  /** The column the server is ordering by, when the caller supports sorting. */
  sort?: { column: string; desc: boolean } | null
  onSort?: (column: string) => void
  /** Outgoing foreign keys, so a referencing value becomes a link. */
  foreignKeys?: DbForeignKey[]
  onFollow?: (fk: DbForeignKey, value: unknown) => void
  /** Row indices selected for a bulk action, when the caller supports one. */
  selection?: Set<number>
  onSelectionChange?: (next: Set<number>) => void
  className?: string
  maxHeightClass?: string
}) {
  const [detail, setDetail] = useState<{ column: string; value: unknown } | null>(null)
  const hasActions = Boolean(onEdit || onDelete)
  const selectable = Boolean(selection && onSelectionChange)

  // Single-column foreign keys become links. A composite key is deliberately
  // left alone: following one means matching several columns at once, and a
  // link on just the first of them would go somewhere wrong.
  const fkByColumn = new Map<string, DbForeignKey>()
  for (const fk of foreignKeys ?? []) {
    if (fk.columns.length === 1) fkByColumn.set(fk.columns[0], fk)
  }

  const allSelected = selectable && selection!.size > 0 && selection!.size === result.rows.length
  const toggleAll = () => {
    if (!onSelectionChange) return
    onSelectionChange(allSelected ? new Set() : new Set(result.rows.map((_, i) => i)))
  }
  const toggleRow = (i: number) => {
    if (!selection || !onSelectionChange) return
    const next = new Set(selection)
    if (next.has(i)) {
      next.delete(i)
    } else {
      next.add(i)
    }
    onSelectionChange(next)
  }

  if (result.columns.length === 0) {
    return (
      <p className="p-4 text-[13px] text-muted-foreground">
        {result.rowsAffected} row(s) affected in {result.duration}.
      </p>
    )
  }

  const rowRecord = (row: unknown[]): Record<string, unknown> => {
    const rec: Record<string, unknown> = {}
    result.columns.forEach((c, i) => {
      rec[c] = row[i]
    })
    return rec
  }

  return (
    <>
      <Table containerClassName={cn(maxHeightClass, className)}>
        <TableHeader className={stickyTableHeader}>
          <TableRow>
            {selectable && (
              <TableHead className="w-9">
                <Checkbox
                  checked={allSelected}
                  onCheckedChange={toggleAll}
                  aria-label="Select every row on this page"
                />
              </TableHead>
            )}
            {hasActions && <TableHead className="w-[5.5rem]" />}
            {result.columns.map((col, i) => (
              <TableHead key={col} className="whitespace-nowrap">
                {onSort ? (
                  <button
                    onClick={() => onSort(col)}
                    className="group/sort inline-flex items-center gap-1 hover:text-foreground"
                    title={`Sort by ${col}`}
                  >
                    {col}
                    <SortIcon active={sort?.column === col} desc={sort?.desc ?? false} />
                  </button>
                ) : (
                  col
                )}
                {result.types[i] && (
                  <span className="ml-1 text-[10px] font-normal normal-case text-muted-foreground/70">
                    {result.types[i].toLowerCase()}
                  </span>
                )}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {result.rows.map((row, i) => (
            <TableRow key={i} className="group" data-selected={selection?.has(i) || undefined}>
              {selectable && (
                <TableCell className="w-9">
                  <Checkbox
                    checked={selection!.has(i)}
                    onCheckedChange={() => toggleRow(i)}
                    aria-label={`Select row ${i + 1}`}
                  />
                </TableCell>
              )}
              {hasActions && (
                <TableCell className="w-[5.5rem]">
                  <div className="flex items-center gap-0.5 opacity-40 transition-opacity group-hover:opacity-100">
                    {onEdit && (
                      <Button
                        size="icon"
                        variant="ghost"
                        className="size-6"
                        title="Edit row"
                        onClick={() => onEdit(rowRecord(row))}
                      >
                        <Pencil className="size-3.5" />
                      </Button>
                    )}
                    {onDelete && (
                      <Button
                        size="icon"
                        variant="ghost"
                        className="size-6 text-destructive"
                        title="Delete row"
                        onClick={() => onDelete(rowRecord(row))}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    )}
                  </div>
                </TableCell>
              )}
              {row.map((cell, j) => {
                const fk = fkByColumn.get(result.columns[j])
                const followable = fk && onFollow && cell !== null && cell !== undefined
                return (
                  <TableCell
                    key={j}
                    onClick={() => setDetail({ column: result.columns[j], value: cell })}
                    className="max-w-xs cursor-pointer truncate font-mono text-xs hover:bg-accent/50"
                    title="Click to view full value"
                  >
                    <span className="inline-flex min-w-0 items-center gap-1">
                      <CellValue value={cell} />
                      {followable && (
                        <button
                          onClick={(e) => {
                            // The cell itself opens the value viewer; following
                            // the reference is a different intent and must not
                            // trigger both.
                            e.stopPropagation()
                            onFollow(fk, cell)
                          }}
                          title={`Open ${fk.refTable} where ${fk.refColumns[0]} = ${String(cell)}`}
                          className="shrink-0 text-muted-foreground/60 hover:text-primary"
                        >
                          <ExternalLink className="size-3" />
                        </button>
                      )}
                    </span>
                  </TableCell>
                )
              })}
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <Dialog open={detail !== null} onOpenChange={(o) => !o && setDetail(null)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm">{detail?.column}</DialogTitle>
          </DialogHeader>
          {detail && <CellDetail value={detail.value} />}
        </DialogContent>
      </Dialog>
    </>
  )
}

function SortIcon({ active, desc }: { active: boolean; desc: boolean }) {
  if (!active)
    return (
      <ChevronsUpDown className="size-3 opacity-0 transition-opacity group-hover/sort:opacity-50" />
    )
  return desc ? <ArrowDown className="size-3 text-primary" /> : <ArrowUp className="size-3 text-primary" />
}

function CellValue({ value }: { value: unknown }) {
  if (value === null || value === undefined)
    return <span className="text-muted-foreground italic">null</span>
  if (typeof value === "object") return <>{JSON.stringify(value)}</>
  if (typeof value === "boolean")
    return <span className="text-primary">{String(value)}</span>
  return <>{String(value)}</>
}

function CellDetail({ value }: { value: unknown }) {
  const text =
    value === null || value === undefined
      ? ""
      : typeof value === "object"
        ? JSON.stringify(value, null, 2)
        : String(value)
  const isNull = value === null || value === undefined
  return (
    <div className="space-y-2">
      <div className="max-h-[60vh] overflow-auto rounded-md border border-hairline bg-muted/40 p-3">
        {isNull ? (
          <span className="text-sm text-muted-foreground italic">null</span>
        ) : (
          <pre className="font-mono text-xs whitespace-pre-wrap break-words">{text}</pre>
        )}
      </div>
      {!isNull && (
        <Button
          size="sm"
          variant="outline"
          onClick={() =>
            navigator.clipboard
              .writeText(text)
              .then(() => toast.success("Copied to clipboard"))
              .catch(() => toast.error("Could not copy"))
          }
        >
          <Copy className="size-3.5" />
          Copy value
        </Button>
      )}
    </div>
  )
}
