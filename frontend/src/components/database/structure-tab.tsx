"use client"

import { Copy, KeyRound, Link2, ListTree, Pencil, Table2, Trash2 } from "lucide-react"
import { notify } from "@/lib/toast"
import { plural } from "@/lib/format"
import { del, get } from "@/lib/api"
import type { DbConnection, DbDriverInfo, DbTableDetail } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useState } from "react"
import type { useConfirm } from "@/components/confirm-dialog"
import { RenameDialog } from "@/components/database/ddl-dialogs"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/** The Structure tab: a table's columns, primary key, indexes, foreign keys and
 *  the DDL that would recreate it — the reference view every database tool has
 *  and the original page lacked. */
export function StructureTab({
  conn,
  schema,
  table,
  info,
  confirm,
}: {
  conn: DbConnection
  schema: string
  table?: string
  info?: DbDriverInfo
  confirm: ReturnType<typeof useConfirm>["confirm"]
}) {
  const { can } = useAuth()
  const [renaming, setRenaming] = useState<string | null>(null)
  const detail = usePoll(
    (signal) =>
      table
        ? get<DbTableDetail>(`/databases/${conn.id}/table`, { schema, table }, signal)
        : Promise.resolve(null as unknown as DbTableDetail),
    0,
    [conn.id, schema, table],
  )

  if (!table) return <EmptyState icon={Table2} title="Select a table to inspect its structure" />
  if (detail.loading) return <LoadingPanel />
  if (detail.error) return <ErrorState error={detail.error} />
  if (!detail.data) return null

  const d = detail.data
  const pk = new Set(d.primaryKey)
  const canEdit = can("service.control") && (info?.ddl ?? false)

  const dropColumn = (column: string) =>
    confirm({
      title: "Drop column",
      phrase: column,
      confirmLabel: "Drop column",
      description: (
        <p>
          Permanently removes <span className="font-mono text-xs">{column}</span> from{" "}
          <b>{table}</b>, and every value stored in it. This cannot be undone.
        </p>
      ),
      action: async (c) => {
        await del(`/databases/${conn.id}/ddl/column`, {
          body: { schema, table, name: column },
          confirm: c,
        })
        notify.success(`Dropped ${column}`)
        detail.refresh()
      },
    })

  const dropIndex = (name: string) =>
    confirm({
      title: "Drop index",
      confirmLabel: "Drop index",
      description: (
        <p>
          Removes the index <span className="font-mono text-xs">{name}</span>. Queries relying on it
          will fall back to a scan.
        </p>
      ),
      action: async (c) => {
        await del(`/databases/${conn.id}/ddl/index`, {
          body: { schema, table, name },
          confirm: c,
        })
        notify.success(`Dropped ${name}`)
        detail.refresh()
      },
    })

  return (
    <div className="grid gap-4">
      <Panel>
        <PanelHeader
          icon={ListTree}
          title="Columns"
          description={plural(d.columns.length, "column")}
        />
        <PanelBody flush>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Nullable</TableHead>
                <TableHead>Default</TableHead>
                <TableHead>Key</TableHead>
                {canEdit && <TableHead className="w-20" />}
              </TableRow>
            </TableHeader>
            <TableBody>
              {d.columns.map((c) => (
                <TableRow key={c.name} className="group">
                  <TableCell className="font-mono text-xs font-medium">{c.name}</TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {c.type.toLowerCase()}
                  </TableCell>
                  <TableCell className="text-xs">
                    {c.nullable ? (
                      <span className="text-muted-foreground">yes</span>
                    ) : (
                      <span className="text-foreground">no</span>
                    )}
                  </TableCell>
                  <TableCell className="max-w-40 truncate font-mono text-xs text-muted-foreground">
                    {c.default || "—"}
                  </TableCell>
                  <TableCell>
                    {pk.has(c.name) && (
                      <Badge variant="secondary" className="gap-1 font-normal">
                        <KeyRound className="size-3" />
                        pk
                      </Badge>
                    )}
                  </TableCell>
                  {canEdit && (
                    <TableCell className="w-20">
                      <div className="flex items-center gap-0.5 opacity-40 transition-opacity group-hover:opacity-100">
                        <Button
                          size="icon"
                          variant="ghost"
                          className="size-6"
                          title="Rename column"
                          onClick={() => setRenaming(c.name)}
                        >
                          <Pencil className="size-3.5" />
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          className="size-6 text-destructive"
                          title="Drop column"
                          onClick={() => dropColumn(c.name)}
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </PanelBody>
      </Panel>

      <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        <Panel>
          <PanelHeader icon={KeyRound} title="Indexes" description={`${d.indexes.length}`} />
          <PanelBody flush>
            {d.indexes.length === 0 ? (
              <p className="p-4 text-xs text-muted-foreground">No indexes.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Columns</TableHead>
                    <TableHead>Unique</TableHead>
                    {canEdit && <TableHead className="w-10" />}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {d.indexes.map((ix) => (
                    <TableRow key={ix.name} className="group">
                      <TableCell className="font-mono text-xs">
                        {ix.name}
                        {ix.primary && (
                          <Badge variant="secondary" className="ml-1.5 font-normal">
                            primary
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {ix.columns.join(", ")}
                      </TableCell>
                      <TableCell className="text-xs">{ix.unique ? "yes" : "no"}</TableCell>
                      {canEdit && (
                        <TableCell className="w-10">
                          {!ix.primary && (
                            <Button
                              size="icon"
                              variant="ghost"
                              className="size-6 text-destructive opacity-0 group-hover:opacity-100"
                              title="Drop index"
                              onClick={() => dropIndex(ix.name)}
                            >
                              <Trash2 className="size-3.5" />
                            </Button>
                          )}
                        </TableCell>
                      )}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader icon={Link2} title="Foreign keys" description={`${d.foreignKeys.length}`} />
          <PanelBody flush>
            {d.foreignKeys.length === 0 ? (
              <p className="p-4 text-xs text-muted-foreground">No foreign keys.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Columns</TableHead>
                    <TableHead>References</TableHead>
                    <TableHead>On delete</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {d.foreignKeys.map((fk) => (
                    <TableRow key={fk.name}>
                      <TableCell className="font-mono text-xs">{fk.columns.join(", ")}</TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {fk.refTable}({fk.refColumns.join(", ")})
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {fk.onDelete || "—"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </PanelBody>
        </Panel>
      </div>

      {renaming && table && (
        <RenameDialog
          open
          onOpenChange={(o) => !o && setRenaming(null)}
          connId={conn.id}
          schema={schema}
          table={table}
          kind="column"
          current={renaming}
          onDone={detail.refresh}
        />
      )}

      {d.createSql && (
        <Panel>
          <PanelHeader
            icon={Table2}
            title="Definition"
            description={
              conn.driver === "postgres" ? "generated from structure" : "as reported by the engine"
            }
            actions={
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  navigator.clipboard
                    .writeText(d.createSql!)
                    .then(() => notify.success("Copied DDL"))
                    .catch(() => notify.error("Could not copy"))
                }
              >
                <Copy className="size-3.5" />
                Copy
              </Button>
            }
          />
          <PanelBody flush>
            <pre className="max-h-96 overflow-auto p-4 font-mono text-xs whitespace-pre">
              {d.createSql}
            </pre>
          </PanelBody>
        </Panel>
      )}
    </div>
  )
}
