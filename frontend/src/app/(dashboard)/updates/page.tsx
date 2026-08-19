"use client"

import { useMemo, useState } from "react"
import { PackageCheck, RefreshCw, RotateCcw, ShieldAlert } from "lucide-react"
import { toast } from "sonner"
import { get, post } from "@/lib/api"
import { relativeTime } from "@/lib/format"
import type { UpdateReport } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader, SearchInput } from "@/components/page"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { StatTile } from "@/components/stat-tile"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
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

  const report = usePoll((signal) => get<UpdateReport>("/updates/", undefined, signal), 300000)

  const visible = useMemo(() => {
    const list = report.data?.packages ?? []
    const needle = filter.trim().toLowerCase()
    if (!needle) return list
    return list.filter(
      (p) =>
        p.name.toLowerCase().includes(needle) || (p.origin ?? "").toLowerCase().includes(needle),
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
    <Page>
      {dialog}
      <PageHeader
        eyebrow="Operations"
        title="Updates"
        description="Operating system packages this server is missing"
        actions={
          <>
            {can("destructive") && data?.available && data.packages.length > 0 && (
              <>
                {data.securityCount > 0 && (
                  <Button size="sm" disabled={applying} onClick={() => apply(true)}>
                    <ShieldAlert className="size-4" />
                    Install {data.securityCount} security update
                    {data.securityCount === 1 ? "" : "s"}
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  disabled={applying}
                  onClick={() => apply(false)}
                >
                  <PackageCheck className="size-4" />
                  Upgrade all {data.packages.length}
                </Button>
              </>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={() => report.refresh()}
              disabled={applying}
            >
              <RefreshCw className="size-4" />
              Re-check
            </Button>
          </>
        }
      />

      {report.error && <ErrorState error={report.error} />}
      {report.loading && !data && (
        <>
          <div className="grid gap-4 sm:grid-cols-3 [&>*]:min-w-0">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-[7.5rem] rounded-xl" />
            ))}
          </div>
          <LoadingPanel />
        </>
      )}

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
            <Notice tone="warning" icon={RotateCcw} title="This server needs a reboot">
              An installed update cannot take effect until the machine restarts
              {data.rebootPackages?.length ? `: ${data.rebootPackages.slice(0, 6).join(", ")}` : ""}
              . Reboot from the Terminal when it suits you — the dashboard will not do it for you.
            </Notice>
          )}

          {data.error && (
            <Notice icon={ShieldAlert} title="Could not read the package database">
              <span className="font-mono text-xs">{data.error}</span>
            </Notice>
          )}

          <div className="grid gap-4 sm:grid-cols-3 [&>*]:min-w-0">
            <StatTile
              label="Updates available"
              value={data.packages.length}
              hint={`checked ${relativeTime(data.lastChecked)}`}
              icon={PackageCheck}
            />
            <StatTile
              label="Security updates"
              value={data.securityCount}
              hint={data.securityCount ? "apply these first" : "none outstanding"}
              icon={ShieldAlert}
              tone={data.securityCount > 0 ? "warning" : "success"}
            />
            <StatTile
              label="Reboot"
              value={data.rebootRequired ? "required" : "not needed"}
              hint={data.manager ?? ""}
              icon={RotateCcw}
              tone={data.rebootRequired ? "warning" : "default"}
            />
          </div>

          {data.packages.length === 0 ? (
            <EmptyState
              icon={PackageCheck}
              title="Everything is up to date"
              description="No packages are waiting to be upgraded."
            />
          ) : (
            <Panel>
              <PanelHeader
                icon={PackageCheck}
                title="Pending packages"
                description={`${visible.length} shown of ${data.packages.length}`}
              />
              <PanelToolbar>
                <SearchInput
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="Filter by package or origin"
                />
              </PanelToolbar>
              <PanelBody flush>
                <Table containerClassName="max-h-[calc(100svh-28rem)]">
                  <TableHeader className={stickyTableHeader}>
                    <TableRow>
                      <TableHead>Package</TableHead>
                      <TableHead>Installed</TableHead>
                      <TableHead>Available</TableHead>
                      <TableHead className="w-full">Origin</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {visible.map((p) => (
                      <TableRow key={p.name}>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <span className="text-[13px] font-medium">{p.name}</span>
                            {p.security && (
                              <Badge variant="destructive" className="font-normal">
                                security
                              </Badge>
                            )}
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
              </PanelBody>
            </Panel>
          )}
        </>
      )}
    </Page>
  )
}
