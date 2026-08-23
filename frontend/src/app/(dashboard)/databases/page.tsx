"use client"

import { useState } from "react"
import { Database, DownloadCloud, Pencil, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { DbConnection } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader, Toolbar } from "@/components/page"
import { EmptyState, ErrorState, LoadingPanel, Spinner } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { BrowseTab, type TableSelection } from "@/components/database/browse-tab"
import { StructureTab } from "@/components/database/structure-tab"
import { QueryTab } from "@/components/database/query-tab"
import { OrmTab } from "@/components/database/orm-tab"
import { ConnectionDialog } from "@/components/database/connection-dialog"

const DRIVER_LABELS: Record<string, string> = {
  postgres: "PostgreSQL",
  mysql: "MySQL",
  mongodb: "MongoDB",
  sqlite: "SQLite",
}

export default function DatabasesPage() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [selectedId, setSelectedId] = useState<number | null>(null)
  // The table selection carries the connection it belongs to, so switching
  // connections drops it without an effect: a selection from another engine is
  // simply not this connection's and is treated as none.
  const [selection, setSelection] = useState<(TableSelection & { connId: number }) | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [editConn, setEditConn] = useState<DbConnection | null>(null)

  const connections = usePoll(
    (signal) => get<DbConnection[]>("/databases/", undefined, signal),
    60000,
  )

  const active = connections.data?.find((c) => c.id === selectedId) ?? connections.data?.[0] ?? null
  const setActive = (conn: DbConnection | null) => setSelectedId(conn?.id ?? null)

  const activeSelection = selection && selection.connId === active?.id ? selection : null
  const isSQL = active?.driver !== "mongodb"

  return (
    <Page>
      <PageHeader
        eyebrow="Access"
        title="Databases"
        description="Browse and edit data, run queries, inspect structure, and generate ORM schemas"
        actions={
          can("system.admin") && (
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Plus className="size-4" />
              Add connection
            </Button>
          )
        }
      />

      {connections.loading && <LoadingPanel />}
      {connections.error && <ErrorState error={connections.error} />}

      {connections.data?.length === 0 && (
        <EmptyState
          icon={Database}
          title="No connections configured"
          description="Add a PostgreSQL, MySQL, MongoDB or SQLite connection to browse it here. Connection strings are encrypted at rest and never sent back to the browser."
        />
      )}

      {connections.data && connections.data.length > 0 && (
        <>
          <Toolbar>
            <Select
              value={active?.id.toString() ?? ""}
              onValueChange={(v) =>
                setActive(connections.data!.find((c) => c.id.toString() === v) ?? null)
              }
            >
              <SelectTrigger size="sm" className="w-[22rem]">
                <SelectValue placeholder="Select a connection" />
              </SelectTrigger>
              <SelectContent>
                {connections.data.map((conn) => (
                  <SelectItem key={conn.id} value={conn.id.toString()}>
                    {conn.name} · {DRIVER_LABELS[conn.driver] ?? conn.driver} · {conn.host}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {active && <ConnectionStatus id={active.id} />}
            <span className="flex-1" />
            {active && can("service.control") && <BackupButton conn={active} />}
            {active && can("system.admin") && (
              <>
                <Button size="sm" variant="ghost" onClick={() => setEditConn(active)}>
                  <Pencil className="size-4" />
                  Edit
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  className="text-destructive"
                  onClick={() =>
                    confirm({
                      title: "Remove connection",
                      phrase: active.name,
                      confirmLabel: "Remove",
                      description: (
                        <p>
                          Removes <b>{active.name}</b> from the dashboard. The database itself is not
                          touched.
                        </p>
                      ),
                      action: async (c) => {
                        await del(`/databases/${active.id}`, { confirm: c })
                        setActive(null)
                        connections.refresh()
                      },
                    })
                  }
                >
                  <Trash2 className="size-4" />
                  Remove
                </Button>
              </>
            )}
          </Toolbar>

          {active && (
            <Tabs defaultValue="browse" key={active.id} className="min-w-0 gap-4">
              <TabsList>
                <TabsTrigger value="browse">Browse</TabsTrigger>
                {isSQL && <TabsTrigger value="structure">Structure</TabsTrigger>}
                {isSQL && <TabsTrigger value="query">Query</TabsTrigger>}
                {isSQL && <TabsTrigger value="orm">ORM</TabsTrigger>}
              </TabsList>
              <TabsContent value="browse" className="min-w-0">
                <BrowseTab
                  conn={active}
                  confirm={confirm}
                  selection={activeSelection}
                  onSelect={(sel) => setSelection({ ...sel, connId: active.id })}
                />
              </TabsContent>
              {isSQL && (
                <TabsContent value="structure" className="min-w-0">
                  <StructureTab
                    conn={active}
                    schema={activeSelection?.schema ?? ""}
                    table={activeSelection?.table}
                  />
                </TabsContent>
              )}
              {isSQL && (
                <TabsContent value="query" className="min-w-0">
                  <QueryTab conn={active} confirm={confirm} />
                </TabsContent>
              )}
              {isSQL && (
                <TabsContent value="orm" className="min-w-0">
                  <OrmTab conn={active} schema="" />
                </TabsContent>
              )}
            </Tabs>
          )}
        </>
      )}

      {can("system.admin") && (
        <ConnectionDialog open={addOpen} onOpenChange={setAddOpen} onDone={connections.refresh} />
      )}
      {editConn && (
        <ConnectionDialog
          key={editConn.id}
          open
          onOpenChange={(o) => !o && setEditConn(null)}
          onDone={connections.refresh}
          existing={editConn}
        />
      )}
      {dialog}
    </Page>
  )
}

function ConnectionStatus({ id }: { id: number }) {
  const { data } = usePoll(
    (signal) => get<{ ok: boolean; error?: string }>(`/databases/${id}/ping`, undefined, signal),
    30000,
    [id],
  )
  if (!data) return <Spinner className="text-muted-foreground" />
  return (
    <Badge variant={data.ok ? "success" : "destructive"} className="font-normal" title={data.error}>
      <span className={cn("size-1.5 rounded-full", data.ok ? "bg-success" : "bg-destructive")} />
      {data.ok ? "connected" : "unreachable"}
    </Badge>
  )
}

function BackupButton({ conn }: { conn: DbConnection }) {
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    try {
      const res = await post<{ path: string; size: number; duration: string }>(
        `/databases/${conn.id}/backup`,
        { database: conn.database },
      )
      toast.success("Dump complete", {
        description: `${bytes(res.size)} in ${res.duration} → ${res.path}`,
      })
    } catch (err) {
      toast.error("Dump failed", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }
  return (
    <Button size="sm" variant="outline" onClick={run} disabled={busy}>
      {busy ? <Spinner /> : <DownloadCloud className="size-4" />}
      Dump now
    </Button>
  )
}
