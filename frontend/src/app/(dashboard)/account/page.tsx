"use client"

import { useState } from "react"
import { KeyRound, Loader2, Monitor, Plus, ShieldCheck, Trash2, UserCog } from "lucide-react"
import { toast } from "sonner"
import { del, get, patch, post } from "@/lib/api"
import { relativeTime, timestamp } from "@/lib/format"
import type { ApiToken, DashboardUser, Role, SessionInfo } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"

export default function AccountPage() {
  const { status, can } = useAuth()

  return (
    <>
      <PageHeader
        title="Account"
        description={`${status?.user?.username} · ${status?.user?.role}`}
      />
      <Tabs defaultValue="security">
        <TabsList>
          <TabsTrigger value="security">Security</TabsTrigger>
          <TabsTrigger value="sessions">Sessions</TabsTrigger>
          <TabsTrigger value="tokens">API tokens</TabsTrigger>
          {can("system.admin") && <TabsTrigger value="users">Dashboard users</TabsTrigger>}
        </TabsList>
        <TabsContent value="security">
          <SecurityTab />
        </TabsContent>
        <TabsContent value="sessions">
          <SessionsTab />
        </TabsContent>
        <TabsContent value="tokens">
          <TokensTab />
        </TabsContent>
        {can("system.admin") && (
          <TabsContent value="users">
            <UsersTab />
          </TabsContent>
        )}
      </Tabs>
    </>
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
      toast.error("The new passwords do not match")
      return
    }
    setBusy(true)
    try {
      await post("/account/password", { currentPassword: current, newPassword: next })
      toast.success("Password changed", { description: "All sessions were signed out." })
      // The server drops every session on a password change, so the only
      // correct next step is back to the login screen.
      await logout()
    } catch (err) {
      toast.error("Could not change password", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Change password</CardTitle>
          <CardDescription>
            At least 12 characters, mixing three of: uppercase, lowercase, digits, symbols. Changing
            it signs out every session, including this one.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="cur-pw">Current password</Label>
            <Input
              id="cur-pw"
              type="password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="new-pw">New password</Label>
            <Input
              id="new-pw"
              type="password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="conf-pw">Confirm new password</Label>
            <Input
              id="conf-pw"
              type="password"
              value={confirmPw}
              onChange={(e) => setConfirmPw(e.target.value)}
            />
          </div>
          <Button onClick={change} disabled={busy || !current || !next}>
            {busy && <Loader2 className="size-4 animate-spin" />}
            Change password
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Two-factor authentication</CardTitle>
          <CardDescription>
            {status?.user?.totpEnabled
              ? "Enrolled and required at every sign in."
              : "Not enrolled."}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-center gap-2">
            <ShieldCheck
              className={
                status?.user?.totpEnabled ? "size-5 text-success" : "size-5 text-muted-foreground"
              }
            />
            <Badge variant={status?.user?.totpEnabled ? "default" : "secondary"}>
              {status?.user?.totpEnabled ? "enabled" : "not enrolled"}
            </Badge>
          </div>

          {codes && (
            <Alert>
              <KeyRound className="size-4" />
              <AlertTitle>New recovery codes</AlertTitle>
              <AlertDescription>
                <div className="mt-2 grid grid-cols-2 gap-1 font-mono text-xs">
                  {codes.map((c) => (
                    <span key={c}>{c}</span>
                  ))}
                </div>
              </AlertDescription>
            </Alert>
          )}

          <Button
            variant="outline"
            onClick={async () => {
              try {
                const res = await post<{ recoveryCodes: string[] }>("/account/recovery-codes")
                setCodes(res.recoveryCodes)
                toast.success("Recovery codes regenerated", {
                  description: "The previous set no longer works.",
                })
              } catch (err) {
                toast.error("Could not regenerate", { description: String(err) })
              }
            }}
          >
            Regenerate recovery codes
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}

function SessionsTab() {
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<SessionInfo[]>("/account/sessions", undefined, signal),
    20000,
  )
  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Active sessions</CardTitle>
        <CardDescription>Signing one out takes effect immediately</CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Address</TableHead>
              <TableHead>Client</TableHead>
              <TableHead>Started</TableHead>
              <TableHead>Last seen</TableHead>
              <TableHead className="w-px" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((session) => (
              <TableRow key={session.id}>
                <TableCell className="font-mono text-xs">
                  {session.ip}
                  {session.current && <Badge className="ml-2 text-[10px]">this session</Badge>}
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
                      size="sm"
                      variant="ghost"
                      className="h-7 px-2 text-xs text-destructive"
                      onClick={async () => {
                        await del(`/account/sessions/${session.id}`)
                        toast.success("Session revoked")
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
                  <EmptyState icon={Monitor} title="No sessions" />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
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
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0">
          <div>
            <CardTitle className="text-base">API tokens</CardTitle>
            <CardDescription>
              For scripting. A token can never exceed the role of the account that minted it, and is
              demoted automatically if that account is.
            </CardDescription>
          </div>
          <CreateTokenDialog onDone={refresh} />
        </CardHeader>
        <CardContent className="p-0">
          {loading && <LoadingRows className="p-4" />}
          {error && <ErrorState error={error} className="m-4" />}
          {data && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
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
                    <TableCell className="font-medium">{token.name}</TableCell>
                    <TableCell className="font-mono text-xs">{token.prefix}…</TableCell>
                    <TableCell>
                      <Badge variant="outline">{token.role}</Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {token.lastUsedAt ? relativeTime(token.lastUsedAt) : "never"}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {token.expiresAt ? relativeTime(token.expiresAt) : "never"}
                    </TableCell>
                    <TableCell>
                      {token.revoked ? (
                        <Badge variant="secondary">revoked</Badge>
                      ) : (
                        <Button
                          size="icon"
                          variant="ghost"
                          className="size-7 text-destructive"
                          onClick={() =>
                            confirm({
                              title: "Revoke token",
                              phrase: token.name,
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
                          <Trash2 className="size-3.5" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
                {data.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={6} className="p-0">
                      <EmptyState icon={KeyRound} title="No tokens" />
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
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
      toast.error("Could not create token", { description: String(err) })
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
          <Alert>
            <KeyRound className="size-4" />
            <AlertTitle>Copy it now — it is not shown again</AlertTitle>
            <AlertDescription className="font-mono text-xs break-all">{secret}</AlertDescription>
          </Alert>
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
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label>Role</Label>
                <Select value={role} onValueChange={(v) => setRole(v as Role)}>
                  <SelectTrigger>
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
      toast.success(`${user.username} updated`)
      refresh()
    } catch (err) {
      toast.error("Could not update", { description: String(err) })
    }
  }

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0">
          <div>
            <CardTitle className="text-base">Dashboard users</CardTitle>
            <CardDescription>Separate from the host&apos;s own Linux accounts</CardDescription>
          </div>
          <CreateDashboardUserDialog onDone={refresh} />
        </CardHeader>
        <CardContent className="p-0">
          {loading && <LoadingRows className="p-4" />}
          {error && <ErrorState error={error} className="m-4" />}
          {data && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>User</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>2FA</TableHead>
                  <TableHead>Last login</TableHead>
                  <TableHead className="w-24">Enabled</TableHead>
                  <TableHead className="w-px" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.map((user) => (
                  <TableRow key={user.id}>
                    <TableCell className="font-medium">
                      {user.username}
                      {user.id === status?.user?.id && (
                        <Badge variant="outline" className="ml-2 text-[10px]">
                          you
                        </Badge>
                      )}
                      {user.mustChangePassword && (
                        <Badge variant="destructive" className="ml-2 text-[10px]">
                          must change password
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <Select value={user.role} onValueChange={(v) => update(user, { role: v })}>
                        <SelectTrigger className="h-7 w-28 text-xs">
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
                      <Badge variant={user.totpEnabled ? "default" : "secondary"}>
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
                      <div className="flex gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 px-2 text-xs"
                          title="Clear the 2FA enrollment so this user can re-enroll"
                          onClick={async () => {
                            await post(`/dashboard-users/${user.id}/reset-totp`)
                            toast.success(`2FA reset for ${user.username}`)
                            refresh()
                          }}
                        >
                          Reset 2FA
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          className="size-7 text-destructive"
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
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
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
      toast.success(`Created ${username}`, {
        description: "They must change this password and enroll 2FA at first sign in.",
      })
      setOpen(false)
      setUsername("")
      setPassword("")
      onDone()
    } catch (err) {
      toast.error("Could not create user", { description: String(err) })
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <UserCog className="size-4" />
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
              <SelectTrigger>
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
