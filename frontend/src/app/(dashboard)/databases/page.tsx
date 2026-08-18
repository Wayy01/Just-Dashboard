"use client"

import { useEffect, useState } from "react"
import dynamic from "next/dynamic"
import { Database, DownloadCloud, Play, Plus, ShieldAlert, Table2, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes } from "@/lib/format"
import type { DbConnection, DbTable, QueryResult, QueryRisk } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows, Spinner } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
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

const MonacoEditor = dynamic(() => import("@monaco-editor/react"), { ssr: false })

export default function DatabasesPage() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [active, setActive] = useState<DbConnection | null>(null)
  const connections = usePoll(
    (signal) => get<DbConnection[]>("/databases/", undefined, signal),
    60000,
  )

  useEffect(() => {
    if (!active && connections.data?.length) setActive(connections.data[0])
  }, [connections.data, active])

  return (
    <>
      <PageHeader
        title="Databases"
        description="Browse schemas, run queries and take dumps"
        actions={can("system.admin") && <AddConnectionDialog onDone={connections.refresh} />}
      />

      {connections.loading && <LoadingRows />}
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
          <div className="flex flex-wrap items-center gap-2">
            <Select
              value={active?.id.toString() ?? ""}
              onValueChange={(v) =>
                setActive(connections.data!.find((c) => c.id.toString() === v) ?? null)
              }
            >
              <SelectTrigger className="w-72">
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
              </Button>
            )}
          </div>

          {active && (
            <Tabs defaultValue="browse" key={active.id}>
              <TabsList>
                <TabsTrigger value="browse">Browse</TabsTrigger>
                <TabsTrigger value="query">Query</TabsTrigger>
              </TabsList>
              <TabsContent value="browse">
                <BrowseTab conn={active} />
              </TabsContent>
              <TabsContent value="query">
                <QueryTab conn={active} confirm={confirm} />
              </TabsContent>
            </Tabs>
          )}
        </>
      )}
      {dialog}
    </>
  )
}

function ConnectionStatus({ id }: { id: number }) {
  const { data } = usePoll(
    (signal) => get<{ ok: boolean; error?: string }>(`/databases/${id}/ping`, undefined, signal),
    30000,
    [id],
  )
  if (!data) return <Spinner />
  return (
    <Badge variant={data.ok ? "default" : "destructive"} title={data.error}>
      {data.ok ? "connected" : "unreachable"}
    </Badge>
  )
}

function BrowseTab({ conn }: { conn: DbConnection }) {
  const [schema, setSchema] = useState(conn.database)
  const [table, setTable] = useState<string>()
  const [offset, setOffset] = useState(0)

  const schemas = usePoll(
    (signal) => get<{ name: string; size: number }[]>(`/databases/${conn.id}/schemas`, undefined, signal),
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
    <div className="grid gap-4 lg:grid-cols-[16rem_1fr]">
      <Card className="min-h-0">
        <CardHeader className="pb-2">
          <CardTitle className="text-base">Schemas</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 p-3">
          <Select value={schema} onValueChange={(v) => { setSchema(v); setTable(undefined) }}>
            <SelectTrigger>
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
          <ScrollArea className="h-[calc(100vh-24rem)]">
            <div className="space-y-0.5 pr-2">
              {tables.data?.map((t) => (
                <button
                  key={`${t.schema}.${t.name}`}
                  onClick={() => {
                    setTable(t.name)
                    setSchema(t.schema)
                    setOffset(0)
                  }}
                  className={
                    table === t.name
                      ? "flex w-full flex-col rounded-md bg-accent px-2 py-1.5 text-left"
                      : "flex w-full flex-col rounded-md px-2 py-1.5 text-left hover:bg-accent/50"
                  }
                >
                  <span className="truncate text-sm">{t.name}</span>
                  <span className="text-[11px] text-muted-foreground">
                    {t.type} · {t.estimatedRows.toLocaleString()} rows
                  </span>
                </button>
              ))}
              {tables.loading && <LoadingRows rows={4} />}
              {tables.data?.length === 0 && (
                <p className="p-2 text-xs text-muted-foreground">No tables in this schema.</p>
              )}
            </div>
          </ScrollArea>
        </CardContent>
      </Card>

      <Card className="min-h-0">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <div>
            <CardTitle className="text-base">{table ?? "Pick a table"}</CardTitle>
            {rows.data && (
              <CardDescription>
                {rows.data.rowCount} rows in {rows.data.duration}
                {rows.data.truncated && " (truncated)"}
              </CardDescription>
            )}
          </div>
          {table && (
            <div className="flex gap-1">
              <Button size="sm" variant="outline" disabled={offset === 0} onClick={() => setOffset((o) => Math.max(0, o - 100))}>
                Previous
              </Button>
              <Button size="sm" variant="outline" onClick={() => setOffset((o) => o + 100)}>
                Next
              </Button>
            </div>
          )}
        </CardHeader>
        <CardContent className="p-0">
          {rows.error && <ErrorState error={rows.error} className="m-4" />}
          {!table && <EmptyState icon={Table2} title="Select a table to browse" />}
          {rows.data && <ResultTable result={rows.data} />}
        </CardContent>
      </Card>
    </div>
  )
}

function ResultTable({ result }: { result: QueryResult }) {
  if (result.columns.length === 0) {
    return (
      <p className="p-4 text-sm text-muted-foreground">
        {result.rowsAffected} row(s) affected in {result.duration}.
      </p>
    )
  }
  return (
    <ScrollArea className="max-h-[calc(100vh-24rem)]">
      <Table>
        <TableHeader>
          <TableRow>
            {result.columns.map((col, i) => (
              <TableHead key={col} className="whitespace-nowrap">
                {col}
                {result.types[i] && (
                  <span className="ml-1 text-[10px] font-normal text-muted-foreground">
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
    </ScrollArea>
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
            <pre className="mt-2 max-h-32 overflow-auto rounded bg-muted p-2 font-mono text-xs">
              {sql}
            </pre>
          </>
        ),
        action: (c) => execute(c),
      })
      return
    }
    execute().catch(() => undefined)
  }

  return (
    <div className="space-y-4">
      {risk?.destructive && (
        <Alert variant="destructive">
          <ShieldAlert className="size-4" />
          <AlertTitle>Destructive statement ({risk.level})</AlertTitle>
          <AlertDescription>{risk.reasons.join(" · ")}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardContent className="p-0">
          <div className="h-56 border-b">
            <MonacoEditor
              height="100%"
              theme="vs-dark"
              language="sql"
              value={sql}
              onChange={(v) => setSql(v ?? "")}
              options={{
                minimap: { enabled: false },
                fontSize: 13,
                automaticLayout: true,
                scrollBeyondLastLine: false,
              }}
            />
          </div>
          <div className="flex items-center gap-2 p-3">
            <Button onClick={run} disabled={busy || !can("service.control")}>
              {busy ? <Spinner /> : <Play className="size-4" />}
              Run
            </Button>
            {risk && !risk.destructive && (
              <Badge variant="secondary">{risk.level}</Badge>
            )}
            {!can("service.control") && (
              <span className="text-xs text-muted-foreground">
                Your role can browse but not execute statements.
              </span>
            )}
          </div>
        </CardContent>
      </Card>

      {result && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Result</CardTitle>
            <CardDescription className="font-mono text-xs">{result.statement}</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <ResultTable result={result} />
          </CardContent>
        </Card>
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
              <SelectTrigger>
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
