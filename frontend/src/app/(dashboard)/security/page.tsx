"use client"

import { useState } from "react"
import { Ban, Plus, Shield, ShieldAlert, ShieldCheck, Trash2, Users } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { timestamp } from "@/lib/format"
import type { Exposure, Fail2banJail, FirewallStatus, LoginSession } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { IconAction } from "@/components/icon-action"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  stickyTableHeader,
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

export default function SecurityPage() {
  return (
    <Page>
      <PageHeader
        eyebrow="Network"
        title="Security"
        description="Firewall, intrusion prevention and active logins"
      />
      <ExposurePanel />
      <Tabs defaultValue="firewall" className="min-w-0 gap-4">
        <TabsList>
          <TabsTrigger value="firewall">Firewall</TabsTrigger>
          <TabsTrigger value="fail2ban">fail2ban</TabsTrigger>
          <TabsTrigger value="sessions">SSH sessions</TabsTrigger>
        </TabsList>
        <TabsContent value="firewall" className="min-w-0">
          <FirewallTab />
        </TabsContent>
        <TabsContent value="fail2ban" className="min-w-0">
          <Fail2banTab />
        </TabsContent>
        <TabsContent value="sessions" className="min-w-0">
          <SessionsTab />
        </TabsContent>
      </Tabs>
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

function FirewallTab() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<FirewallStatus>("/firewall/", undefined, signal),
    20000,
  )

  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />
  if (!data?.available) {
    return <EmptyState icon={Shield} title="No firewall tool found" description={data?.error} />
  }

  return (
    <>
      <div className="flex min-w-0 flex-col gap-4">
        <Notice icon={ShieldAlert} title="Lockout protection">
          A rule that would block the address you are connected from is refused before it is applied
          — a firewall change should never be the thing that costs you access to the box.
        </Notice>

        <Panel>
          <PanelHeader
            icon={Shield}
            title={`${data.backend} · ${data.enabled ? "active" : "inactive"}`}
            description={data.defaultPolicy ?? "no default policy reported"}
            actions={
              can("system.admin") &&
              data.backend === "ufw" && (
                <>
                  <AddRuleDialog onDone={refresh} />
                  <Switch
                    checked={data.enabled}
                    onCheckedChange={(enabled) =>
                      confirm({
                        title: enabled ? "Enable firewall" : "Disable firewall",
                        phrase: enabled ? "enable firewall" : "disable firewall",
                        confirmLabel: enabled ? "Enable" : "Disable",
                        description: enabled ? (
                          <p className="text-destructive">
                            ufw applies its default-deny policy immediately. If the port this
                            dashboard listens on is not already allowed, you will lose access.
                          </p>
                        ) : (
                          <p className="text-destructive">
                            Every rule stops being enforced and the host is left unfiltered.
                          </p>
                        ),
                        action: async (c) => {
                          await post("/firewall/enabled", { enabled }, { confirm: c })
                          refresh()
                        },
                      })
                    }
                  />
                </>
              )
            }
          />
          <PanelBody flush>
            <Table containerClassName="max-h-[calc(100svh-26rem)]">
              <TableHeader className={stickyTableHeader}>
                <TableRow>
                  <TableHead className="w-12">#</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>To</TableHead>
                  <TableHead>From</TableHead>
                  <TableHead className="w-full">Comment</TableHead>
                  <TableHead className="w-px" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.rules.map((rule, i) => (
                  <TableRow key={`${rule.number}-${i}`} className="group">
                    <TableCell className="numeric font-mono text-xs">{rule.number}</TableCell>
                    <TableCell>
                      <Badge
                        variant={rule.action === "ALLOW" ? "success" : "destructive"}
                        className="font-normal"
                      >
                        {rule.action}
                      </Badge>
                      {rule.direction && (
                        <span className="ml-1 text-[11px] text-muted-foreground">
                          {rule.direction}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{rule.to || "—"}</TableCell>
                    <TableCell className="font-mono text-xs">{rule.from || "anywhere"}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">{rule.comment}</TableCell>
                    <TableCell>
                      {can("system.admin") && rule.number !== undefined && (
                        <IconAction
                          label="Delete rule"
                          className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                          onClick={() =>
                            confirm({
                              title: "Delete firewall rule",
                              phrase: `delete rule ${rule.number}`,
                              confirmLabel: "Delete",
                              description: <p className="font-mono text-xs">{rule.raw}</p>,
                              action: async (c) => {
                                await del(`/firewall/rules/${rule.number}`, { confirm: c })
                                refresh()
                              },
                            })
                          }
                        >
                          <Trash2 />
                        </IconAction>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
                {data.rules.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={6} className="p-0">
                      <EmptyState icon={Shield} title="No rules configured" />
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </PanelBody>
        </Panel>
      </div>
      {dialog}
    </>
  )
}

function AddRuleDialog({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false)
  const [action, setAction] = useState("allow")
  const [port, setPort] = useState("")
  const [protocol, setProtocol] = useState("tcp")
  const [from, setFrom] = useState("")
  const [comment, setComment] = useState("")

  const submit = async () => {
    try {
      await post("/firewall/rules", { action, direction: "in", port, protocol, from, comment })
      toast.success("Rule added")
      setOpen(false)
      setPort("")
      setFrom("")
      setComment("")
      onDone()
    } catch (err) {
      toast.error("Rule rejected", { description: String(err) })
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <Plus className="size-4" />
          Add rule
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New inbound rule</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label>Action</Label>
              <Select value={action} onValueChange={setAction}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="allow">allow</SelectItem>
                  <SelectItem value="deny">deny</SelectItem>
                  <SelectItem value="reject">reject</SelectItem>
                  <SelectItem value="limit">limit</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>Protocol</Label>
              <Select value={protocol} onValueChange={setProtocol}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="tcp">tcp</SelectItem>
                  <SelectItem value="udp">udp</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="rule-port">Port or range</Label>
            <Input
              id="rule-port"
              value={port}
              onChange={(e) => setPort(e.target.value)}
              placeholder="443 or 8000:8010"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="rule-from">Source (optional)</Label>
            <Input
              id="rule-from"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
              placeholder="10.0.0.0/8"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="rule-comment">Comment</Label>
            <Input id="rule-comment" value={comment} onChange={(e) => setComment(e.target.value)} />
          </div>
        </div>
        <DialogFooter>
          <Button onClick={submit} disabled={!port}>
            Add rule
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Fail2banTab() {
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
  if (!data?.available) return <EmptyState icon={Ban} title="fail2ban is not installed" />
  if (!data.running) {
    return (
      <EmptyState
        icon={Ban}
        title="fail2ban is installed but not responding"
        description={data.error}
      />
    )
  }

  const unban = async (jail: string, ip: string) => {
    try {
      await post(`/fail2ban/${encodeURIComponent(jail)}/unban`, { ip })
      toast.success(`${ip} unbanned from ${jail}`)
      refresh()
    } catch (err) {
      toast.error("Could not unban", { description: String(err) })
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
      {data.jails.map((jail) => (
        <Panel key={jail.name}>
          <PanelHeader
            icon={Ban}
            title={jail.name}
            description={`${jail.currentlyBanned} banned now · ${jail.totalBanned} total · ${jail.currentlyFailed} failing`}
          />
          <PanelBody>
            {jail.bannedIps.length === 0 ? (
              <p className="text-[13px] text-muted-foreground">Nothing currently banned.</p>
            ) : (
              <div className="space-y-0.5">
                {jail.bannedIps.map((ip) => (
                  <div
                    key={ip}
                    className="flex items-center justify-between gap-2 rounded-md px-2 py-1 hover:bg-[var(--row-hover)]"
                  >
                    <span className="font-mono text-xs">{ip}</span>
                    {can("system.admin") && (
                      <Button size="xs" variant="ghost" onClick={() => unban(jail.name, ip)}>
                        Unban
                      </Button>
                    )}
                  </div>
                ))}
              </div>
            )}
          </PanelBody>
        </Panel>
      ))}
      {data.jails.length === 0 && <EmptyState icon={Ban} title="No jails configured" />}
    </div>
  )
}

function SessionsTab() {
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
