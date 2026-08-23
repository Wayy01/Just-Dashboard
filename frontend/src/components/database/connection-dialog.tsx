"use client"

import { useState } from "react"
import { CheckCircle2, Plug, XCircle } from "lucide-react"
import { toast } from "sonner"
import { post, put } from "@/lib/api"
import type { DbConnection, DbDriver } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Spinner } from "@/components/state"
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

const PLACEHOLDERS: Record<DbDriver, string> = {
  postgres: "postgres://user:password@127.0.0.1:5432/dbname?sslmode=disable",
  mysql: "user:password@tcp(127.0.0.1:3306)/dbname",
  mongodb: "mongodb://user:password@127.0.0.1:27017/dbname",
  sqlite: "/var/lib/myapp/data.db",
}

const DRIVER_LABELS: Record<DbDriver, string> = {
  postgres: "PostgreSQL",
  mysql: "MySQL / MariaDB",
  mongodb: "MongoDB",
  sqlite: "SQLite (file path)",
}

/**
 * Add or edit a connection, with a Test button that dials the DSN before it is
 * saved and reports the server version back — the one piece of feedback that
 * turns "did I get the host right" from a save-and-pray into an answer. On edit
 * the driver is fixed (changing it would strand the stored secret) and an empty
 * DSN keeps the existing one, so a rename never needs an unreadable password
 * re-typed.
 */
export function ConnectionDialog({
  open,
  onOpenChange,
  onDone,
  existing,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onDone: () => void
  existing?: DbConnection
}) {
  const editing = Boolean(existing)
  const [name, setName] = useState(existing?.name ?? "")
  const [driver, setDriver] = useState<DbDriver>(existing?.driver ?? "postgres")
  const [dsn, setDsn] = useState("")
  const [testResult, setTestResult] = useState<{ ok: boolean; version?: string; error?: string } | null>(
    null,
  )
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)

  const test = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const res = await post<{ ok: boolean; version?: string; error?: string }>("/databases/test", {
        driver,
        dsn,
      })
      setTestResult(res)
    } catch (err) {
      setTestResult({ ok: false, error: String(err) })
    } finally {
      setTesting(false)
    }
  }

  const save = async () => {
    setSaving(true)
    try {
      if (editing) {
        await put(`/databases/${existing!.id}`, { name, dsn })
        toast.success(`Updated ${name}`)
      } else {
        await post("/databases/", { name, driver, dsn })
        toast.success(`Added ${name}`)
      }
      onOpenChange(false)
      onDone()
    } catch (err) {
      toast.error(editing ? "Could not update connection" : "Could not add connection", {
        description: String(err),
      })
    } finally {
      setSaving(false)
    }
  }

  const canSave = name !== "" && (editing || dsn !== "")

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? "Edit connection" : "Add database connection"}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="conn-name">Name</Label>
            <Input id="conn-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label>Driver</Label>
            <Select value={driver} onValueChange={(v) => setDriver(v as DbDriver)} disabled={editing}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(DRIVER_LABELS) as DbDriver[]).map((d) => (
                  <SelectItem key={d} value={d}>
                    {DRIVER_LABELS[d]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {editing && (
              <p className="text-xs text-muted-foreground">
                The driver is fixed once a connection exists.
              </p>
            )}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="conn-dsn">
              {driver === "sqlite" ? "Database file path" : "Connection string"}
              {editing && <span className="ml-1.5 text-muted-foreground">(leave blank to keep)</span>}
            </Label>
            <Input
              id="conn-dsn"
              value={dsn}
              onChange={(e) => {
                setDsn(e.target.value)
                setTestResult(null)
              }}
              className="font-mono text-xs"
              placeholder={PLACEHOLDERS[driver]}
            />
            <p className="text-xs text-muted-foreground">
              Encrypted with the dashboard&apos;s master key and never returned to a browser.
            </p>
          </div>

          {testResult && (
            <div
              className={
                "flex items-start gap-2 rounded-md border p-2.5 text-xs " +
                (testResult.ok
                  ? "border-success/30 bg-success/10 text-success"
                  : "border-destructive/30 bg-destructive/10 text-destructive")
              }
            >
              {testResult.ok ? (
                <CheckCircle2 className="mt-0.5 size-4 shrink-0" />
              ) : (
                <XCircle className="mt-0.5 size-4 shrink-0" />
              )}
              <span className="min-w-0 break-words">
                {testResult.ok
                  ? testResult.version
                    ? `Connected — ${testResult.version}`
                    : "Connected"
                  : testResult.error}
              </span>
            </div>
          )}
        </div>
        <DialogFooter className="sm:justify-between">
          <Button variant="outline" onClick={test} disabled={testing || dsn === ""}>
            {testing ? <Spinner /> : <Plug className="size-4" />}
            Test connection
          </Button>
          <Button onClick={save} disabled={!canSave || saving}>
            {saving && <Spinner />}
            {editing ? "Save" : "Add"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
