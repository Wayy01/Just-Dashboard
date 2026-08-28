"use client"

import { useMemo, useState } from "react"
import { ArrowUpCircle, PackageCheck, RefreshCw, RotateCcw, ShieldAlert, Sparkles } from "lucide-react"
import { get, post } from "@/lib/api"
import { relativeTime } from "@/lib/format"
import type { Job, UpdateReport } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useSelfUpdate } from "@/hooks/use-self-update"
import { useConfirm } from "@/components/confirm-dialog"
import { JobConsole, RecentJobs, useJobConsole } from "@/components/job-console"
import { DashboardUpdatePanel } from "@/components/update/update-panel"
import { Page, PageHeader, Section, SearchInput } from "@/components/page"
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
  const self = useSelfUpdate()

  // The package list is only right once an upgrade has finished — refresh on
  // the running → succeeded edge from the console itself, not from an effect
  // watching its status.
  const console_ = useJobConsole({ onSuccess: () => report.refresh() })

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
            Runs the host&rsquo;s package manager
            {securityOnly ? ", restricted to the security pocket" : ""}. Services whose packages
            change are restarted.
          </p>
          <p className="text-muted-foreground">
            New packages are never installed and nothing is removed. The output appears below as it
            happens, and the upgrade keeps running if you close this page.
          </p>
        </>
      ),
      action: async (c) => {
        setApplying(true)
        try {
          const job = await post<Job>("/updates/apply", undefined, {
            confirm: c,
            query: { security: securityOnly },
          })
          console_.attach(job)
        } finally {
          setApplying(false)
        }
      },
    })

  const data = report.data
  const canApply = can("destructive") && data?.available && data.packages.length > 0
  const dashboardBehind = self.report?.available ?? false
  const dashboardValue = dashboardBehind
    ? `→ ${self.report?.latest}`
    : self.report
      ? "current"
      : "—"

  return (
    <Page>
      {dialog}
      <PageHeader
        eyebrow="Operations"
        title="Updates"
        description="Everything on this machine that has a newer version — the dashboard, and the host's packages"
        actions={<RecentJobs kinds={["updates."]} onOpen={console_.open} />}
      />

      {/* The one-glance answer to "what is behind", before either detail
          block — the dashboard and the OS are one question. */}
      <div className="grid gap-4 sm:grid-cols-2 [&>*]:min-w-0">
        <StatTile
          label="Dashboard"
          icon={Sparkles}
          value={dashboardValue}
          tone={dashboardBehind ? "warning" : self.report ? "success" : "default"}
          hint={
            self.report
              ? `version ${self.report.version}${
                  self.report.releases.length > 1
                    ? ` · ${self.report.releases.length} releases behind`
                    : ""
                }`
              : "checking…"
          }
        />
        <StatTile
          label="OS packages"
          icon={PackageCheck}
          value={data?.available ? data.packages.length : data ? "n/a" : "—"}
          tone={
            !data?.available
              ? "default"
              : data.securityCount > 0
                ? "warning"
                : data.packages.length > 0
                  ? "default"
                  : "success"
          }
          hint={
            data?.available
              ? data.securityCount > 0
                ? `${data.securityCount} security · checked ${relativeTime(data.lastChecked)}`
                : `${data.manager} · checked ${relativeTime(data.lastChecked)}`
              : "no supported package manager"
          }
        />
      </div>

      {/* The decision. Security updates are the reason to be on this page, and
          the button to act is here rather than three tiles down. */}
      {canApply && data.securityFiltering && data.securityCount > 0 && (
        <Panel>
          <PanelHeader
            icon={ShieldAlert}
            title={`${data.securityCount} security update${data.securityCount === 1 ? "" : "s"} outstanding`}
            description="Apply these first — the full upgrade can wait for a quieter moment"
            actions={
              <Button size="sm" disabled={applying} onClick={() => apply(true)}>
                <ShieldAlert className="size-4" />
                Install security updates
              </Button>
            }
          />
        </Panel>
      )}

      {data?.rebootRequired && (
        <Notice tone="warning" icon={RotateCcw} title="This server needs a reboot">
          An installed update cannot take effect until the machine restarts
          {data.rebootPackages?.length ? `: ${data.rebootPackages.slice(0, 6).join(", ")}` : ""}.
          Reboot from the Terminal when it suits you — the dashboard will not do it for you.
        </Notice>
      )}

      <Section title="Dashboard">
        <DashboardUpdatePanel />
      </Section>

      <Section
        title="Operating system"
        actions={
          <>
            {canApply && (
              <Button size="sm" variant="outline" disabled={applying} onClick={() => apply(false)}>
                <ArrowUpCircle className="size-4" />
                Upgrade all {data.packages.length}
              </Button>
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
      >
        <JobConsole
          job={console_.job}
          lines={console_.lines}
          onDismiss={console_.dismiss}
          onCancel={console_.cancel}
        />

        {report.error && <ErrorState error={report.error} />}
        {report.loading && !data && (
          <>
            <Skeleton className="h-24 rounded-xl" />
            <LoadingPanel />
          </>
        )}

        {data && !data.available && (
          <EmptyState
            icon={PackageCheck}
            title="No supported package manager"
            description="This host does not appear to use apt, dnf, yum, zypper, pacman or apk."
          />
        )}

        {data?.available && data.error && (
          <Notice icon={ShieldAlert} title="Could not read the package database">
            <span className="font-mono text-xs">{data.error}</span>
          </Notice>
        )}

        {data?.available &&
          (data.packages.length === 0 ? (
            <EmptyState
              icon={PackageCheck}
              title="Everything is up to date"
              description={
                data.securityFiltering
                  ? "No packages are waiting to be upgraded."
                  : `${data.manager} publishes no advisory data, so a clean list here means only that nothing at all is pending.`
              }
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
                              <Badge variant="warning" className="font-normal">
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
                          <p
                            className="max-w-[18rem] truncate font-mono text-[11px] text-muted-foreground"
                            title={p.origin}
                          >
                            {p.origin || "—"}
                          </p>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </PanelBody>
            </Panel>
          ))}
      </Section>
    </Page>
  )
}
