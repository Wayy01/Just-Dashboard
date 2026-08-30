"use client"

import { useState } from "react"
import {
  DesktopDevice,
  Key,
  LockClosed,
  Plus,
  ShieldCheck,
  Trash,
  UserSettings,
} from "@/components/icons"
import { notify } from "@/lib/toast"
import { del, get, patch, post } from "@/lib/api"
import { relativeTime, timestamp } from "@/lib/format"
import type { ApiToken, DashboardUser, Role, SessionInfo } from "@/lib/types"
import { useViewState } from "@/lib/view-state"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { Panel, PanelBody, PanelFooter, PanelHeader, Well } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel, Notice, Spinner } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"

export default function AccountPage() {
  const [tab, setTab] = useViewState("account.tab", "security")
  const { status, can } = useAuth()

  return (
    <Page>
      <PageHeader
        eyebrow="You"
        title="Account"
        description={
          <span className="flex flex-wrap items-center gap-2">
            <span>{status?.user?.username}</span>
            <Badge variant="outline" className="font-normal capitalize">
              {status?.user?.role}
            </Badge>
            <Badge
              variant={status?.user?.totpEnabled ? "success" : "warning"}
              className="font-normal"
            >
              {status?.user?.totpEnabled ? "2FA enabled" : "2FA not enrolled"}
            </Badge>
          </span>
        }
      />
      <Tabs value={tab} onValueChange={setTab} className="min-w-0 gap-4">
        <TabsList>
          <TabsTrigger value="security">Security</TabsTrigger>
          <TabsTrigger value="sessions">Sessions</TabsTrigger>
          <TabsTrigger value="tokens">API tokens</TabsTrigger>
          {can("system.admin") && <TabsTrigger value="users">Dashboard users</TabsTrigger>}
        </TabsList>
        <TabsContent value="security" className="min-w-0">
          <SecurityTab />
        </TabsContent>
        <TabsContent value="sessions" className="min-w-0">
          <SessionsTab />
        </TabsContent>
        <TabsContent value="tokens" className="min-w-0">
          <TokensTab />
        </TabsContent>
        {can("system.admin") && (
          <TabsContent value="users" className="min-w-0">
            <UsersTab />
          </TabsContent>
        )}
      </Tabs>
    </Page>
  )
}

function SecurityTab() {
  const { status, logout } = useAuth()
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirmPw, setConfirmPw] = useState("")
  const [busy, setBusy] = useState(false)
  const [codes, setCodes] = useState<string[] | null>(null)

  const change = async () => {
    if (next !== confirmPw) {
      notify.error("The new passwords do not match")
      return
    }
    setBusy(true)
    try {
      await post("/account/password", { currentPassword: current, newPassword: next })
      notify.success("Password changed", { description: "All sessions were signed out." })
      // The server drops every session on a password change, so the only
      // correct next step is back to the login screen.
      await logout()
    } catch (err) {
      notify.error("Could not change password", err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
      <Panel>
        <PanelHeader
          icon={LockClosed}
          title="Change password"
          description="At least 12 characters, mixing three character classes"
        />
        <PanelBody className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="cur-pw">Current password</Label>
            <Input
              id="cur-pw"
              type="password"
              autoComplete="current-password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="new-pw">New password</Label>
            <Input
              id="new-pw"
              type="password"
              autoComplete="new-password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="conf-pw">Confirm new password</Label>
            <Input
              id="conf-pw"
              type="password"
              autoComplete="new-password"
              value={confirmPw}
              onChange={(e) => setConfirmPw(e.target.value)}
            />
          </div>
          <p className="text-xs leading-relaxed text-muted-foreground">
            Changing it signs out every session, including this one.
          </p>
        </PanelBody>
        <PanelFooter>
          <Button size="sm" onClick={change} disabled={busy || !current || !next}>
            {busy && <Spinner className="size-4" />}
            Change password
          </Button>
        </PanelFooter>
      </Panel>

      <Panel>
        <PanelHeader
          icon={ShieldCheck}
          title="Two-factor authentication"
          description="A code from your authenticator app, checked at every sign in"
          actions={
            <Badge
              variant={status?.user?.totpEnabled ? "success" : "secondary"}
              className="font-normal"
            >
              {status?.user?.totpEnabled ? "enabled" : "not enrolled"}
            </Badge>
          }
        />
        <PanelBody className="space-y-3">
          {codes ? (
            <>
              <Notice tone="warning" icon={Key} title="New recovery codes">
                The previous set no longer works. These are shown only now.
              </Notice>
              <Well className="grid grid-cols-2 gap-x-4 gap-y-1.5">
                {codes.map((c) => (
                  <span key={c} className="tracking-wider">
                    {c}
                  </span>
                ))}
              </Well>
            </>
          ) : (
            <p className="text-xs leading-relaxed text-muted-foreground">
              Recovery codes are your way back in if you lose the authenticator. Regenerating issues
              a fresh set and invalidates the old one immediately.
            </p>
          )}
        </PanelBody>
        <PanelFooter>
          <Button
            size="sm"
            variant="outline"
            onClick={async () => {
              try {
                const res = await post<{ recoveryCodes: string[] }>("/account/recovery-codes")
                setCodes(res.recoveryCodes)
                notify.success("Recovery codes regenerated", {
                  description: "The previous set no longer works.",
                })
              } catch (err) {
                notify.error("Could not regenerate", err)
              }
            }}
          >
            Regenerate recovery codes
          </Button>
        </PanelFooter>
      </Panel>
    </div>
  )
}

function SessionsTab() {
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<SessionInfo[]>("/account/sessions", undefined, signal),
    20000,
  )
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  return (
    <Panel>
      <PanelHeader
        icon={DesktopDevice}
        title="Active sessions"
        description="Signing one out takes effect immediately"
      />
      <PanelBody flush>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Address</TableHead>
              <TableHead className="w-full">Client</TableHead>
              <TableHead>Started</TableHead>
              <TableHead>Last seen</TableHead>
              <TableHead className="w-px" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((session) => (
              <TableRow key={session.id} className="group">
                <TableCell className="font-mono text-xs">
                  {session.ip}
                  {session.current && (
                    <Badge variant="success" className="ml-2 text-[10px] font-normal">
                      this session
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="max-w-xs truncate text-xs text-muted-foreground">
                  {session.userAgent}
                </TableCell>
                <TableCell className="text-xs">{timestamp(session.createdAt)}</TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {relativeTime(session.lastSeenAt)}
                </TableCell>
                <TableCell>
                  {!session.current && (
                    <Button
                      size="xs"
                      variant="ghost"
                      className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                      onClick={async () => {
                        await del(`/account/sessions/${session.id}`)
                        notify.success("Session revoked")
                        refresh()
                      }}
                    >
                      Revoke
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
            {data?.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="p-0">
                  <EmptyState icon={DesktopDevice} title="No sessions" />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </PanelBody>
    </Panel>
  )
}

function TokensTab() {
  const { confirm, dialog } = useConfirm()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<ApiToken[]>("/tokens/", undefined, signal),
    30000,
  )

  return (
    <>
      <Panel>
        <PanelHeader
          icon={Key}
          title="API tokens"
          description="A token can never exceed the role of the account that minted it, and is demoted automatically if that account is"
          actions={<CreateTokenDialog onDone={refresh} />}
        />
        <PanelBody flush>
          {loading && <LoadingPanel rows={3} />}
          {error && <ErrorState error={error} className="m-4" />}
          {data && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-full">Name</TableHead>
                  <TableHead>Prefix</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Last used</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="w-px" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.map((token) => (
                  <TableRow key={token.id} className={token.revoked ? "opacity-50" : undefined}>
                    <TableCell className="text-[13px] font-medium">{token.name}</TableCell>
                    <TableCell className="font-mono text-xs">{token.prefix}…</TableCell>
                    <TableCell>
                      <Badge variant="outline" className="font-normal">
                        {token.role}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {token.lastUsedAt ? relativeTime(token.lastUsedAt) : "never"}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {token.expiresAt ? relativeTime(token.expiresAt) : "never"}
                    </TableCell>
                    <TableCell>
                      {token.revoked ? (
                        <Badge variant="secondary" className="font-normal">
                          revoked
                        </Badge>
                      ) : (
                        <Button
                          size="icon-xs"
                          variant="ghost"
                          aria-label={`Revoke ${token.name}`}
                          className="text-destructive"
                          onClick={() =>
                            confirm({
                              title: "Revoke token",
                              confirmLabel: "Revoke",
                              description: (
                                <p>
                                  Anything using <b>{token.name}</b> stops working immediately.
                                </p>
                              ),
                              action: async (c) => {
                                await del(`/tokens/${token.id}`, { confirm: c })
                                refresh()
                              },
                            })
                          }
                        >
                          <Trash />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
                {data.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={6} className="p-0">
                      <EmptyState
                        icon={Key}
                        title="No tokens"
                        description="Mint one to script against this dashboard from CI or a cron job."
                      />
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </PanelBody>
      </Panel>
      {dialog}
    </>
  )
}

function CreateTokenDialog({ onDone }: { onDone: () => void }) {
  const { status } = useAuth()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [role, setRole] = useState<Role>("readonly")
  const [ttlDays, setTtlDays] = useState(90)
  const [secret, setSecret] = useState<string | null>(null)

  const create = async () => {
    try {
      const res = await post<{ secret: string }>("/tokens/", { name, role, ttlDays })
      setSecret(res.secret)
      onDone()
    } catch (err) {
      notify.error("Could not create token", err)
    }
  }

  // A token may narrow the owner's role but never widen it; the server
  // enforces this too, so the picker only avoids a pointless round trip.
  const allowed: Role[] =
    status?.user?.role === "admin"
      ? ["admin", "limited", "readonly"]
      : status?.user?.role === "limited"
        ? ["limited", "readonly"]
        : ["readonly"]

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (!o) {
          setSecret(null)
          setName("")
        }
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus className="size-4" />
          New token
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>New API token</DialogTitle>
          <DialogDescription>
            Send it as <code className="font-mono">Authorization: Bearer …</code>
          </DialogDescription>
        </DialogHeader>

        {secret ? (
          <Notice tone="warning" icon={Key} title="Copy it now — it is not shown again">
            <code className="font-mono text-xs break-all">{secret}</code>
          </Notice>
        ) : (
          <div className="grid gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="tok-name">Name</Label>
              <Input
                id="tok-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="ci-deploy"
              />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>Role</Label>
                <Select value={role} onValueChange={(v) => setRole(v as Role)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {allowed.map((r) => (
                      <SelectItem key={r} value={r}>
                        {r}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="tok-ttl">Expires in (days)</Label>
                <Input
                  id="tok-ttl"
                  type="number"
                  min={0}
                  value={ttlDays}
                  onChange={(e) => setTtlDays(Number(e.target.value))}
                />
              </div>
            </div>
          </div>
        )}

        <DialogFooter>
          {secret ? (
            <Button onClick={() => setOpen(false)}>Done</Button>
          ) : (
            <Button onClick={create} disabled={!name}>
              Create
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function UsersTab() {
  const { status } = useAuth()
  const { confirm, dialog } = useConfirm()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<DashboardUser[]>("/dashboard-users/", undefined, signal),
    30000,
  )

  const update = async (user: DashboardUser, body: Record<string, unknown>) => {
    try {
      await patch(`/dashboard-users/${user.id}`, body)
      notify.success(`${user.username} updated`)
      refresh()
    } catch (err) {
      notify.error("Could not update", err)
    }
  }

  return (
    <>
      <Panel>
        <PanelHeader
          icon={UserSettings}
          title="Dashboard users"
          description="Separate from the host's own Linux accounts"
          actions={<CreateDashboardUserDialog onDone={refresh} />}
        />
        <PanelBody flush>
          {loading && <LoadingPanel rows={3} />}
          {error && <ErrorState error={error} className="m-4" />}
          {data && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-full">User</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>2FA</TableHead>
                  <TableHead>Last login</TableHead>
                  <TableHead className="w-24">Enabled</TableHead>
                  <TableHead className="w-px" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.map((user) => (
                  <TableRow key={user.id} className="group">
                    <TableCell className="text-[13px] font-medium">
                      {user.username}
                      {user.id === status?.user?.id && (
                        <Badge variant="outline" className="ml-2 text-[10px] font-normal">
                          you
                        </Badge>
                      )}
                      {user.mustChangePassword && (
                        <Badge variant="warning" className="ml-2 text-[10px] font-normal">
                          must change password
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <Select value={user.role} onValueChange={(v) => update(user, { role: v })}>
                        <SelectTrigger size="sm" className="w-28 text-xs">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="admin">admin</SelectItem>
                          <SelectItem value="limited">limited</SelectItem>
                          <SelectItem value="readonly">readonly</SelectItem>
                        </SelectContent>
                      </Select>
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={user.totpEnabled ? "success" : "secondary"}
                        className="font-normal"
                      >
                        {user.totpEnabled ? "enrolled" : "pending"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {user.lastLoginAt.startsWith("0001")
                        ? "never"
                        : relativeTime(user.lastLoginAt)}
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={!user.disabled}
                        onCheckedChange={(v) => update(user, { disabled: !v })}
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
                        <Button
                          size="xs"
                          variant="ghost"
                          title="Clear the 2FA enrollment so this user can re-enroll"
                          onClick={async () => {
                            await post(`/dashboard-users/${user.id}/reset-totp`)
                            notify.success(`2FA reset for ${user.username}`)
                            refresh()
                          }}
                        >
                          Reset 2FA
                        </Button>
                        <Button
                          size="icon-xs"
                          variant="ghost"
                          aria-label={`Delete ${user.username}`}
                          className="text-destructive"
                          onClick={() =>
                            confirm({
                              title: "Delete dashboard user",
                              phrase: user.username,
                              confirmLabel: "Delete",
                              description: (
                                <p>
                                  <b>{user.username}</b> loses access immediately, along with every
                                  session and API token they hold.
                                </p>
                              ),
                              action: async (c) => {
                                await del(`/dashboard-users/${user.id}`, { confirm: c })
                                refresh()
                              },
                            })
                          }
                        >
                          <Trash />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </PanelBody>
      </Panel>
      {dialog}
    </>
  )
}

function CreateDashboardUserDialog({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false)
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [role, setRole] = useState<Role>("readonly")

  const create = async () => {
    try {
      await post("/dashboard-users/", { username, password, role })
      notify.success(`Created ${username}`, {
        description: "They must change this password and enroll 2FA at first sign in.",
      })
      setOpen(false)
      setUsername("")
      setPassword("")
      onDone()
    } catch (err) {
      notify.error("Could not create user", err)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <UserSettings className="size-4" />
          New user
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New dashboard user</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="du-name">Username</Label>
            <Input id="du-name" value={username} onChange={(e) => setUsername(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="du-pw">Initial password</Label>
            <Input
              id="du-pw"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              At least 12 characters, mixing three character classes.
            </p>
          </div>
          <div className="space-y-1.5">
            <Label>Role</Label>
            <Select value={role} onValueChange={(v) => setRole(v as Role)}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="admin">admin — everything</SelectItem>
                <SelectItem value="limited">limited — start/stop and edit files</SelectItem>
                <SelectItem value="readonly">readonly — look but do not touch</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button onClick={create} disabled={!username || !password}>
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
