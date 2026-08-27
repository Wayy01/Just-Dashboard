"use client"

import { useEffect, useState } from "react"
import {
  Clock,
  Download,
  FileJson,
  Play,
  ListTree,
  Save,
  ShieldAlert,
  Star,
  Trash2,
} from "lucide-react"
import { notify } from "@/lib/toast"
import { plural } from "@/lib/format"
import { del, get, post } from "@/lib/api"
import { cn, ringSafeScroll } from "@/lib/utils"
import type {
  DbConnection,
  DbHistoryEntry,
  DbOutline,
  DbSavedQuery,
  QueryResult,
  QueryRisk,
} from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import type { useConfirm } from "@/components/confirm-dialog"
import { CodeEditor } from "@/components/code-editor"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Panel, PanelBody, PanelFooter, PanelHeader, Well } from "@/components/panel"
import { EmptyState, Notice, Spinner } from "@/components/state"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Database, Table2 } from "lucide-react"
import { ResultGrid } from "@/components/database/result-grid"

type ConfirmFn = ReturnType<typeof useConfirm>["confirm"]

/** The Query tab: the SQL editor, live risk classification, a per-connection
 *  history and saved snippets, and a result you can export. */
export function QueryTab({ conn, confirm }: { conn: DbConnection; confirm: ConfirmFn }) {
  const { can } = useAuth()
  const [sql, setSql] = useState("SELECT 1;")
  const [risk, setRisk] = useState<QueryRisk | null>(null)
  const [result, setResult] = useState<QueryResult | null>(null)
  const [plan, setPlan] = useState<QueryResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [saveOpen, setSaveOpen] = useState(false)

  const saved = usePoll(
    (signal) => get<DbSavedQuery[]>(`/databases/${conn.id}/queries`, undefined, signal),
    0,
    [conn.id],
  )
  const history = usePoll(
    (signal) => get<DbHistoryEntry[]>(`/databases/${conn.id}/history`, { limit: 50 }, signal),
    0,
    [conn.id],
  )
  // The schema, fetched once per connection, so completion knows what exists.
  // A failure here is silent: an editor without suggestions still runs SQL, and
  // an error toast for a convenience nobody asked for is noise.
  const outline = usePoll(
    (signal) => get<DbOutline>(`/databases/${conn.id}/outline`, undefined, signal),
    0,
    [conn.id],
  )

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
      setPlan(null)
      history.refresh()
      notify.success(
        res.result.columns.length
          ? `${plural(res.result.rowCount, "row")} in ${res.result.duration}`
          : `${plural(res.result.rowsAffected, "row")} affected`,
      )
    } catch (err) {
      history.refresh()
      notify.error("Query failed", err)
      throw err
    } finally {
      setBusy(false)
    }
  }

  const run = () => {
    if (risk?.destructive) {
      confirm({
        title: "Run destructive statement",
        // Only a critical statement is typed for — a DROP, a TRUNCATE, or an
        // UPDATE/DELETE with no WHERE. "high" is a scoped write, which is the
        // ordinary work of a SQL console; making somebody type "run high"
        // dozens of times a sitting is how the phrase stops being read.
        phrase: risk.level === "critical" ? `run ${risk.level}` : undefined,
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

  // Planning is not running: every dialect describes the statement without
  // executing it, which is why this needs no confirmation even for a DELETE.
  const explain = async () => {
    setBusy(true)
    try {
      const res = await post<{ result: QueryResult }>(`/databases/${conn.id}/explain`, {
        query: sql,
      })
      setPlan(res.result)
      setResult(null)
    } catch (err) {
      notify.error("Could not plan the statement", err)
    } finally {
      setBusy(false)
    }
  }

  const saveSnippet = async (name: string) => {
    await post(`/databases/${conn.id}/queries`, { name, sql })
    notify.success(`Saved "${name}"`)
    saved.refresh()
  }
  const deleteSnippet = (q: DbSavedQuery) =>
    confirm({
      title: "Delete saved query",
      description: (
        <p>
          Removes the snippet <b>{q.name}</b>.
        </p>
      ),
      action: async () => {
        await del(`/databases/${conn.id}/queries/${q.id}`)
        saved.refresh()
      },
    })

  const exportResult = (format: "csv" | "json") => {
    if (!result) return
    const text = format === "csv" ? toCSV(result) : toJSON(result)
    const blob = new Blob([text], { type: format === "csv" ? "text/csv" : "application/json" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = `query-result.${format}`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {risk?.destructive && (
        <Notice tone="danger" icon={ShieldAlert} title={`Destructive statement (${risk.level})`}>
          {risk.reasons.join(" · ")}
        </Notice>
      )}

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem] [&>*]:min-w-0">
        <Panel>
          <PanelHeader
            icon={Database}
            title="SQL"
            description={`${conn.driver} · ${conn.database}`}
          />
          <PanelBody flush>
            <CodeEditor
              className="h-56"
              language="sql"
              value={sql}
              onChange={setSql}
              completions={outline.data?.tables}
            />
          </PanelBody>
          <PanelFooter>
            <Button size="sm" onClick={run} disabled={busy || !can("service.control")}>
              {busy ? <Spinner /> : <Play className="size-3.5" />}
              Run
            </Button>
            <Button size="sm" variant="outline" onClick={explain} disabled={busy || !sql.trim()}>
              <ListTree className="size-3.5" />
              Explain
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => setSaveOpen(true)}
              disabled={!can("service.control") || !sql.trim()}
            >
              <Save className="size-3.5" />
              Save
            </Button>
            {risk && !risk.destructive && (
              <Badge variant="secondary" className="font-normal">
                {risk.level}
              </Badge>
            )}
            {outline.data && (
              <span className="text-[11px] text-muted-foreground">
                {plural(Object.keys(outline.data.tables).length, "table")} available to autocomplete
              </span>
            )}
            {!can("service.control") && (
              <span className="text-xs text-muted-foreground">
                Your role can browse but not execute statements.
              </span>
            )}
          </PanelFooter>
        </Panel>

        <Panel className="min-h-0">
          <Tabs defaultValue="history" className="gap-0">
            <PanelHeader className="py-2">
              <TabsList className="h-8">
                <TabsTrigger value="history" className="gap-1 text-xs">
                  <Clock className="size-3.5" />
                  History
                </TabsTrigger>
                <TabsTrigger value="saved" className="gap-1 text-xs">
                  <Star className="size-3.5" />
                  Saved
                </TabsTrigger>
              </TabsList>
            </PanelHeader>
            <PanelBody flush>
              <TabsContent value="history" className="m-0">
                <div
                  className={cn(
                    "max-h-[16rem] divide-y divide-hairline overflow-y-auto",
                    ringSafeScroll,
                  )}
                >
                  {history.data?.length === 0 && (
                    <p className="p-3 text-xs text-muted-foreground">No statements yet.</p>
                  )}
                  {history.data?.map((h) => (
                    <button
                      key={h.id}
                      onClick={() => setSql(h.sql)}
                      className="flex w-full flex-col gap-0.5 px-3 py-2 text-left hover:bg-accent"
                    >
                      <span className="truncate font-mono text-[11px]">{h.sql}</span>
                      <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                        <span
                          className={cn(
                            "size-1.5 rounded-full",
                            h.success ? "bg-success" : "bg-destructive",
                          )}
                        />
                        {h.risk} · {plural(h.rowCount, "row")} · {h.durationMs}ms
                      </span>
                    </button>
                  ))}
                </div>
              </TabsContent>
              <TabsContent value="saved" className="m-0">
                <div
                  className={cn(
                    "max-h-[16rem] divide-y divide-hairline overflow-y-auto",
                    ringSafeScroll,
                  )}
                >
                  {saved.data?.length === 0 && (
                    <p className="p-3 text-xs text-muted-foreground">No saved queries.</p>
                  )}
                  {saved.data?.map((q) => (
                    <div
                      key={q.id}
                      className="group flex items-center gap-1 px-3 py-2 hover:bg-accent"
                    >
                      <button
                        onClick={() => setSql(q.sql)}
                        className="flex min-w-0 flex-1 flex-col gap-0.5 text-left"
                      >
                        <span className="truncate text-xs font-medium">{q.name}</span>
                        <span className="truncate font-mono text-[10px] text-muted-foreground">
                          {q.sql}
                        </span>
                      </button>
                      {can("service.control") && (
                        <Button
                          size="icon"
                          variant="ghost"
                          className="size-6 shrink-0 text-destructive opacity-0 group-hover:opacity-100"
                          onClick={() => deleteSnippet(q)}
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      )}
                    </div>
                  ))}
                </div>
              </TabsContent>
            </PanelBody>
          </Tabs>
        </Panel>
      </div>

      {plan && (
        <Panel>
          <PanelHeader
            icon={ListTree}
            title="Query plan"
            description="How the engine intends to run this — nothing was executed"
          />
          <PanelBody flush>
            <ResultGrid result={plan} />
          </PanelBody>
        </Panel>
      )}

      {result && (
        <Panel>
          <PanelHeader
            icon={Table2}
            title="Result"
            description={result.statement}
            actions={
              result.columns.length > 0 && (
                <>
                  <Button size="sm" variant="ghost" onClick={() => exportResult("csv")}>
                    <Download className="size-3.5" />
                    CSV
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => exportResult("json")}>
                    <FileJson className="size-3.5" />
                    JSON
                  </Button>
                </>
              )
            }
          />
          <PanelBody flush>
            <ResultGrid result={result} />
          </PanelBody>
        </Panel>
      )}
      {!result && !plan && <EmptyState icon={Table2} title="Run a statement to see results" />}

      <SaveDialog open={saveOpen} onOpenChange={setSaveOpen} onSave={saveSnippet} />
    </div>
  )
}

function SaveDialog({
  open,
  onOpenChange,
  onSave,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onSave: (name: string) => Promise<void>
}) {
  const [name, setName] = useState("")
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try {
      await onSave(name)
      onOpenChange(false)
      setName("")
    } catch (err) {
      notify.error("Could not save", err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Save query</DialogTitle>
        </DialogHeader>
        <div className="space-y-1.5">
          <Label htmlFor="snippet-name">Name</Label>
          <Input
            id="snippet-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Active users last 30 days"
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button onClick={save} disabled={!name.trim() || busy}>
            {busy && <Spinner />}
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// toCSV / toJSON build a downloadable file from an already-fetched result, so a
// query result the operator is looking at can be saved without re-running it.
function toCSV(result: QueryResult): string {
  const escape = (v: unknown) => {
    if (v === null || v === undefined) return ""
    const s = typeof v === "object" ? JSON.stringify(v) : String(v)
    return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
  }
  const lines = [result.columns.join(",")]
  for (const row of result.rows) lines.push(row.map(escape).join(","))
  return lines.join("\n")
}

function toJSON(result: QueryResult): string {
  const objs = result.rows.map((row) => {
    const o: Record<string, unknown> = {}
    result.columns.forEach((c, i) => (o[c] = row[i]))
    return o
  })
  return JSON.stringify(objs, null, 2)
}
