"use client"

import { useEffect, useRef, useState } from "react"
import { Database, DownloadCloud, Flame, Pencil, Plus, Trash2 } from "lucide-react"
import { notify } from "@/lib/toast"
import { del, downloadUrl, get, post } from "@/lib/api"
import { bytes, plural } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { DbConnection, DbDriverInfo, DbSyncResult } from "@/lib/types"
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
import { NewDatabaseDialog } from "@/components/database/new-database-dialog"
import { StructureTab } from "@/components/database/structure-tab"
import { QueryTab } from "@/components/database/query-tab"
import { OrmTab } from "@/components/database/orm-tab"
import { MonitorTab } from "@/components/database/monitor-tab"
import { SearchTab } from "@/components/database/search-tab"
import { ErDiagram } from "@/components/database/er-diagram"
import { RedisBrowser } from "@/components/database/redis-browser"
import { MongoBrowser } from "@/components/database/mongo-browser"
import { ConnectionDialog } from "@/components/database/connection-dialog"

export default function DatabasesPage() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [selectedId, setSelectedId] = useState<number | null>(null)
  // The table selection carries the connection it belongs to, so switching
  // connections drops it without an effect: a selection from another engine is
  // simply not this connection's and is treated as none.
  const [selection, setSelection] = useState<(TableSelection & { connId: number }) | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [newOpen, setNewOpen] = useState(false)
  // A database that was just created is not in the connection list yet, so its
  // name is held until a refresh brings it in and then selected.
  const [pendingSelect, setPendingSelect] = useState<string | null>(null)
  const [editConn, setEditConn] = useState<DbConnection | null>(null)

  const connections = usePoll(
    (signal) => get<DbConnection[]>("/databases/", undefined, signal),
    60000,
  )
  // The engine catalogue decides which tabs exist. Keeping that on the server
  // means a newly registered dialect shows up here with no frontend change, and
  // a tab that would fail on every request is never offered.
  const drivers = usePoll(
    (signal) => get<DbDriverInfo[]>("/databases/drivers", undefined, signal),
    0,
  )

  // A database that was just created is selected by name rather than by id,
  // because it has no id here until the refresh that follows brings it in.
  // Derived rather than synchronised in an effect: the name is simply preferred
  // while it is set, and choosing anything else clears it.
  const active =
    (pendingSelect && connections.data?.find((c) => c.name === pendingSelect)) ||
    connections.data?.find((c) => c.id === selectedId) ||
    connections.data?.[0] ||
    null
  const setActive = (conn: DbConnection | null) => {
    setPendingSelect(null)
    setSelectedId(conn?.id ?? null)
  }

  // Databases running on this server connect themselves.
  //
  // Everything a connection needs is readable from the container, so a list of
  // them with a Connect button each was a question with one sensible answer,
  // occupying the top of the page until it was answered. This asks the server
  // to reconcile once per mount: it is idempotent, skips by address, and stays
  // out of the audit log when it adds nothing.
  const synced = useRef(false)
  useEffect(() => {
    if (synced.current || !can("system.admin")) return
    synced.current = true
    post<DbSyncResult>("/databases/sync", {})
      .then((res) => {
        if (res.added.length > 0) {
          connections.refresh()
          notify.success(`Connected ${plural(res.added.length, "database")} on this server`, {
            description: res.added.join(", "),
          })
        }
        // A database that was recognised and could not be reached is the one
        // case worth interrupting for: it is running, it is on the Docker page,
        // and without this the reconcile appears to have done nothing about it.
        // Warned rather than errored — nothing is broken, something is
        // unpublished — and left up until dismissed, because the reason names
        // the fix and is longer than a glance.
        for (const server of res.unreachable ?? []) {
          notify.warning(`${server.container} is running but cannot be reached`, {
            description: server.reason,
            duration: Infinity,
          })
        }
      })
      // A host with no Docker socket has nothing to reconcile, which is not
      // worth saying on every page load.
      .catch(() => undefined)
  }, [can, connections])

  const activeSelection = selection && selection.connId === active?.id ? selection : null
  const info = drivers.data?.find((d) => d.id === active?.driver)
  const isSQL = info?.sql ?? false
  const isRedis = active?.driver === "redis"
  const isMongo = active?.driver === "mongodb"

  return (
    // fill, so the page is exactly the space the shell handed it and the
    // scrolling happens inside the panel that owns the content. A grid that
    // grew past the bottom took the header and the connection picker with it,
    // which is what made choosing a table feel like losing your place.
    <Page fill>
      <PageHeader
        eyebrow="Access"
        title="Databases"
        description="Browse and edit data, reshape schemas, run queries, and generate ORM models"
        actions={
          can("system.admin") && (
            <Button size="sm" onClick={() => setNewOpen(true)}>
              <Plus className="size-4" />
              New database
            </Button>
          )
        }
      />

      {connections.loading && <LoadingPanel />}
      {connections.error && <ErrorState error={connections.error} />}

      {connections.data?.length === 0 && (
        <EmptyState
          icon={Database}
          title="No databases yet"
          description="Anything running on this server is connected automatically. Use New database to start one — it takes a free port, generates its own password and connects itself — or, from the same dialog, point the dashboard at a database somewhere else."
        />
      )}

      {connections.data && connections.data.length > 0 && (
        <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3">
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
                    {conn.name} ·{" "}
                    {drivers.data?.find((d) => d.id === conn.driver)?.label ?? conn.driver}
                    {conn.host ? ` · ${conn.host}` : ""}
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
                  onClick={() =>
                    confirm({
                      title: "Remove connection",
                      confirmLabel: "Remove",
                      description: (
                        <p>
                          Removes <b>{active.name}</b> from the dashboard. The database itself is
                          not touched.
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
                {/*
                 * The one action on this page that nothing undoes. It is
                 * deliberately the last item and the only red one: "Remove"
                 * beside it forgets a connection string, and the two were
                 * indistinguishable while the destructive one did not exist.
                 */}
                <Button
                  size="sm"
                  variant="ghost"
                  className="text-destructive"
                  onClick={() =>
                    confirm({
                      title: "Delete database",
                      confirmLabel: "Delete for good",
                      phrase: dropPhrase(active),
                      description: (
                        <div className="space-y-2">
                          <p>
                            Deletes <b>{dropPhrase(active)}</b> on {active.host || "this server"}.{" "}
                            {dropExplanation(active)}
                          </p>
                          <p>
                            Nothing here can bring it back — take a dump first if you might want it.
                            If this database was started from this page, its container keeps
                            running; remove that from Docker.
                          </p>
                        </div>
                      ),
                      action: async (c) => {
                        const res = await del<{ connectionRemoved: boolean }>(
                          `/databases/${active.id}/database`,
                          // An empty object rather than no body: the route
                          // accepts an optional database name, and the server
                          // defaults it to this connection's own.
                          { confirm: c, body: {} },
                        )
                        if (res.connectionRemoved) setActive(null)
                        connections.refresh()
                      },
                    })
                  }
                >
                  <Flame className="size-4" />
                  Delete database
                </Button>
              </>
            )}
          </Toolbar>

          {active && (
            <Tabs
              defaultValue="browse"
              key={active.id}
              className="flex min-h-0 min-w-0 flex-1 flex-col gap-3"
            >
              <TabsList>
                <TabsTrigger value="browse">
                  {isRedis ? "Keys" : isMongo ? "Collections" : "Browse"}
                </TabsTrigger>
                {isSQL && <TabsTrigger value="structure">Structure</TabsTrigger>}
                {isSQL && <TabsTrigger value="diagram">Diagram</TabsTrigger>}
                {isSQL && <TabsTrigger value="query">Query</TabsTrigger>}
                {isSQL && <TabsTrigger value="search">Find</TabsTrigger>}
                {isSQL && <TabsTrigger value="monitor">Monitor</TabsTrigger>}
                {isSQL && <TabsTrigger value="orm">Generate</TabsTrigger>}
              </TabsList>

              <TabsContent value="browse" className="min-w-0 flex-1 overflow-y-auto">
                {isRedis ? (
                  <RedisBrowser conn={active} confirm={confirm} />
                ) : isMongo ? (
                  <MongoBrowser conn={active} confirm={confirm} />
                ) : (
                  <BrowseTab
                    conn={active}
                    info={info}
                    confirm={confirm}
                    selection={activeSelection}
                    onSelect={(sel) => setSelection(sel && { ...sel, connId: active.id })}
                  />
                )}
              </TabsContent>

              {isSQL && (
                <TabsContent value="structure" className="min-w-0 flex-1 overflow-y-auto">
                  <StructureTab
                    conn={active}
                    info={info}
                    confirm={confirm}
                    schema={activeSelection?.schema ?? ""}
                    table={activeSelection?.table}
                  />
                </TabsContent>
              )}
              {/* The diagram is a canvas: it takes the space and pans inside
                  itself rather than scrolling the page. */}
              {isSQL && (
                <TabsContent value="diagram" className="flex min-h-0 min-w-0 flex-1 flex-col">
                  <ErDiagram
                    conn={active}
                    schema={activeSelection?.schema ?? ""}
                    // Clicking a table in the diagram lands on it in Browse,
                    // which is the whole reason to look at a schema map.
                    onOpenTable={(schema, table) =>
                      setSelection({ schema, table, connId: active.id })
                    }
                  />
                </TabsContent>
              )}
              {isSQL && (
                <TabsContent value="query" className="min-w-0 flex-1 overflow-y-auto">
                  <QueryTab conn={active} confirm={confirm} />
                </TabsContent>
              )}
              {isSQL && (
                <TabsContent value="search" className="min-w-0 flex-1 overflow-y-auto">
                  <SearchTab
                    conn={active}
                    schema={activeSelection?.schema ?? ""}
                    onOpenTable={(schema, table) =>
                      setSelection({ schema, table, connId: active.id })
                    }
                  />
                </TabsContent>
              )}
              {isSQL && (
                <TabsContent value="monitor" className="min-w-0 flex-1 overflow-y-auto">
                  <MonitorTab
                    conn={active}
                    schema={activeSelection?.schema ?? ""}
                    confirm={confirm}
                    onOpenTable={(schema, table) =>
                      setSelection({ schema, table, connId: active.id })
                    }
                  />
                </TabsContent>
              )}
              {isSQL && (
                <TabsContent value="orm" className="min-w-0 flex-1 overflow-y-auto">
                  <OrmTab conn={active} schema={activeSelection?.schema ?? ""} />
                </TabsContent>
              )}
            </Tabs>
          )}
        </div>
      )}

      {can("system.admin") && (
        <>
          <NewDatabaseDialog
            open={newOpen}
            onOpenChange={setNewOpen}
            onCreated={(name) => {
              // Both halves matter: the refresh brings the new row in, and the
              // name is what selects it once it arrives. Without the refresh
              // the pending name matches nothing until the next slow poll.
              connections.refresh()
              setPendingSelect(name)
            }}
            onConnectManually={() => setAddOpen(true)}
          />
          <ConnectionDialog open={addOpen} onOpenChange={setAddOpen} onDone={connections.refresh} />
        </>
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

  // The dump stays on the server — that is what the restore route reads, and
  // what a scheduled job would keep — and a copy comes down to the browser.
  // A backup whose only copy lives on the machine it is protecting is not one.
  const download = (file: string) => {
    const a = document.createElement("a")
    a.href = downloadUrl(`/databases/${conn.id}/backup/download`, { file })
    a.download = file
    a.click()
  }

  const run = async () => {
    setBusy(true)
    try {
      const res = await post<{
        path: string
        file: string
        size: number
        duration: string
        summary?: string
      }>(`/databases/${conn.id}/backup`, { database: conn.database })
      download(res.file)
      // What the dump actually holds. A dump of nothing and a dump of
      // everything both end in "complete", and "0 tables" is the only thing
      // that tells them apart before the day someone needs the file.
      notify.success("Dump complete", {
        description: [res.summary, `${bytes(res.size)} in ${res.duration}`, res.path]
          .filter(Boolean)
          .join(" · "),
        // A browser that declined the automatic download — or an operator who
        // dismissed the prompt — still has one click to the file, rather than
        // having to take the dump again.
        action: { label: "Download", onClick: () => download(res.file) },
      })
    } catch (err) {
      notify.error("Dump failed", err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Button size="sm" variant="outline" onClick={run} disabled={busy}>
      {busy ? <Spinner /> : <DownloadCloud className="size-4" />}
      Dump &amp; download
    </Button>
  )
}

/**
 * What the operator has to type to delete a database, mirroring the server's
 * dropTargetName. The server re-decides; this is what the dialog shows.
 *
 * It is the database's name on six engines. SQLite's is a file, so it is the
 * file's name rather than the whole path — a phrase too long to type is a
 * phrase that gets pasted without being read. Redis numbers its keyspaces, and
 * "0" is not a confirmation, so it takes the form Redis uses itself.
 */
function dropPhrase(conn: DbConnection) {
  if (conn.driver === "sqlite") return (conn.database ?? "").split("/").pop() ?? ""
  if (conn.driver === "redis") return `db${(conn.database || "0").replace(/^db/, "")}`
  return conn.database ?? ""
}

/** What deleting actually does on this engine, in one sentence for the dialog. */
function dropExplanation(conn: DbConnection) {
  switch (conn.driver) {
    case "sqlite":
      return "The database file is deleted from disk, along with its write-ahead log."
    case "redis":
      return "Every key in this keyspace is deleted. Redis keyspaces are fixed at startup, so the numbered database itself stays — emptied."
    case "oracle":
      return "The schema and everything it owns are dropped. Oracle refuses this while the account is connected, including from this dashboard."
    default:
      return "Every table, view, index and row in it is dropped. The server itself keeps running."
  }
}
