"use client"

import { useState } from "react"
import { Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { errorMessage, post } from "@/lib/api"
import type { DbDriverInfo, DbNewColumn, DbTableDetail } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Spinner } from "@/components/state"
import { Well } from "@/components/panel"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

/**
 * The schema-editing forms.
 *
 * Every one of them shows the statement it will run before it runs it. A DDL
 * form that hides its SQL asks the operator to trust a black box with their
 * schema; showing it costs a few lines and turns the form into something you
 * can also learn from. The server builds the real statement — these previews
 * are rendered from the same fields and are labelled as approximations only
 * where they cannot be exact.
 */

const EMPTY_COLUMN: DbNewColumn = {
  name: "",
  type: "",
  notNull: false,
  primaryKey: false,
  default: "",
}

export function CreateTableDialog({
  open,
  onOpenChange,
  connId,
  schema,
  info,
  onDone,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  connId: number
  schema: string
  info?: DbDriverInfo
  onDone: () => void
}) {
  const [table, setTable] = useState("")
  const [columns, setColumns] = useState<DbNewColumn[]>([{ ...EMPTY_COLUMN }])
  const [busy, setBusy] = useState(false)

  const types = info?.columnTypes ?? []
  const setCol = (i: number, patch: Partial<DbNewColumn>) =>
    setColumns((cs) => cs.map((c, j) => (j === i ? { ...c, ...patch } : c)))

  const submit = async () => {
    setBusy(true)
    try {
      const res = await post<{ statement: string }>(`/databases/${connId}/ddl/table`, {
        schema,
        table,
        columns: columns.filter((c) => c.name && c.type),
      })
      toast.success(`Created ${table}`, { description: res.statement })
      onOpenChange(false)
      setTable("")
      setColumns([{ ...EMPTY_COLUMN }])
      onDone()
    } catch (err) {
      toast.error("Could not create the table", { description: errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }

  const valid = table.trim() !== "" && columns.some((c) => c.name && c.type)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Create table</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="ddl-table">Table name</Label>
            <Input
              id="ddl-table"
              value={table}
              onChange={(e) => setTable(e.target.value)}
              className="font-mono text-xs"
              autoFocus
            />
          </div>

          <div className="space-y-2">
            <Label>Columns</Label>
            <div className="max-h-72 space-y-2 overflow-y-auto pr-1">
              {columns.map((c, i) => (
                <div
                  key={i}
                  className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-2"
                >
                  <Input
                    placeholder="name"
                    value={c.name}
                    onChange={(e) => setCol(i, { name: e.target.value })}
                    className="h-8 font-mono text-xs"
                  />
                  <TypePicker
                    types={types}
                    value={c.type}
                    onChange={(v) => setCol(i, { type: v })}
                  />
                  <div className="flex items-center gap-2">
                    <label className="flex items-center gap-1 text-[11px] text-muted-foreground">
                      <Checkbox
                        checked={c.primaryKey}
                        onCheckedChange={(v) => setCol(i, { primaryKey: Boolean(v) })}
                      />
                      pk
                    </label>
                    <label className="flex items-center gap-1 text-[11px] text-muted-foreground">
                      <Checkbox
                        checked={c.notNull}
                        onCheckedChange={(v) => setCol(i, { notNull: Boolean(v) })}
                      />
                      req
                    </label>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="size-7 text-destructive"
                      disabled={columns.length === 1}
                      onClick={() => setColumns((cs) => cs.filter((_, j) => j !== i))}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
            <Button
              size="sm"
              variant="outline"
              onClick={() => setColumns((cs) => [...cs, { ...EMPTY_COLUMN }])}
            >
              <Plus className="size-3.5" />
              Add column
            </Button>
          </div>

          {valid && (
            <div className="space-y-1">
              <Label className="text-xs text-muted-foreground">Statement</Label>
              <Well className="max-h-32 font-mono text-[11px]">
                {previewCreate(schema, table, columns)}
              </Well>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!valid || busy}>
            {busy && <Spinner />}
            Create table
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * A type field that offers the engine's own types but still accepts anything
 * typed in. The list is the fast path; the free text is what keeps an unusual
 * but legitimate type from being unreachable, and the server validates it
 * either way.
 */
function TypePicker({
  types,
  value,
  onChange,
}: {
  types: string[]
  value: string
  onChange: (v: string) => void
}) {
  return (
    <div className="flex min-w-0 items-center gap-1">
      <Input
        placeholder="type"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-8 min-w-0 font-mono text-xs"
      />
      {types.length > 0 && (
        <Select value="" onValueChange={onChange}>
          <SelectTrigger size="sm" className="h-8 w-8 shrink-0 px-0 [&>svg]:mx-auto">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {types.map((t) => (
              <SelectItem key={t} value={t} className="font-mono text-xs">
                {t}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    </div>
  )
}

function previewCreate(schema: string, table: string, columns: DbNewColumn[]) {
  const rel = schema ? `${schema}.${table}` : table
  const pks = columns.filter((c) => c.primaryKey && c.name)
  const lines = columns
    .filter((c) => c.name && c.type)
    .map((c) => {
      let l = `  ${c.name} ${c.type}`
      if (c.default) l += ` DEFAULT ${c.default}`
      if (c.notNull) l += " NOT NULL"
      if (pks.length === 1 && c.primaryKey) l += " PRIMARY KEY"
      return l
    })
  if (pks.length > 1) lines.push(`  PRIMARY KEY (${pks.map((c) => c.name).join(", ")})`)
  return `CREATE TABLE ${rel} (\n${lines.join(",\n")}\n)`
}

export function AddColumnDialog({
  open,
  onOpenChange,
  connId,
  schema,
  table,
  info,
  onDone,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  connId: number
  schema: string
  table: string
  info?: DbDriverInfo
  onDone: () => void
}) {
  const [col, setCol] = useState<DbNewColumn>({ ...EMPTY_COLUMN })
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    try {
      const res = await post<{ statement: string }>(`/databases/${connId}/ddl/column`, {
        schema,
        table,
        column: col,
      })
      toast.success(`Added ${col.name}`, { description: res.statement })
      onOpenChange(false)
      setCol({ ...EMPTY_COLUMN })
      onDone()
    } catch (err) {
      toast.error("Could not add the column", { description: errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add column to {table}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="space-y-1.5">
            <Label>Name</Label>
            <Input
              value={col.name}
              onChange={(e) => setCol({ ...col, name: e.target.value })}
              className="font-mono text-xs"
              autoFocus
            />
          </div>
          <div className="space-y-1.5">
            <Label>Type</Label>
            <TypePicker
              types={info?.columnTypes ?? []}
              value={col.type}
              onChange={(v) => setCol({ ...col, type: v })}
            />
          </div>
          <div className="space-y-1.5">
            <Label>Default</Label>
            <Input
              value={col.default ?? ""}
              onChange={(e) => setCol({ ...col, default: e.target.value })}
              className="font-mono text-xs"
              placeholder="literal, e.g. 0 or 'draft'"
            />
          </div>
          <label className="flex items-center gap-2 text-xs">
            <Checkbox
              checked={col.notNull}
              onCheckedChange={(v) => setCol({ ...col, notNull: Boolean(v) })}
            />
            Required (NOT NULL)
          </label>
          {col.notNull && !col.default && (
            <p className="text-xs text-destructive">
              A required column added to a table with existing rows needs a default, or every
              existing row would violate it.
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!col.name || !col.type || busy}>
            {busy && <Spinner />}
            Add column
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function CreateIndexDialog({
  open,
  onOpenChange,
  connId,
  schema,
  table,
  detail,
  onDone,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  connId: number
  schema: string
  table: string
  detail?: DbTableDetail | null
  onDone: () => void
}) {
  const [name, setName] = useState("")
  const [fields, setFields] = useState<string[]>([])
  const [unique, setUnique] = useState(false)
  const [busy, setBusy] = useState(false)

  const toggle = (col: string) =>
    setFields((f) => (f.includes(col) ? f.filter((c) => c !== col) : [...f, col]))

  const submit = async () => {
    setBusy(true)
    try {
      const res = await post<{ statement: string }>(`/databases/${connId}/ddl/index`, {
        schema,
        table,
        name,
        fields,
        unique,
      })
      toast.success(`Created ${name}`, { description: res.statement })
      onOpenChange(false)
      setName("")
      setFields([])
      onDone()
    } catch (err) {
      toast.error("Could not create the index", { description: errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create index on {table}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="space-y-1.5">
            <Label>Index name</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="font-mono text-xs"
              placeholder={`idx_${table}_…`}
              autoFocus
            />
          </div>
          <div className="space-y-1.5">
            <Label>Columns — order matters</Label>
            <div className="max-h-48 space-y-0.5 overflow-y-auto rounded-md border border-hairline p-1.5">
              {detail?.columns.map((c) => (
                <label
                  key={c.name}
                  className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-xs hover:bg-accent"
                >
                  <Checkbox
                    checked={fields.includes(c.name)}
                    onCheckedChange={() => toggle(c.name)}
                  />
                  <span className="font-mono">{c.name}</span>
                  <span className="text-muted-foreground">{c.type.toLowerCase()}</span>
                  {fields.includes(c.name) && (
                    <span className="ml-auto text-[10px] text-primary">
                      #{fields.indexOf(c.name) + 1}
                    </span>
                  )}
                </label>
              ))}
            </div>
          </div>
          <label className="flex items-center gap-2 text-xs">
            <Checkbox checked={unique} onCheckedChange={(v) => setUnique(Boolean(v))} />
            Unique
          </label>
          <p className="text-xs text-muted-foreground">
            Building an index can lock or rewrite a large table. On a busy server, do it when you
            can afford the write.
          </p>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!name || fields.length === 0 || busy}>
            {busy && <Spinner />}
            Create index
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function RenameDialog({
  open,
  onOpenChange,
  connId,
  schema,
  table,
  kind,
  current,
  onDone,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  connId: number
  schema: string
  table: string
  kind: "table" | "column"
  current: string
  // Handed the new name: a caller holding a selection has to follow the
  // rename, or it goes on asking the server for a table that no longer exists.
  onDone: (to: string) => void
}) {
  const [to, setTo] = useState(current)
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    try {
      await post(`/databases/${connId}/ddl/rename`, {
        schema,
        table,
        kind,
        name: kind === "column" ? current : "",
        to,
      })
      toast.success(`Renamed to ${to}`)
      onOpenChange(false)
      onDone(to)
    } catch (err) {
      toast.error("Could not rename", { description: errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Rename {kind}</DialogTitle>
        </DialogHeader>
        <div className="space-y-1.5">
          <Label>New name for {current}</Label>
          <Input
            value={to}
            onChange={(e) => setTo(e.target.value)}
            className="font-mono text-xs"
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!to || to === current || busy}>
            {busy && <Spinner />}
            Rename
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
