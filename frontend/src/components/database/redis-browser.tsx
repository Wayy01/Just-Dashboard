"use client"

import { useCallback, useState } from "react"
import { Clock, Copy, Database, KeyRound, Save, Search, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { bytes } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { DbConnection, RedisPage, RedisValue } from "@/lib/types"
import { useAuth } from "@/hooks/use-auth"
import { usePoll } from "@/hooks/use-poll"
import type { useConfirm } from "@/components/confirm-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Panel, PanelBody, PanelFooter, PanelHeader } from "@/components/panel"
import { EmptyState, LoadingRows, Spinner } from "@/components/state"
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

type ConfirmFn = ReturnType<typeof useConfirm>["confirm"]

/**
 * Redis in its own vocabulary.
 *
 * The temptation is to render keys as a table and call it a data grid, which is
 * how most panels bolt Redis on and why none of them are pleasant to use: a key
 * has a type, a TTL and a shape that changes what "edit" even means. So this is
 * a key list beside a typed value editor, and the pattern box is a real SCAN
 * pattern rather than a client-side filter over one page — on a keyspace with
 * millions of keys, filtering what you happened to fetch is not searching.
 *
 * Paging is a cursor, not an offset, because SCAN is. The cursor stack is what
 * makes "previous" possible at all: SCAN can only go forward, so going back
 * means remembering where you have been.
 */
export function RedisBrowser({ conn, confirm }: { conn: DbConnection; confirm: ConfirmFn }) {
  const { can } = useAuth()
  const [db, setDb] = useState("0")
  const [pattern, setPattern] = useState("*")
  const [applied, setApplied] = useState("*")
  const [cursor, setCursor] = useState(0)
  const [history, setHistory] = useState<number[]>([])
  const [selected, setSelected] = useState<string | null>(null)

  const databases = usePoll(
    (signal) => get<{ name: string; size: number }[]>(`/databases/${conn.id}/schemas`, undefined, signal),
    0,
    [conn.id],
  )
  const page = usePoll(
    (signal) =>
      get<RedisPage>(`/databases/${conn.id}/keys`, { db, pattern: applied, cursor, count: 100 }, signal),
    0,
    [conn.id, db, applied, cursor],
  )
  const value = usePoll(
    (signal) =>
      selected
        ? get<RedisValue>(`/databases/${conn.id}/keys/value`, { db, key: selected }, signal)
        : Promise.resolve(null as unknown as RedisValue),
    0,
    [conn.id, db, selected],
  )

  const search = () => {
    setCursor(0)
    setHistory([])
    setApplied(pattern.trim() || "*")
  }
  const next = () => {
    if (!page.data || page.data.done) return
    setHistory((h) => [...h, cursor])
    setCursor(page.data.cursor)
  }
  const prev = () => {
    setHistory((h) => {
      if (h.length === 0) return h
      setCursor(h[h.length - 1])
      return h.slice(0, -1)
    })
  }

  const canWrite = can("service.control")
  const reload = useCallback(() => {
    page.refresh()
    value.refresh()
  }, [page, value])

  const deleteKey = (key: string) =>
    confirm({
      title: "Delete key",
      phrase: key,
      confirmLabel: "Delete",
      description: (
        <p>
          Permanently removes <span className="font-mono text-xs">{key}</span> and its value.
        </p>
      ),
      action: async (c) => {
        await del(`/databases/${conn.id}/keys`, { body: { key }, confirm: c, query: { db } })
        toast.success("Key deleted")
        setSelected(null)
        page.refresh()
      },
    })

  return (
    <div className="grid gap-4 lg:grid-cols-[22rem_minmax(0,1fr)] [&>*]:min-w-0">
      <Panel>
        <PanelHeader
          icon={Database}
          title="Keys"
          description={`${page.data?.keys.length ?? 0} on this page`}
          actions={
            databases.data && databases.data.length > 1 ? (
              <Select
                value={db}
                onValueChange={(v) => {
                  setDb(v)
                  setCursor(0)
                  setHistory([])
                  setSelected(null)
                }}
              >
                <SelectTrigger size="sm" className="w-24">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {databases.data.map((d) => (
                    <SelectItem key={d.name} value={d.name}>
                      db{d.name}
                      {d.size > 0 && ` · ${d.size}`}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : undefined
          }
        />
        <PanelBody className="space-y-3">
          <div className="flex items-center gap-1.5">
            <Input
              value={pattern}
              onChange={(e) => setPattern(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && search()}
              className="h-8 font-mono text-xs"
              placeholder="user:*"
            />
            <Button size="sm" variant="outline" onClick={search}>
              <Search className="size-3.5" />
            </Button>
          </div>
          <p className="text-[11px] text-muted-foreground">
            A glob pattern, matched by the server with SCAN — not a filter over this page.
          </p>

          <div className="max-h-[calc(100svh-30rem)] space-y-0.5 overflow-y-auto">
            {page.loading && <LoadingRows rows={5} />}
            {page.data?.keys.length === 0 && (
              <p className="p-2 text-xs text-muted-foreground">No keys match this pattern.</p>
            )}
            {page.data?.keys.map((k) => (
              <button
                key={k.key}
                onClick={() => setSelected(k.key)}
                className={cn(
                  "flex w-full min-w-0 flex-col rounded-md px-2 py-1.5 text-left transition-colors",
                  selected === k.key
                    ? "bg-primary/12 font-medium text-foreground"
                    : "hover:bg-accent",
                )}
              >
                <span className="truncate font-mono text-[12px]">{k.key}</span>
                <span className="truncate text-[10px] text-muted-foreground">
                  {k.type} · {k.size.toLocaleString()}
                  {k.ttl >= 0 && ` · ttl ${k.ttl}s`}
                </span>
              </button>
            ))}
          </div>
        </PanelBody>
        <PanelFooter>
          <Button size="sm" variant="outline" disabled={history.length === 0} onClick={prev}>
            Previous
          </Button>
          <Button size="sm" variant="outline" disabled={page.data?.done} onClick={next}>
            Next
          </Button>
          {page.data?.done && <span className="text-[11px] text-muted-foreground">End of scan</span>}
        </PanelFooter>
      </Panel>

      <Panel>
        <PanelHeader
          icon={KeyRound}
          title={selected ?? "Pick a key"}
          description={value.data ? `${value.data.type}${value.data.truncated ? " · truncated" : ""}` : undefined}
          actions={
            selected &&
            canWrite && (
              <Button size="sm" variant="ghost" className="text-destructive" onClick={() => deleteKey(selected)}>
                <Trash2 className="size-3.5" />
                Delete
              </Button>
            )
          }
        />
        <PanelBody flush>
          {!selected && <EmptyState icon={KeyRound} title="Select a key to see its value" />}
          {selected && value.loading && (
            <div className="flex items-center gap-2 p-6 text-sm text-muted-foreground">
              <Spinner /> Reading…
            </div>
          )}
          {value.data && (
            <RedisValueView
              // Remounting on the key is what resets the editor's draft. Syncing
              // it in an effect instead left a window where the previous key's
              // text was on screen under the new key's name — and one Save away
              // from being written to the wrong key.
              key={value.data.key}
              conn={conn}
              db={db}
              value={value.data}
              canWrite={canWrite}
              confirm={confirm}
              onChanged={reload}
            />
          )}
        </PanelBody>
      </Panel>
    </div>
  )
}

function RedisValueView({
  conn,
  db,
  value,
  canWrite,
  confirm,
  onChanged,
}: {
  conn: DbConnection
  db: string
  value: RedisValue
  canWrite: boolean
  confirm: ConfirmFn
  onChanged: () => void
}) {
  const [draft, setDraft] = useState(value.string ?? "")
  const [ttl, setTtl] = useState(String(value.ttl))
  const [busy, setBusy] = useState(false)

  const save = async () => {
    setBusy(true)
    try {
      await post(`/databases/${conn.id}/keys/value`, { key: value.key, type: "string", value: draft }, {
        query: { db },
      })
      toast.success("Value saved")
      onChanged()
    } catch (err) {
      toast.error("Could not save", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }

  const saveTtl = async () => {
    setBusy(true)
    try {
      await post(`/databases/${conn.id}/keys/expire`, { key: value.key, ttl: Number(ttl) || -1 }, {
        query: { db },
      })
      toast.success(Number(ttl) > 0 ? `Expires in ${ttl}s` : "Expiry cleared")
      onChanged()
    } catch (err) {
      toast.error("Could not set the TTL", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }

  const copy = (text: string) =>
    navigator.clipboard
      .writeText(text)
      .then(() => toast.success("Copied"))
      .catch(() => toast.error("Could not copy"))

  return (
    <div className="space-y-3 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="secondary" className="font-normal">
          {value.type}
        </Badge>
        <div className="flex items-center gap-1.5">
          <Clock className="size-3.5 text-muted-foreground" />
          <Input
            value={ttl}
            onChange={(e) => setTtl(e.target.value)}
            className="h-7 w-24 font-mono text-xs"
            disabled={!canWrite}
          />
          <span className="text-[11px] text-muted-foreground">seconds (-1 = never)</span>
          {canWrite && (
            <Button size="sm" variant="outline" onClick={saveTtl} disabled={busy}>
              Set
            </Button>
          )}
        </div>
      </div>

      {value.type === "string" && (
        <div className="space-y-2">
          <Label className="text-xs">Value</Label>
          <Textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            className="h-56 font-mono text-xs"
            readOnly={!canWrite}
          />
          <div className="flex items-center gap-1.5">
            {canWrite && (
              <Button size="sm" onClick={save} disabled={busy}>
                {busy ? <Spinner /> : <Save className="size-3.5" />}
                Save
              </Button>
            )}
            <Button size="sm" variant="outline" onClick={() => copy(draft)}>
              <Copy className="size-3.5" />
              Copy
            </Button>
            <span className="text-[11px] text-muted-foreground">{bytes(draft.length)}</span>
          </div>
        </div>
      )}

      {value.hash && (
        <MemberTable
          head={["Field", "Value"]}
          rows={Object.entries(value.hash).map(([k, v]) => [k, v])}
          onDelete={
            canWrite
              ? (field) =>
                  confirm({
                    title: "Delete hash field",
                    phrase: field,
                    confirmLabel: "Delete",
                    description: (
                      <p>
                        Removes <span className="font-mono text-xs">{field}</span> from{" "}
                        <b>{value.key}</b>.
                      </p>
                    ),
                    action: async (c) => {
                      await del(`/databases/${conn.id}/keys`, {
                        body: { key: value.key, field },
                        confirm: c,
                        query: { db },
                      })
                      onChanged()
                    },
                  })
              : undefined
          }
        />
      )}
      {value.list && <MemberTable head={["#", "Value"]} rows={value.list.map((v, i) => [String(i), v])} />}
      {value.set && <MemberTable head={["Member"]} rows={value.set.map((v) => [v])} />}
      {value.zset && (
        <MemberTable head={["Member", "Score"]} rows={value.zset.map((z) => [z.member, String(z.score)])} />
      )}
      {value.stream && (
        <MemberTable
          head={["ID", "Fields"]}
          rows={value.stream.map((e) => [e.id, JSON.stringify(e.values)])}
        />
      )}

      {value.truncated && (
        <p className="text-xs text-muted-foreground">
          Showing the first 500 entries. Use the Query tab of your Redis client for the rest.
        </p>
      )}
      {value.type !== "string" && canWrite && (
        <p className="text-xs text-muted-foreground">
          Collections are read-only here: editing one member from a form would quietly drop
          everything the form did not show.
        </p>
      )}
    </div>
  )
}

function MemberTable({
  head,
  rows,
  onDelete,
}: {
  head: string[]
  rows: string[][]
  onDelete?: (first: string) => void
}) {
  return (
    <div className="overflow-hidden rounded-md border border-hairline">
      <Table containerClassName="max-h-[26rem]">
        <TableHeader>
          <TableRow>
            {head.map((h) => (
              <TableHead key={h}>{h}</TableHead>
            ))}
            {onDelete && <TableHead className="w-10" />}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((r, i) => (
            <TableRow key={i}>
              {r.map((c, j) => (
                <TableCell key={j} className="max-w-md truncate font-mono text-xs">
                  {c}
                </TableCell>
              ))}
              {onDelete && (
                <TableCell className="w-10">
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-6 text-destructive"
                    onClick={() => onDelete(r[0])}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
