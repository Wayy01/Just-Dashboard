"use client"

import { useState } from "react"
import { Ban, Plus, Shield, ShieldAlert, Trash2, Users } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import { timestamp } from "@/lib/format"
import type { Fail2banJail, FirewallStatus, LoginSession } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { IconAction } from "@/components/icon-action"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
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
    <>
      <PageHeader title="Security" description="Firewall, intrusion prevention and active logins" />
      <Tabs defaultValue="firewall">
        <TabsList>
          <TabsTrigger value="firewall">Firewall</TabsTrigger>
          <TabsTrigger value="fail2ban">fail2ban</TabsTrigger>
          <TabsTrigger value="sessions">SSH sessions</TabsTrigger>
        </TabsList>
        <TabsContent value="firewall">
          <FirewallTab />
        </TabsContent>
        <TabsContent value="fail2ban">
          <Fail2banTab />
        </TabsContent>
        <TabsContent value="sessions">
          <SessionsTab />
        </TabsContent>
      </Tabs>
    </>
  )
}

function FirewallTab() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<FirewallStatus>("/firewall/", undefined, signal),
    20000,
  )

  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />
  if (!data?.available) {
    return <EmptyState icon={Shield} title="No firewall tool found" description={data?.error} />
  }

  return (
    <>
      <div className="space-y-4">
        <Alert>
          <ShieldAlert className="size-4" />
          <AlertTitle>Lockout protection</AlertTitle>
          <AlertDescription>
            A rule that would block the address you are connected from is refused before it is
            applied — a firewall change should never be the thing that costs you access to the box.
          </AlertDescription>
        </Alert>

        <Card>
          <CardHeader className="flex flex-row items-start justify-between">
            <div>
              <CardTitle className="text-base">
                {data.backend} · {data.enabled ? "active" : "inactive"}
              </CardTitle>
              <CardDescription>
                {data.defaultPolicy ?? "no default policy reported"}
              </CardDescription>
            </div>
            <div className="flex items-center gap-2">
              {can("system.admin") && data.backend === "ufw" && (
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
              )}
            </div>
          </CardHeader>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-12">#</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>To</TableHead>
                  <TableHead>From</TableHead>
                  <TableHead>Comment</TableHead>
                  <TableHead className="w-px" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.rules.map((rule, i) => (
                  <TableRow key={`${rule.number}-${i}`}>
                    <TableCell className="font-mono text-xs tabular-nums">{rule.number}</TableCell>
                    <TableCell>
                      <Badge variant={rule.action === "ALLOW" ? "default" : "destructive"}>
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
                          className="text-destructive"
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
                          <Trash2 className="size-3.5" />
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
          </CardContent>
        </Card>
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
                <SelectTrigger>
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
                <SelectTrigger>
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

  if (loading) return <LoadingRows />
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
    <div className="grid items-start gap-4 lg:grid-cols-2 [&>*]:min-w-0">
      {data.jails.map((jail) => (
        <Card key={jail.name}>
          <CardHeader>
            <CardTitle className="text-base">{jail.name}</CardTitle>
            <CardDescription>
              {jail.currentlyBanned} banned now · {jail.totalBanned} total · {jail.currentlyFailed}{" "}
              failing
            </CardDescription>
          </CardHeader>
          <CardContent>
            {jail.bannedIps.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nothing currently banned.</p>
            ) : (
              <div className="space-y-1">
                {jail.bannedIps.map((ip) => (
                  <div
                    key={ip}
                    className="flex items-center justify-between gap-2 rounded-md px-2 py-1 hover:bg-accent"
                  >
                    <span className="font-mono text-xs">{ip}</span>
                    {can("system.admin") && (
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-6 px-2 text-xs"
                        onClick={() => unban(jail.name, ip)}
                      >
                        Unban
                      </Button>
                    )}
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
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
  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />
  if (!data?.length) return <EmptyState icon={Users} title="No interactive logins" />

  return (
    <Card>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>User</TableHead>
              <TableHead>Terminal</TableHead>
              <TableHead>From</TableHead>
              <TableHead>Logged in</TableHead>
              <TableHead>Idle</TableHead>
              <TableHead>Type</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.map((session, i) => (
              <TableRow key={`${session.user}-${session.tty}-${i}`}>
                <TableCell className="font-medium">{session.user}</TableCell>
                <TableCell className="font-mono text-xs">{session.tty}</TableCell>
                <TableCell className="font-mono text-xs">{session.from || "local"}</TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {session.loginTime ? timestamp(session.loginTime) : "—"}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {session.idle ?? "—"}
                </TableCell>
                <TableCell>
                  <Badge variant={session.isSsh ? "default" : "secondary"}>
                    {session.isSsh ? "ssh" : "local"}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
