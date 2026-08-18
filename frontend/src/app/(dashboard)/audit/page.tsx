"use client"

import { useState } from "react"
import { FileText, Search } from "lucide-react"
import { get } from "@/lib/api"
import { relativeTime, timestamp } from "@/lib/format"
import type { AuditEntry } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { Card, CardContent } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

const PAGE_SIZE = 100

export default function AuditPage() {
  const [username, setUsername] = useState("")
  const [action, setAction] = useState("")
  const [onlyFailed, setOnlyFailed] = useState(false)
  const [offset, setOffset] = useState(0)

  const { data, error, loading } = usePoll(
    (signal) =>
      get<{ entries: AuditEntry[]; total: number }>(
        "/audit/",
        { username, action, failed: onlyFailed, limit: PAGE_SIZE, offset },
        signal,
      ),
    15000,
    [username, action, onlyFailed, offset],
  )

  return (
    <>
      <PageHeader
        title="Audit log"
        description="Every state-changing request, with who made it and from where"
      />

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative">
          <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={username}
            onChange={(e) => {
              setUsername(e.target.value)
              setOffset(0)
            }}
            placeholder="User"
            className="w-40 pl-8"
          />
        </div>
        <Input
          value={action}
          onChange={(e) => {
            setAction(e.target.value)
            setOffset(0)
          }}
          placeholder="Action, e.g. docker.container"
          className="w-64"
        />
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <Checkbox
            checked={onlyFailed}
            onCheckedChange={(v) => {
              setOnlyFailed(v === true)
              setOffset(0)
            }}
          />
          Failures only
        </label>
        <span className="flex-1" />
        <span className="text-sm text-muted-foreground">
          {data ? `${offset + 1}–${Math.min(offset + PAGE_SIZE, data.total)} of ${data.total}` : ""}
        </span>
        <Button
          size="sm"
          variant="outline"
          disabled={offset === 0}
          onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
        >
          Previous
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={!data || offset + PAGE_SIZE >= data.total}
          onClick={() => setOffset((o) => o + PAGE_SIZE)}
        >
          Next
        </Button>
      </div>

      {loading && !data && <LoadingRows rows={8} />}
      {error && <ErrorState error={error} />}

      {data && (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-44">When</TableHead>
                  <TableHead>Who</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Target</TableHead>
                  <TableHead className="w-20">Result</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.entries.map((entry) => (
                  <TableRow
                    key={entry.id}
                    className={entry.success ? undefined : "bg-destructive/5"}
                  >
                    <TableCell className="text-xs">
                      <div>{timestamp(entry.ts)}</div>
                      <p className="text-[11px] text-muted-foreground">{relativeTime(entry.ts)}</p>
                    </TableCell>
                    <TableCell>
                      <div className="text-sm">{entry.username || <em>anonymous</em>}</div>
                      <p className="font-mono text-[11px] text-muted-foreground">
                        {entry.ip} · {entry.actor}
                      </p>
                    </TableCell>
                    <TableCell>
                      <div className="font-mono text-xs">{entry.action}</div>
                      <p className="font-mono text-[11px] text-muted-foreground">
                        {entry.method} {entry.path}
                      </p>
                    </TableCell>
                    <TableCell className="max-w-xs">
                      <div className="truncate font-mono text-xs">{entry.target}</div>
                      {entry.detail && (
                        <p
                          className="truncate text-[11px] text-muted-foreground"
                          title={entry.detail}
                        >
                          {entry.detail}
                        </p>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge variant={entry.success ? "secondary" : "destructive"}>
                        {entry.status}
                      </Badge>
                    </TableCell>
                  </TableRow>
                ))}
                {data.entries.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={5} className="p-0">
                      <EmptyState icon={FileText} title="No entries match" />
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </>
  )
}
