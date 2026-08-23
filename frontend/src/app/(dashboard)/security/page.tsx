"use client"

import { useState } from "react"
import {
  Ban,
  Cable,
  History,
  Radar,
  Network,
  Shield,
  ShieldAlert,
  ShieldCheck,
  Siren,
  TerminalSquare,
  Users,
} from "lucide-react"
import { toast } from "sonner"
import { get, post, ApiError } from "@/lib/api"
import { timestamp } from "@/lib/format"
import type {
  BanEvent,
  Exposure,
  Fail2banJail,
  FirewallStatus,
  LoginRecord,
  LoginSession,
  Posture,
  SecurityFinding,
} from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { ConnectionsPanel } from "@/components/security/connections-panel"
import { FirewallPanel } from "@/components/security/firewall-panel"
import { JailPanel } from "@/components/security/jail-panel"
import { NetworkPanel } from "@/components/security/network-panel"
import { OffendersPanel } from "@/components/security/offenders-panel"
import { PosturePanel } from "@/components/security/posture-panel"
import { SSHPanel } from "@/components/security/ssh-panel"
import { ToolsPanel } from "@/components/security/tools-panel"
import { Badge } from "@/components/ui/badge"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export default function SecurityPage() {
  const { confirm, dialog } = useConfirm()
  const posture = usePoll<Posture>(
    (signal) => get("/security/posture", undefined, signal),
    120000,
  )
  const firewall = usePoll<FirewallStatus>(
    (signal) => get("/firewall/", undefined, signal),
    20000,
  )

  /**
   * A finding's one-click remedy.
   *
   * The whole difference between a warning and a fix. The server names the
   * action; the page maps it to the request that carries it out, and to the
   * confirmation it deserves — the two that can cost access to the machine
   * both go through the typed phrase rather than round it.
   */
  const applyFix = async (finding: SecurityFinding) => {
    const fix = finding.fix ?? ""
    if (fix === "firewall.enable") {
      confirm({
        title: "Enable firewall",
        phrase: "enable firewall",
        confirmLabel: "Enable",
        description: (
          <p className="text-destructive">
            ufw applies its default-deny policy immediately. If the port this dashboard listens on
            is not already allowed, you will lose access.
          </p>
        ),
        action: async (c) => {
          await post("/firewall/enabled", { enabled: true }, { confirm: c })
          firewall.refresh()
          posture.refresh()
        },
      })
      return
    }
    if (fix.startsWith("ssh.")) {
      const [key, value] = fix.slice(4).split("=")
      confirm({
        title: finding.fixLabel ?? "Apply SSH change",
        phrase: "change ssh",
        confirmLabel: "Test and apply",
        description: (
          <div className="space-y-2">
            <p>
              Sets <code className="font-mono">{key}</code> to{" "}
              <code className="font-mono">{value}</code>, tests it with sshd&rsquo;s own parser and
              puts the file back if the test fails.
            </p>
            <p className="text-destructive">
              Keep this session open and confirm you can still log in from a second terminal before
              closing it.
            </p>
          </div>
        ),
        action: async (c) => {
          await post("/ssh/config", { settings: { [key]: value } }, { confirm: c })
          toast.success("Applied")
          posture.refresh()
        },
      })
      return
    }
    toast.info("Fix this from the tab below", { description: finding.advice })
  }

  return (
    <Page>
      <PageHeader
        eyebrow="Network"
        title="Security"
        description="Exposure, firewall, SSH, intrusion prevention and who is connected"
      />
      <ExposurePanel />
      <PosturePanel posture={posture.data} loading={posture.loading} onFix={applyFix} />

      <Tabs defaultValue="firewall" className="min-w-0 gap-4">
        <TabsList>
          <TabsTrigger value="firewall">
            <Shield className="size-3.5" />
            Firewall
          </TabsTrigger>
          <TabsTrigger value="ssh">
            <TerminalSquare className="size-3.5" />
            SSH
          </TabsTrigger>
          <TabsTrigger value="fail2ban">
            <Siren className="size-3.5" />
            Intrusion
          </TabsTrigger>
          <TabsTrigger value="connections">
            <Network className="size-3.5" />
            Connections
          </TabsTrigger>
          <TabsTrigger value="sessions">
            <Users className="size-3.5" />
            Logins
          </TabsTrigger>
          <TabsTrigger value="network">
            <Cable className="size-3.5" />
            Network
          </TabsTrigger>
          <TabsTrigger value="tools">
            <Radar className="size-3.5" />
            Tools
          </TabsTrigger>
        </TabsList>

        <TabsContent value="firewall" className="min-w-0">
          <FirewallPanel
            status={firewall.data}
            posture={posture.data}
            loading={firewall.loading}
            error={firewall.error}
            refresh={() => {
              firewall.refresh()
              posture.refresh()
            }}
          />
        </TabsContent>
        <TabsContent value="ssh" className="min-w-0">
          <SSHPanel />
        </TabsContent>
        <TabsContent value="fail2ban" className="min-w-0">
          <Fail2banTab posture={posture.data} />
        </TabsContent>
        <TabsContent value="connections" className="min-w-0">
          <ConnectionsPanel />
        </TabsContent>
        <TabsContent value="sessions" className="min-w-0">
          <SessionsTab />
        </TabsContent>
        <TabsContent value="network" className="min-w-0">
          <NetworkPanel />
        </TabsContent>
        <TabsContent value="tools" className="min-w-0">
          <ToolsPanel />
        </TabsContent>
      </Tabs>
      {dialog}
    </Page>
  )
}

/**
 * How this dashboard is reachable, above everything else on the page.
 *
 * It is the security property the whole product rests on, and it lives in an
 * env file nobody opens again after install day. On screen it stays true: a
 * machine that quietly became reachable from the internet says so here instead
 * of waiting to be discovered.
 */
function ExposurePanel() {
  const { data } = usePoll<Exposure>((signal) => get("/exposure", undefined, signal), 60_000)
  if (!data) return null

  const safe = data.grade === "tailscale" || data.grade === "private" || data.grade === "tunnel"
  const alarming = data.grade === "open"

  const label: Record<Exposure["grade"], string> = {
    tailscale: "Tailscale only",
    tunnel: "SSH tunnel only",
    private: "Private network",
    public: "Public addresses",
    open: "Open to the internet",
  }

  return (
    <Notice
      tone={safe ? "success" : alarming ? "danger" : "warning"}
      icon={safe ? ShieldCheck : ShieldAlert}
      title={
        <span className="flex flex-wrap items-center gap-2">
          Reachable from
          <Badge
            variant={alarming ? "destructive" : safe ? "success" : "warning"}
            className="font-normal"
          >
            {label[data.grade]}
          </Badge>
        </span>
      }
    >
      <div className="space-y-1.5">
        <p>{data.summary}</p>
        <p className="flex flex-wrap items-center gap-1.5">
          <span className="eyebrow">allowlist</span>
          {data.allowlist.map((cidr) => (
            <code
              key={cidr}
              className="rounded border border-hairline bg-surface-sunken px-1.5 py-0.5 font-mono text-[11px]"
            >
              {cidr}
            </code>
          ))}
        </p>
        {data.recommendation && (
          <p className="font-medium text-foreground">{data.recommendation}</p>
        )}
      </div>
    </Notice>
  )
}

function Fail2banTab({ posture }: { posture: Posture | undefined }) {
  const { can } = useAuth()
  const { data, error, loading, refresh } = usePoll(
    (signal) =>
      get<{ available: boolean; running: boolean; jails: Fail2banJail[]; error?: string }>(
        "/fail2ban/",
        undefined,
        signal,
      ),
    20000,
  )

  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />
  if (!data?.available) {
    return (
      <div className="space-y-4">
        <EmptyState
          icon={Ban}
          title="fail2ban is not installed"
          description="It turns an endless brute-force against a port that has to stay open into a few attempts and a ban, which is the one thing a firewall cannot do for SSH."
        />
      </div>
    )
  }
  if (!data.running) {
    return (
      <EmptyState
        icon={Ban}
        title="fail2ban is installed but not responding"
        description={data.error ?? "Installed and stopped is the state that looks protected and is not."}
      />
    )
  }

  return (
    <div className="space-y-4">
      {/* The findings for this area, above the jails that fix them. */}
      {posture?.findings.some((f) => f.area === "intrusion") && (
        <div className="space-y-2">
          {posture.findings
            .filter((f) => f.area === "intrusion")
            .map((f) => (
              <Notice
                key={f.id}
                tone={f.level === "critical" ? "danger" : f.level === "warning" ? "warning" : "default"}
                icon={Siren}
                title={f.title}
              >
                <p>{f.detail}</p>
                {f.advice && <p className="mt-1 text-foreground/80">{f.advice}</p>}
              </Notice>
            ))}
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        {data.jails.map((jail) => (
          <JailPanel
            key={jail.name}
            jail={jail}
            canManage={can("system.admin")}
            onChanged={refresh}
          />
        ))}
        {data.jails.length === 0 && (
          <EmptyState
            icon={Ban}
            title="No jails configured"
            description="A running fail2ban with no jails bans nobody. Enable at least the sshd jail."
          />
        )}
      </div>

      <OffendersPanel onBlocked={refresh} />

      {/* Under the current bans, what fail2ban has actually been doing. A ban
          expires, so a jail's list is empty again by morning however busy the
          night was — and "nothing currently banned" reads as "nothing
          happened". */}
      <BanHistoryPanel />
    </div>
  )
}

/**
 * What fail2ban has done recently, read from its own log.
 *
 * Not remembered by the dashboard and not inferred by polling the jail: a ban
 * shorter than the polling interval would never be seen that way, and the
 * events either side of a restart would be invented. fail2ban writes this
 * itself, and the page simply reads it.
 */
function BanHistoryPanel() {
  const { data, error, loading } = usePoll(
    (signal) => get<BanEvent[]>("/fail2ban/history", { limit: 100 }, signal),
    60000,
  )

  const unavailable = error instanceof ApiError && error.code === "fail2ban_unavailable"

  return (
    <Panel>
      <PanelHeader
        icon={History}
        title="Ban activity"
        description="From fail2ban's own log — including bans that have since expired"
      />
      <PanelBody flush>
        {unavailable ? (
          <Notice tone="default" title="No fail2ban log on this host">
            fail2ban is not installed, or it logs only to the journal. There is no file to read
            back.
          </Notice>
        ) : error ? (
          <ErrorState error={error} />
        ) : loading ? (
          <LoadingPanel />
        ) : !data?.length ? (
          <EmptyState icon={History} title="No ban activity recorded" />
        ) : (
          <Table containerClassName="max-h-[24rem]">
            <TableHeader>
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>Jail</TableHead>
                <TableHead className="w-full">Address</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((event, i) => (
                <TableRow key={`${event.at}-${event.ip}-${i}`}>
                  <TableCell className="text-xs whitespace-nowrap text-muted-foreground">
                    {timestamp(event.at)}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={event.action === "ban" ? "destructive" : "secondary"}
                      className="font-normal"
                    >
                      {event.action}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-[13px]">{event.jail}</TableCell>
                  <TableCell className="font-mono text-xs">{event.ip}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </PanelBody>
    </Panel>
  )
}

function SessionsTab() {
  return (
    <div className="space-y-4">
      <CurrentSessions />
      {/* The record, under the snapshot. "Who is signed in right now" is the
          one moment nobody worries about; the question that matters on an
          exposed host is who got in overnight, and the host has been keeping
          that answer in wtmp all along. */}
      <LoginHistoryPanel />
    </div>
  )
}

function CurrentSessions() {
  const { data, error, loading } = usePoll(
    (signal) => get<LoginSession[]>("/ssh-sessions", undefined, signal),
    10000,
  )
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />
  if (!data?.length) return <EmptyState icon={Users} title="No interactive logins" />

  return (
    <Panel>
      <PanelHeader
        icon={Users}
        title="Interactive logins"
        description={`${data.length} session${data.length === 1 ? "" : "s"} on this host right now`}
      />
      <PanelBody flush>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>User</TableHead>
              <TableHead>Terminal</TableHead>
              <TableHead className="w-full">From</TableHead>
              <TableHead>Logged in</TableHead>
              <TableHead>Idle</TableHead>
              <TableHead>Type</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.map((session, i) => (
              <TableRow key={`${session.user}-${session.tty}-${i}`}>
                <TableCell className="text-[13px] font-medium">{session.user}</TableCell>
                <TableCell className="font-mono text-xs">{session.tty}</TableCell>
                <TableCell className="font-mono text-xs">{session.from || "local"}</TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {session.loginTime ? timestamp(session.loginTime) : "—"}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {session.idle ?? "—"}
                </TableCell>
                <TableCell>
                  <Badge variant={session.isSsh ? "outline" : "secondary"} className="font-normal">
                    {session.isSsh ? "ssh" : "local"}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </PanelBody>
    </Panel>
  )
}

/**
 * Recent logins, read from the host rather than remembered by the dashboard.
 *
 * Failed attempts are a separate request behind system.admin: btmp records
 * whatever was typed at a login prompt, and what people type at a login prompt
 * is sometimes their password in the username field.
 */
function LoginHistoryPanel() {
  const { can } = useAuth()
  const [failed, setFailed] = useState(false)
  const admin = can("system.admin")
  const showFailed = failed && admin

  const { data, error, loading } = usePoll(
    (signal) =>
      get<LoginRecord[]>(showFailed ? "/logins/failed" : "/logins", { limit: 100 }, signal),
    60000,
    [showFailed],
  )

  const unavailable = error instanceof ApiError && error.code === "login_history_unavailable"

  return (
    <Panel>
      <PanelHeader
        icon={History}
        title={showFailed ? "Failed login attempts" : "Recent logins"}
        description={
          showFailed
            ? "From the host's btmp record — every attempt that did not get in"
            : "From the host's wtmp record, including restarts"
        }
        actions={
          admin && (
            <ToggleGroup
              type="single"
              value={showFailed ? "failed" : "ok"}
              onValueChange={(next) => setFailed(next === "failed")}
              variant="outline"
              size="sm"
              aria-label="Which logins to show"
            >
              <ToggleGroupItem value="ok" className="px-2.5 text-[11px]">
                Successful
              </ToggleGroupItem>
              <ToggleGroupItem value="failed" className="px-2.5 text-[11px]">
                Failed
              </ToggleGroupItem>
            </ToggleGroup>
          )
        }
      />
      <PanelBody flush>
        {unavailable ? (
          <Notice tone="default" title="No login record on this host">
            wtmp is not being written here, so there is nothing to read back. That is normal on a
            minimal image, and it means logins leave no trace on this machine at all.
          </Notice>
        ) : error ? (
          <ErrorState error={error} />
        ) : loading ? (
          <LoadingPanel />
        ) : !data?.length ? (
          <EmptyState
            icon={History}
            title={showFailed ? "No failed attempts recorded" : "No logins recorded"}
          />
        ) : (
          <Table containerClassName="max-h-[28rem]">
            <TableHeader>
              <TableRow>
                <TableHead>User</TableHead>
                <TableHead>Terminal</TableHead>
                <TableHead className="w-full">From</TableHead>
                <TableHead>When</TableHead>
                <TableHead>Lasted</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((record, i) => (
                <TableRow key={`${record.user}-${record.loginTime ?? i}-${i}`}>
                  <TableCell className="text-[13px] font-medium">
                    <span className="flex items-center gap-2">
                      {record.user}
                      {record.kind !== "login" && (
                        <Badge variant="secondary" className="font-normal">
                          {record.kind === "boot" ? "boot" : "shutdown"}
                        </Badge>
                      )}
                    </span>
                  </TableCell>
                  <TableCell className="font-mono text-xs">{record.tty || "—"}</TableCell>
                  <TableCell className="font-mono text-xs">{record.from || "local"}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {record.loginTime ? timestamp(record.loginTime) : "—"}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {record.active ? (
                      <Badge variant="success" className="font-normal">
                        still open
                      </Badge>
                    ) : (
                      (record.duration ?? record.ended ?? "—")
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </PanelBody>
    </Panel>
  )
}
