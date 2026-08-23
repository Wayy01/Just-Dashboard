"use client"

import { useMemo, useState } from "react"
import { Database, Download, FileJson, Plus, Table2 } from "lucide-react"
import { toast } from "sonner"
import { del, downloadUrl, get, patch, post } from "@/lib/api"
import { bytes } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { DbConnection, DbTable, DbTableDetail, QueryResult } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import type { useConfirm } from "@/components/confirm-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { ResultGrid } from "@/components/database/result-grid"
import { RowEditor } from "@/components/database/row-editor"

type ConfirmFn = ReturnType<typeof useConfirm>["confirm"]
export type TableSelection = { schema: string; table: string }
const PAGE = 100

/**
 * The Browse tab: a schema/table rail beside a data grid that can now insert,
 * edit and delete rows and export the table, not only page through it.
 *
 * Tables are listed across every schema in one request, each carrying its real
 * schema, and browsing keys off that — a plainer and more correct model than
 * feeding a database name in where a namespace was expected. The schema filter
 * is built from the schemas actually present, so it says something true on every
 * engine. Editing is offered only when a table has a primary key and the role
 * permits it: without a key the server cannot scope a change to one row.
 */
export function BrowseTab({
  conn,
  confirm,
  selection,
  onSelect,
}: {
  conn: DbConnection
  confirm: ConfirmFn
  selection: TableSelection | null
  onSelect: (sel: TableSelection) => void
}) {
  const { can } = useAuth()
  const [textFilter, setTextFilter] = useState("")
  const [schemaFilter, setSchemaFilter] = useState("all")
  const [offset, setOffset] = useState(0)
  const [editor, setEditor] = useState<{ mode: "insert" | "edit"; initial?: Record<string, unknown> } | null>(
    null,
  )

  const tables = usePoll(
    (signal) => get<DbTable[]>(`/databases/${conn.id}/tables`, { schema: "" }, signal),
    0,
    [conn.id],
  )
  const detail = usePoll(
    (signal) =>
      selection && conn.driver !== "mongodb"
        ? get<DbTableDetail>(
            `/databases/${conn.id}/table`,
            { schema: selection.schema, table: selection.table },
            signal,
          )
        : Promise.resolve(null as unknown as DbTableDetail),
    0,
    [conn.id, selection?.schema, selection?.table],
  )
  const rows = usePoll(
    (signal) =>
      selection
        ? get<QueryResult>(
            `/databases/${conn.id}/browse`,
            { schema: selection.schema, table: selection.table, limit: PAGE, offset },
            signal,
          )
        : Promise.resolve(null as unknown as QueryResult),
    0,
    [conn.id, selection?.schema, selection?.table, offset],
  )

  const schemaNames = useMemo(() => {
    const set = new Set<string>()
    for (const t of tables.data ?? []) set.add(t.schema)
    return [...set].sort()
  }, [tables.data])

  const visibleTables = useMemo(() => {
    let list = tables.data ?? []
    if (schemaFilter !== "all") list = list.filter((t) => t.schema === schemaFilter)
    const q = textFilter.trim().toLowerCase()
    if (q) list = list.filter((t) => t.name.toLowerCase().includes(q))
    return list
  }, [tables.data, schemaFilter, textFilter])

  const select = (t: DbTable) => {
    setOffset(0)
    onSelect({ schema: t.schema, table: t.name })
  }

  const pk = detail.data?.primaryKey ?? []
  const canWrite = can("service.control") && conn.driver !== "mongodb"
  const canEditRows = canWrite && pk.length > 0 && detail.data !== null
  const table = selection?.table

  const reload = () => {
    rows.refresh()
    tables.refresh()
  }
  const insertRow = async (values: Record<string, unknown>) => {
    await post(`/databases/${conn.id}/rows`, { schema: selection?.schema, table, values })
    toast.success("Row inserted")
    reload()
  }
  const updateRow = async (values: Record<string, unknown>, key?: Record<string, unknown>) => {
    await patch(`/databases/${conn.id}/rows`, { schema: selection?.schema, table, values, key })
    toast.success("Row updated")
    reload()
  }
  const deleteRow = (row: Record<string, unknown>) => {
    const key: Record<string, unknown> = {}
    for (const c of pk) key[c] = row[c]
    confirm({
      title: "Delete row",
      phrase: table,
      confirmLabel: "Delete row",
      description: (
        <p className="text-sm">
          Permanently deletes the row where{" "}
          <span className="font-mono text-xs">
            {pk.map((c) => `${c}=${String(row[c])}`).join(", ")}
          </span>{" "}
          from <b>{table}</b>. This cannot be undone.
        </p>
      ),
      action: async (c) => {
        await del(`/databases/${conn.id}/rows`, {
          body: { schema: selection?.schema, table, key },
          confirm: c,
        })
        toast.success("Row deleted")
        reload()
      },
    })
  }

  const exportTable = (format: "csv" | "json") => {
    if (!selection) return
    const a = document.createElement("a")
    a.href = downloadUrl(`/databases/${conn.id}/export`, {
      schema: selection.schema,
      table: selection.table,
      format,
    })
    a.click()
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[16rem_minmax(0,1fr)] [&>*]:min-w-0">
      <Panel>
        <PanelHeader
          icon={Database}
          title="Tables"
          description={`${visibleTables.length} of ${tables.data?.length ?? 0}`}
        />
        <PanelBody className="space-y-3">
          {schemaNames.length > 1 && (
            <Select value={schemaFilter} onValueChange={setSchemaFilter}>
              <SelectTrigger size="sm" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All schemas</SelectItem>
                {schemaNames.map((s) => (
                  <SelectItem key={s} value={s}>
                    {s}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <Input
            placeholder="Filter tables…"
            value={textFilter}
            onChange={(e) => setTextFilter(e.target.value)}
            className="h-8 text-xs"
          />
          <div className="max-h-[calc(100svh-28rem)] space-y-0.5 overflow-y-auto">
            {visibleTables.map((t) => (
              <button
                key={`${t.schema}.${t.name}`}
                onClick={() => select(t)}
                className={cn(
                  "flex w-full min-w-0 flex-col rounded-md px-2 py-1.5 text-left transition-colors",
                  table === t.name && selection?.schema === t.schema
                    ? "bg-primary/12 font-medium text-foreground"
                    : "hover:bg-accent",
                )}
              >
                <span className="truncate text-[13px]">{t.name}</span>
                <span className="truncate text-[11px] text-muted-foreground">
                  {schemaNames.length > 1 ? `${t.schema} · ` : ""}
                  {t.type} · {t.estimatedRows.toLocaleString()} rows
                  {t.size ? ` · ${bytes(t.size)}` : ""}
                </span>
              </button>
            ))}
            {tables.loading && <LoadingRows rows={4} />}
            {tables.data?.length === 0 && (
              <p className="p-2 text-xs text-muted-foreground">No tables found.</p>
            )}
            {tables.data && tables.data.length > 0 && visibleTables.length === 0 && (
              <p className="p-2 text-xs text-muted-foreground">No tables match the filter.</p>
            )}
          </div>
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader
          icon={Table2}
          title={table ?? "Pick a table"}
          description={
            rows.data
              ? `${rows.data.rowCount} rows in ${rows.data.duration}${rows.data.truncated ? " (truncated)" : ""}`
              : undefined
          }
          actions={
            table && (
              <>
                {canWrite && (
                  <Button size="sm" variant="outline" onClick={() => setEditor({ mode: "insert" })}>
                    <Plus className="size-3.5" />
                    Insert
                  </Button>
                )}
                <Button size="sm" variant="ghost" title="Export CSV" onClick={() => exportTable("csv")}>
                  <Download className="size-3.5" />
                  CSV
                </Button>
                <Button size="sm" variant="ghost" title="Export JSON" onClick={() => exportTable("json")}>
                  <FileJson className="size-3.5" />
                  JSON
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={offset === 0}
                  onClick={() => setOffset((o) => Math.max(0, o - PAGE))}
                >
                  Previous
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={rows.data ? rows.data.rowCount < PAGE : true}
                  onClick={() => setOffset((o) => o + PAGE)}
                >
                  Next
                </Button>
              </>
            )
          }
        />
        <PanelBody flush>
          {rows.error && <ErrorState error={rows.error} className="m-4" />}
          {!table && <EmptyState icon={Table2} title="Select a table to browse" />}
          {table && rows.data && (
            <>
              {canWrite && pk.length === 0 && detail.data && (
                <p className="border-b border-hairline bg-muted/30 px-4 py-1.5 text-[11px] text-muted-foreground">
                  This table has no primary key, so rows cannot be edited individually. Use the Query
                  tab with an explicit WHERE clause.
                </p>
              )}
              <ResultGrid
                result={rows.data}
                onEdit={canEditRows ? (row) => setEditor({ mode: "edit", initial: row }) : undefined}
                onDelete={canEditRows ? deleteRow : undefined}
              />
            </>
          )}
        </PanelBody>
      </Panel>

      {editor && detail.data && (
        <RowEditor
          open
          onOpenChange={(o) => !o && setEditor(null)}
          mode={editor.mode}
          columns={detail.data.columns}
          primaryKey={pk}
          initial={editor.initial}
          onSubmit={editor.mode === "insert" ? insertRow : updateRow}
        />
      )}
    </div>
  )
}
