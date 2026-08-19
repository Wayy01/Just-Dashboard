"use client"

import { useEffect, useState } from "react"
import { Database, DownloadCloud, Play, Plus, ShieldAlert, Table2, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { DbConnection, DbTable, QueryResult, QueryRisk } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { CodeEditor } from "@/components/code-editor"
import { Page, PageHeader, Toolbar } from "@/components/page"
import { Panel, PanelBody, PanelFooter, PanelHeader, Well } from "@/components/panel"
import {
  EmptyState,
  ErrorState,
  LoadingPanel,
  LoadingRows,
  Notice,
  Spinner,
} from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
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
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"

export default function DatabasesPage() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const connections = usePoll(
    (signal) => get<DbConnection[]>("/databases/", undefined, signal),
    60000,
  )

  // Derived rather than stored: the first connection is the default until the
  // operator picks another, which needs no extra render to settle.
  const active = connections.data?.find((c) => c.id === selectedId) ?? connections.data?.[0] ?? null
  const setActive = (conn: DbConnection | null) => setSelectedId(conn?.id ?? null)

  return (
    <Page>
      <PageHeader
        eyebrow="Access"
        title="Databases"
        description="Browse schemas, run queries and take dumps"
        actions={can("system.admin") && <AddConnectionDialog onDone={connections.refresh} />}
      />

      {connections.loading && <LoadingPanel />}
      {connections.error && <ErrorState error={connections.error} />}

      {connections.data?.length === 0 && (
        <EmptyState
          icon={Database}
          title="No connections configured"
          description="Add a Postgres, MySQL or MongoDB connection to browse it here. Connection strings are encrypted at rest and never sent back to the browser."
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
                    {conn.name} · {conn.driver} · {conn.host}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {active && <ConnectionStatus id={active.id} />}
            <span className="flex-1" />
            {active && can("service.control") && <BackupButton conn={active} />}
            {active && can("system.admin") && (
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
            )}
          </Toolbar>

          {active && (
            <Tabs defaultValue="browse" key={active.id} className="min-w-0 gap-4">
              <TabsList>
                <TabsTrigger value="browse">Browse</TabsTrigger>
                <TabsTrigger value="query">Query</TabsTrigger>
              </TabsList>
              <TabsContent value="browse" className="min-w-0">
                <BrowseTab conn={active} />
              </TabsContent>
              <TabsContent value="query" className="min-w-0">
                <QueryTab conn={active} confirm={confirm} />
              </TabsContent>
            </Tabs>
          )}
        </>
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

function BrowseTab({ conn }: { conn: DbConnection }) {
  const [schema, setSchema] = useState(conn.database)
  const [table, setTable] = useState<string>()
  const [offset, setOffset] = useState(0)

  const schemas = usePoll(
    (signal) =>
      get<{ name: string; size: number }[]>(`/databases/${conn.id}/schemas`, undefined, signal),
    0,
    [conn.id],
  )
  const tables = usePoll(
    (signal) => get<DbTable[]>(`/databases/${conn.id}/tables`, { schema }, signal),
    0,
    [conn.id, schema],
  )
  const rows = usePoll(
    (signal) =>
      table
        ? get<QueryResult>(
            `/databases/${conn.id}/browse`,
            { schema, table, limit: 100, offset },
            signal,
          )
        : Promise.resolve(null as unknown as QueryResult),
    0,
    [conn.id, schema, table, offset],
  )

  return (
    <div className="grid gap-4 lg:grid-cols-[16rem_minmax(0,1fr)] [&>*]:min-w-0">
      <Panel>
        <PanelHeader
          icon={Database}
          title="Schemas"
          description={`${tables.data?.length ?? 0} tables`}
        />
        <PanelBody className="space-y-3">
          <Select
            value={schema}
            onValueChange={(v) => {
              setSchema(v)
              setTable(undefined)
            }}
          >
            <SelectTrigger size="sm" className="w-full">
              <SelectValue placeholder="Database" />
            </SelectTrigger>
            <SelectContent>
              {schemas.data?.map((s) => (
                <SelectItem key={s.name} value={s.name}>
                  {s.name} {s.size > 0 && `(${bytes(s.size)})`}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="max-h-[calc(100svh-26rem)] space-y-0.5 overflow-y-auto">
            {tables.data?.map((t) => (
              <button
                key={`${t.schema}.${t.name}`}
                onClick={() => {
                  setTable(t.name)
                  setSchema(t.schema)
                  setOffset(0)
                }}
                className={cn(
                  "flex w-full min-w-0 flex-col rounded-md px-2 py-1.5 text-left transition-colors",
                  table === t.name
                    ? "bg-primary/12 font-medium text-foreground"
                    : "hover:bg-accent",
                )}
              >
                <span className="truncate text-[13px]">{t.name}</span>
                <span className="truncate text-[11px] text-muted-foreground">
                  {t.type} · {t.estimatedRows.toLocaleString()} rows
                </span>
              </button>
            ))}
            {tables.loading && <LoadingRows rows={4} />}
            {tables.data?.length === 0 && (
              <p className="p-2 text-xs text-muted-foreground">No tables in this schema.</p>
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
                <Button
                  size="sm"
                  variant="outline"
                  disabled={offset === 0}
                  onClick={() => setOffset((o) => Math.max(0, o - 100))}
                >
                  Previous
                </Button>
                <Button size="sm" variant="outline" onClick={() => setOffset((o) => o + 100)}>
                  Next
                </Button>
              </>
            )
          }
        />
        <PanelBody flush>
          {rows.error && <ErrorState error={rows.error} className="m-4" />}
          {!table && <EmptyState icon={Table2} title="Select a table to browse" />}
          {rows.data && <ResultTable result={rows.data} />}
        </PanelBody>
      </Panel>
    </div>
  )
}

function ResultTable({ result }: { result: QueryResult }) {
  if (result.columns.length === 0) {
    return (
      <p className="p-4 text-[13px] text-muted-foreground">
        {result.rowsAffected} row(s) affected in {result.duration}.
      </p>
    )
  }
  return (
    <Table containerClassName="max-h-[calc(100svh-24rem)]">
      <TableHeader className={stickyTableHeader}>
        <TableRow>
          {result.columns.map((col, i) => (
            <TableHead key={col} className="whitespace-nowrap">
              {col}
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
          <TableRow key={i}>
            {row.map((cell, j) => (
              <TableCell key={j} className="max-w-xs truncate font-mono text-xs">
                {cell === null ? (
                  <span className="text-muted-foreground italic">null</span>
                ) : typeof cell === "object" ? (
                  JSON.stringify(cell)
                ) : (
                  String(cell)
                )}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

type ConfirmFn = ReturnType<typeof useConfirm>["confirm"]

function QueryTab({ conn, confirm }: { conn: DbConnection; confirm: ConfirmFn }) {
  const { can } = useAuth()
  const [sql, setSql] = useState("SELECT 1;")
  const [risk, setRisk] = useState<QueryRisk | null>(null)
  const [result, setResult] = useState<QueryResult | null>(null)
  const [busy, setBusy] = useState(false)

  // Classification happens as you type, so the warning appears before the
  // statement is ever sent to the database.
  useEffect(() => {
    const timer = setTimeout(() => {
      post<QueryRisk>(`/databases/${conn.id}/classify`, { query: sql })
        .then(setRisk)
        .catch(() => setRisk(null))
    }, 400)
    return () => clearTimeout(timer)
  }, [sql, conn.id])

  const execute = async (confirmText?: string) => {
    setBusy(true)
    try {
      const res = await post<{ result: QueryResult; risk: QueryRisk }>(
        `/databases/${conn.id}/query`,
        { query: sql, maxRows: 500 },
        { confirm: confirmText },
      )
      setResult(res.result)
      toast.success(
        res.result.columns.length
          ? `${res.result.rowCount} rows in ${res.result.duration}`
          : `${res.result.rowsAffected} rows affected`,
      )
    } catch (err) {
      toast.error("Query failed", { description: String(err) })
      throw err
    } finally {
      setBusy(false)
    }
  }

  const run = () => {
    if (risk?.destructive) {
      confirm({
        title: "Run destructive statement",
        phrase: `run ${risk.level}`,
        confirmLabel: "Run it",
        description: (
          <>
            <p className="text-destructive">This statement {risk.reasons.join(", ")}.</p>
            <Well className="mt-2 max-h-32">{sql}</Well>
          </>
        ),
        action: (c) => execute(c),
      })
      return
    }
    execute().catch(() => undefined)
  }

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {risk?.destructive && (
        <Notice tone="danger" icon={ShieldAlert} title={`Destructive statement (${risk.level})`}>
          {risk.reasons.join(" · ")}
        </Notice>
      )}

      <Panel>
        <PanelHeader
          icon={Database}
          title="SQL"
          description={`${conn.driver} · ${conn.database}`}
        />
        <PanelBody flush>
          <CodeEditor className="h-56" language="sql" value={sql} onChange={setSql} />
        </PanelBody>
        <PanelFooter>
          <Button size="sm" onClick={run} disabled={busy || !can("service.control")}>
            {busy ? <Spinner /> : <Play className="size-3.5" />}
            Run
          </Button>
          {risk && !risk.destructive && (
            <Badge variant="secondary" className="font-normal">
              {risk.level}
            </Badge>
          )}
          {!can("service.control") && (
            <span className="text-xs text-muted-foreground">
              Your role can browse but not execute statements.
            </span>
          )}
        </PanelFooter>
      </Panel>

      {result && (
        <Panel>
          <PanelHeader icon={Table2} title="Result" description={result.statement} />
          <PanelBody flush>
            <ResultTable result={result} />
          </PanelBody>
        </Panel>
      )}
    </div>
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

function AddConnectionDialog({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [driver, setDriver] = useState("postgres")
  const [dsn, setDsn] = useState("")

  const placeholders: Record<string, string> = {
    postgres: "postgres://user:password@127.0.0.1:5432/dbname?sslmode=disable",
    mysql: "user:password@tcp(127.0.0.1:3306)/dbname",
    mongodb: "mongodb://user:password@127.0.0.1:27017/dbname",
  }

  const create = async () => {
    try {
      await post("/databases/", { name, driver, dsn })
      toast.success(`Added ${name}`)
      setOpen(false)
      setName("")
      setDsn("")
      onDone()
    } catch (err) {
      toast.error("Could not add connection", { description: String(err) })
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus className="size-4" />
          Add connection
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Add database connection</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="conn-name">Name</Label>
            <Input id="conn-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label>Driver</Label>
            <Select value={driver} onValueChange={setDriver}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="postgres">PostgreSQL</SelectItem>
                <SelectItem value="mysql">MySQL / MariaDB</SelectItem>
                <SelectItem value="mongodb">MongoDB</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="conn-dsn">Connection string</Label>
            <Input
              id="conn-dsn"
              value={dsn}
              onChange={(e) => setDsn(e.target.value)}
              className="font-mono text-xs"
              placeholder={placeholders[driver]}
            />
            <p className="text-xs text-muted-foreground">
              Encrypted with the dashboard&apos;s master key and never returned to a browser.
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button onClick={create} disabled={!name || !dsn}>
            Add
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
