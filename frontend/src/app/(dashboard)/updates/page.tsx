"use client"

import { useMemo, useState } from "react"
import { PackageCheck, RefreshCw, RotateCcw, Search, ShieldAlert } from "lucide-react"
import { toast } from "sonner"
import { get, post } from "@/lib/api"
import { relativeTime } from "@/lib/format"
import type { UpdateReport } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card, CardContent } from "@/components/ui/card"
import { StatCard } from "@/components/stat-card"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export default function UpdatesPage() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [filter, setFilter] = useState("")
  const [applying, setApplying] = useState(false)

  const report = usePoll(
    (signal) => get<UpdateReport>("/updates/", undefined, signal),
    300000,
  )

  const visible = useMemo(() => {
    const list = report.data?.packages ?? []
    const needle = filter.trim().toLowerCase()
    if (!needle) return list
    return list.filter(
      (p) => p.name.toLowerCase().includes(needle) || (p.origin ?? "").toLowerCase().includes(needle),
    )
  }, [report.data, filter])

  const apply = (securityOnly: boolean) =>
    confirm({
      title: securityOnly ? "Install security updates" : "Upgrade all packages",
      phrase: securityOnly ? "install security updates" : "upgrade packages",
      confirmLabel: securityOnly ? "Install" : "Upgrade",
      description: (
        <>
          <p>
            Runs <b>apt-get upgrade</b> on the host
            {securityOnly ? " restricted to the security pocket" : ""}. Services whose packages
            change are restarted.
          </p>
          <p className="text-muted-foreground">
            New packages are never installed and nothing is removed. This can take several minutes.
          </p>
        </>
      ),
      action: async (c) => {
        setApplying(true)
        try {
          const res = await post<{ output: string }>("/updates/apply", undefined, {
            confirm: c,
            query: { security: securityOnly },
          })
          const tail = res.output.trim().split("\n").slice(-3).join("\n")
          toast.success("Updates applied", { description: tail })
          report.refresh()
        } finally {
          setApplying(false)
        }
      },
    })

  const data = report.data

  return (
    <>
      {dialog}
      <PageHeader
        title="Updates"
        description="Operating system packages this server is missing"
        actions={
          <Button variant="outline" size="sm" onClick={() => report.refresh()} disabled={applying}>
            <RefreshCw className="size-4" />
            Re-check
          </Button>
        }
      />

      {report.error && <ErrorState error={report.error} />}
      {report.loading && !data && <LoadingRows />}

      {data && !data.available && (
        <EmptyState
          icon={PackageCheck}
          title="No supported package manager"
          description="This page reports apt packages; the host does not appear to use apt."
        />
      )}

      {data?.available && (
        <>
          {data.rebootRequired && (
            <Alert variant="destructive">
              <RotateCcw className="size-4" />
              <AlertTitle>This server needs a reboot</AlertTitle>
              <AlertDescription>
                An installed update cannot take effect until the machine restarts
                {data.rebootPackages?.length
                  ? `: ${data.rebootPackages.slice(0, 6).join(", ")}`
                  : ""}
                . Reboot from the Terminal when it suits you — the dashboard will not do it for you.
              </AlertDescription>
            </Alert>
          )}

          {data.error && (
            <Alert>
              <ShieldAlert className="size-4" />
              <AlertTitle>Could not read the package database</AlertTitle>
              <AlertDescription className="font-mono text-xs">{data.error}</AlertDescription>
            </Alert>
          )}

          <div className="grid gap-4 sm:grid-cols-3 [&>*]:min-w-0">
            <StatCard
              title="Updates available"
              value={data.packages.length}
              detail={`checked ${relativeTime(data.lastChecked)}`}
              icon={PackageCheck}
            />
            <StatCard
              title="Security updates"
              value={data.securityCount}
              detail={data.securityCount ? "apply these first" : "none outstanding"}
              icon={ShieldAlert}
              tone={data.securityCount > 0 ? "warning" : "default"}
            />
            <StatCard
              title="Reboot"
              value={data.rebootRequired ? "required" : "not needed"}
              detail={data.manager ?? ""}
              icon={RotateCcw}
              tone={data.rebootRequired ? "warning" : "default"}
            />
          </div>

          {can("destructive") && data.packages.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {data.securityCount > 0 && (
                <Button size="sm" disabled={applying} onClick={() => apply(true)}>
                  <ShieldAlert className="size-4" />
                  Install {data.securityCount} security update
                  {data.securityCount === 1 ? "" : "s"}
                </Button>
              )}
              <Button size="sm" variant="outline" disabled={applying} onClick={() => apply(false)}>
                <PackageCheck className="size-4" />
                Upgrade all {data.packages.length}
              </Button>
            </div>
          )}

          {data.packages.length === 0 ? (
            <EmptyState
              icon={PackageCheck}
              title="Everything is up to date"
              description="No packages are waiting to be upgraded."
            />
          ) : (
            <>
              <div className="relative max-w-sm">
                <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="Filter by package or origin"
                  className="pl-8"
                />
              </div>
              <Card>
                <CardContent className="p-0">
                  <Table containerClassName="max-h-[calc(100svh-30rem)]">
                    <TableHeader className={stickyTableHeader}>
                      <TableRow>
                        <TableHead>Package</TableHead>
                        <TableHead>Installed</TableHead>
                        <TableHead>Available</TableHead>
                        <TableHead>Origin</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {visible.map((p) => (
                        <TableRow key={p.name}>
                          <TableCell>
                            <div className="flex items-center gap-2">
                              <span className="font-medium">{p.name}</span>
                              {p.security && <Badge variant="destructive">security</Badge>}
                            </div>
                          </TableCell>
                          <TableCell className="font-mono text-xs text-muted-foreground">
                            {p.current || "—"}
                          </TableCell>
                          <TableCell className="font-mono text-xs">{p.candidate}</TableCell>
                          <TableCell>
                            <div className="max-w-[18rem] min-w-0">
                              <p
                                className="truncate font-mono text-[11px] text-muted-foreground"
                                title={p.origin}
                              >
                                {p.origin || "—"}
                              </p>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
            </>
          )}
        </>
      )}
    </>
  )
}
