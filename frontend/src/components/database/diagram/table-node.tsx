"use client"

import { memo } from "react"
import { Handle, Position, type NodeProps } from "@xyflow/react"
import { Eye, Fingerprint, KeyRound, Link2, Table2 } from "lucide-react"
import { cn } from "@/lib/utils"
import type { DbGraphColumn, DbGraphTable } from "@/lib/types"

export const ROW_HEIGHT = 26
export const HEADER_HEIGHT = 40
export const NODE_WIDTH = 268

export type TableNodeData = {
  table: DbGraphTable
  /** Dimmed when something else is focused and this is not related to it. */
  dimmed: boolean
  focused: boolean
  /** Hides everything but keys, for reading a large schema's shape. */
  keysOnly: boolean
  onOpen: (schema: string, table: string) => void
}

/**
 * One table, drawn as the list of columns it is.
 *
 * The point of a schema diagram is the columns — which one is the key, which
 * one points somewhere else — so boxes with only names on them answer a
 * question nobody was asking. Every column is a row here, and every row is an
 * anchor: an edge lands on `orders.customer_id`, not on `orders`.
 *
 * Colour comes from the palette, so the diagram re-themes with everything else.
 * The key colours are the one deliberate constant, because they carry meaning
 * rather than decoration.
 */
function TableNodeComponent({ data, selected }: NodeProps & { data: TableNodeData }) {
  const { table, dimmed, focused, keysOnly, onOpen } = data
  const columns = keysOnly
    ? table.columns.filter((c) => c.primaryKey || c.foreignKey || c.unique)
    : table.columns
  const hidden = table.columns.length - columns.length

  return (
    <div
      className={cn(
        "overflow-hidden rounded-lg border bg-card shadow-sm transition-opacity duration-200",
        dimmed && "opacity-20",
        focused && "ring-2 ring-primary",
        selected && !focused && "ring-1 ring-primary/50",
      )}
      style={{ width: NODE_WIDTH }}
    >
      <button
        onClick={() => onOpen(table.schema, table.name)}
        className="flex w-full items-center gap-2 border-b bg-primary/10 px-3 text-left transition-colors hover:bg-primary/20"
        style={{ height: HEADER_HEIGHT }}
        title={`Open ${table.name}`}
      >
        <Table2 className="size-3.5 shrink-0 text-primary" />
        <span className="min-w-0 flex-1 truncate font-mono text-[12.5px] font-medium">
          {table.name}
        </span>
        {table.rows > 0 && (
          <span className="shrink-0 text-[10px] tabular-nums text-muted-foreground">
            {table.rows.toLocaleString()}
          </span>
        )}
      </button>

      <div>
        {columns.map((c) => (
          <ColumnRow key={c.name} table={table.name} column={c} />
        ))}
        {hidden > 0 && (
          <div
            className="flex items-center gap-1.5 px-3 text-[10px] text-muted-foreground"
            style={{ height: ROW_HEIGHT }}
          >
            <Eye className="size-3" />
            {hidden} more {hidden === 1 ? "column" : "columns"}
          </div>
        )}
        {columns.length === 0 && (
          <div
            className="px-3 text-[10px] italic text-muted-foreground"
            style={{ height: ROW_HEIGHT, lineHeight: `${ROW_HEIGHT}px` }}
          >
            no columns readable
          </div>
        )}
      </div>
    </div>
  )
}

function ColumnRow({ table, column }: { table: string; column: DbGraphColumn }) {
  // Both sides carry a handle for every column, because which side an edge
  // leaves by depends on where the layout put the other table — and an edge
  // referring to a handle that does not exist is dropped silently rather than
  // drawn badly.
  const handleStyle = { opacity: 0, width: 1, height: 1, border: 0, minWidth: 0, minHeight: 0 }
  return (
    <div
      className="relative flex items-center gap-1.5 px-3 hover:bg-accent/60"
      style={{ height: ROW_HEIGHT }}
      title={`${column.name} · ${column.type}${column.nullable ? " · nullable" : " · not null"}`}
    >
      {/* Four per column: a source and a target on each side. An edge needs a
          source at one end and a target at the other, and which side each uses
          depends on where the layout put the two tables — so all four have to
          exist. A single type, or a missing side, drops the edge silently
          rather than drawing it wrongly, which is exactly how this was found. */}
      <Handle
        type="source"
        position={Position.Left}
        id={`${table}.${column.name}.left.s`}
        style={{ ...handleStyle, left: 0 }}
        isConnectable={false}
      />
      <Handle
        type="target"
        position={Position.Left}
        id={`${table}.${column.name}.left.t`}
        style={{ ...handleStyle, left: 0 }}
        isConnectable={false}
      />
      <Handle
        type="source"
        position={Position.Right}
        id={`${table}.${column.name}.right.s`}
        style={{ ...handleStyle, right: 0 }}
        isConnectable={false}
      />
      <Handle
        type="target"
        position={Position.Right}
        id={`${table}.${column.name}.right.t`}
        style={{ ...handleStyle, right: 0 }}
        isConnectable={false}
      />
      {column.primaryKey ? (
        <KeyRound className="size-3 shrink-0 text-amber-500" />
      ) : column.foreignKey ? (
        <Link2 className="size-3 shrink-0 text-sky-500" />
      ) : column.unique ? (
        <Fingerprint className="size-3 shrink-0 text-muted-foreground/70" />
      ) : (
        <span className="size-3 shrink-0" />
      )}
      <span
        className={cn(
          "min-w-0 flex-1 truncate font-mono text-[11px]",
          column.primaryKey && "font-medium",
        )}
      >
        {column.name}
      </span>
      <span className="shrink-0 truncate font-mono text-[9.5px] text-muted-foreground">
        {shortType(column.type)}
        {!column.nullable && <span className="ml-0.5 text-destructive/70">*</span>}
      </span>
    </div>
  )
}

/**
 * Type names are for recognition here, not reproduction. "timestamp with time
 * zone" in a nine-pixel column pushes the name out of the box, and a reader
 * already knows what timestamptz means.
 */
function shortType(type: string): string {
  const map: Record<string, string> = {
    "timestamp with time zone": "timestamptz",
    "timestamp without time zone": "timestamp",
    "character varying": "varchar",
    "double precision": "float8",
    bigint: "int8",
    integer: "int4",
    smallint: "int2",
    boolean: "bool",
    character: "char",
  }
  const mapped = map[type.toLowerCase()] ?? type
  return mapped.length > 14 ? mapped.slice(0, 13) + "…" : mapped
}

export const TableNode = memo(TableNodeComponent)
