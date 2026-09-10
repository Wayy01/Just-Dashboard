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
  const { logout } = useAuth()
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirmPw, setConfirmPw] = useState("")
  const [busy, setBusy] = useState(false)

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

      <TwoFactorPanel />
    </div>
  )
}

/**
 * Two-factor, as something you turn on rather than something you are handed.
 *
 * It used to be neither: enrolment happened once, during a sign-in nobody
 * could get past without it, and this panel existed only to reissue recovery
 * codes. Now that an install can leave it optional, the account page is where
 * enrolling and un-enrolling actually belong — and the panel has to make the
 * state obvious, because "am I protected by this" is the whole question.
 *
 * The three states are: not enrolled, enrolled, and enrolled on an install
 * that requires it — where the off switch is absent rather than disabled,
 * since a control that cannot be used is a question the operator has to answer
 * for themselves.
 */
function TwoFactorPanel() {
  const { status, refresh } = useAuth()
  const [enrollment, setEnrollment] = useState<{ secret: string; otpauthUrl: string } | null>(null)
  const [code, setCode] = useState("")
  const [codes, setCodes] = useState<string[] | null>(null)
  const [password, setPassword] = useState("")
  const [disabling, setDisabling] = useState(false)
  const [busy, setBusy] = useState(false)

  const enrolled = Boolean(status?.user?.totpEnabled)
  const required = Boolean(status?.require2fa)

  const begin = async () => {
    setBusy(true)
    try {
      setEnrollment(await post<{ secret: string; otpauthUrl: string }>("/auth/2fa/setup"))
    } catch (err) {
      notify.error("Could not start enrolment", err)
    } finally {
      setBusy(false)
    }
  }

  const enable = async () => {
    setBusy(true)
    try {
      const res = await post<{ recoveryCodes: string[] }>("/auth/2fa/enable", { code })
      setCodes(res.recoveryCodes)
      setEnrollment(null)
      setCode("")
      await refresh().catch(() => undefined)
      notify.success("Two-factor enabled", {
        description: "You will be asked for a code the next time you sign in.",
      })
    } catch (err) {
      notify.error("Code rejected", err)
    } finally {
      setBusy(false)
    }
  }

  const disable = async () => {
    setBusy(true)
    try {
      await post("/account/2fa/disable", { password })
      setPassword("")
      setDisabling(false)
      setCodes(null)
      await refresh().catch(() => undefined)
      notify.success("Two-factor turned off", {
        description: "Your password is now the only thing between a session and this server.",
      })
    } catch (err) {
      notify.error("Could not turn it off", err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Panel>
      <PanelHeader
        icon={ShieldCheck}
        title="Two-factor authentication"
        description="A code from your authenticator app, checked at every sign in"
        actions={
          <Badge variant={enrolled ? "success" : "secondary"} className="font-normal">
            {enrolled ? "enabled" : required ? "required — not yet enrolled" : "not enrolled"}
          </Badge>
        }
      />
      <PanelBody className="space-y-3">
        {codes ? (
          <>
            <Notice tone="warning" icon={Key} title="Recovery codes">
              Each one works once, in place of your authenticator. Any previous set no longer works,
              and this is the only time these are shown.
            </Notice>
            <Well className="grid grid-cols-2 gap-x-4 gap-y-1.5">
              {codes.map((c) => (
                <span key={c} className="tracking-wider">
                  {c}
                </span>
              ))}
            </Well>
          </>
        ) : enrolled ? (
          <p className="text-xs leading-relaxed text-muted-foreground">
            Your account asks for a code at every sign in. Recovery codes are the way back in if you
            lose the authenticator; regenerating issues a fresh set and invalidates the old one
            immediately.
          </p>
        ) : enrollment ? (
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label>Secret</Label>
              <Well className="font-mono text-[13px] tracking-widest break-all">
                {enrollment.secret}
              </Well>
              <p className="text-xs text-muted-foreground">
                Add it to your authenticator, or{" "}
                <a href={enrollment.otpauthUrl} className="underline underline-offset-4">
                  open it directly
                </a>
                . The seed is sealed with the dashboard&apos;s master key and shown only here.
              </p>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="enrol-code">Code from your app</Label>
              <Input
                id="enrol-code"
                inputMode="numeric"
                maxLength={6}
                placeholder="000000"
                className="h-11 max-w-48 text-center font-mono text-lg tracking-[0.4em]"
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
            </div>
          </div>
        ) : disabling ? (
          <div className="space-y-3">
            <Notice tone="warning" title="Your password becomes the only factor">
              This dashboard is root-equivalent. A session left open on an unlocked laptop is
              exactly what the second factor answers, which is why turning it off costs a password.
            </Notice>
            <div className="space-y-1.5">
              <Label htmlFor="disable-pw">Current password</Label>
              <Input
                id="disable-pw"
                type="password"
                autoComplete="current-password"
                className="max-w-72"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
          </div>
        ) : (
          <p className="text-xs leading-relaxed text-muted-foreground">
            {required
              ? "This install requires an authenticator. Enrol one now — until you do, your session can reach nothing but this page."
              : "Not required on this install, and worth having anyway: a password alone is one stolen credential away from root on this server."}
          </p>
        )}
      </PanelBody>
      <PanelFooter className="gap-2">
        {enrolled && !codes && (
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={async () => {
              setBusy(true)
              try {
                const res = await post<{ recoveryCodes: string[] }>("/account/recovery-codes")
                setCodes(res.recoveryCodes)
                notify.success("Recovery codes regenerated", {
                  description: "The previous set no longer works.",
                })
              } catch (err) {
                notify.error("Could not regenerate", err)
              } finally {
                setBusy(false)
              }
            }}
          >
            Regenerate recovery codes
          </Button>
        )}

        {enrolled && !required && !disabling && (
          <Button size="sm" variant="ghost" onClick={() => setDisabling(true)}>
            Turn off
          </Button>
        )}

        {disabling && (
          <>
            <Button size="sm" variant="destructive" disabled={busy || !password} onClick={disable}>
              {busy && <Spinner className="size-4" />}
              Turn two-factor off
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setDisabling(false)
                setPassword("")
              }}
            >
              Cancel
            </Button>
          </>
        )}

        {!enrolled &&
          (enrollment ? (
            <>
              <Button size="sm" disabled={busy || code.length < 6} onClick={enable}>
                {busy && <Spinner className="size-4" />}
                Enable two-factor
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setEnrollment(null)
                  setCode("")
                }}
              >
                Cancel
              </Button>
            </>
          ) : (
            <Button size="sm" disabled={busy} onClick={begin}>
              {busy && <Spinner className="size-4" />}
              Enable two-factor
            </Button>
          ))}

        {codes && (
          <Button size="sm" variant="outline" onClick={() => setCodes(null)}>
            I have saved them
          </Button>
        )}
      </PanelFooter>
    </Panel>
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
