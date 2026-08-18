"use client"

import { useState } from "react"
import { KeyRound, Lock, Plus, Trash2, Unlock, UserPlus, Users } from "lucide-react"
import { toast } from "sonner"
import { del, get, patch, post } from "@/lib/api"
import { relativeTime } from "@/lib/format"
import type { SSHKey, SystemUser } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
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
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

export default function SystemUsersPage() {
  const { confirm, dialog } = useConfirm()
  const [showSystem, setShowSystem] = useState(false)
  const [keysFor, setKeysFor] = useState<string | null>(null)
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<SystemUser[]>("/system-users/", { system: showSystem }, signal),
    30000,
    [showSystem],
  )

  const setLocked = async (user: SystemUser, locked: boolean) => {
    try {
      await patch(`/system-users/${encodeURIComponent(user.username)}`, { locked })
      toast.success(`${user.username} ${locked ? "locked" : "unlocked"}`)
      refresh()
    } catch (err) {
      toast.error("Could not change lock state", { description: String(err) })
    }
  }

  return (
    <>
      <PageHeader
        title="System users"
        description="Operating system accounts on this host, separate from dashboard logins"
        actions={<CreateUserDialog onDone={refresh} />}
      />

      <label className="flex items-center gap-2 text-sm text-muted-foreground">
        <Checkbox checked={showSystem} onCheckedChange={(v) => setShowSystem(v === true)} />
        Include system accounts
      </label>

      {loading && <LoadingRows />}
      {error && <ErrorState error={error} />}

      {data && (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>User</TableHead>
                  <TableHead className="w-20">UID</TableHead>
                  <TableHead>Groups</TableHead>
                  <TableHead>Shell</TableHead>
                  <TableHead>Last login</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead className="w-px" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.map((user) => (
                  <TableRow key={user.username} className="group">
                    <TableCell>
                      <div className="font-medium">{user.username}</div>
                      {user.comment && (
                        <p className="text-xs text-muted-foreground">{user.comment}</p>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-xs tabular-nums">{user.uid}</TableCell>
                    <TableCell className="max-w-48 truncate text-xs text-muted-foreground">
                      {user.groups.join(", ")}
                    </TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">
                      {user.shell}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {user.lastLogin ? relativeTime(user.lastLogin) : "never"}
                    </TableCell>
                    <TableCell className="space-x-1">
                      {user.locked && <Badge variant="secondary">locked</Badge>}
                      {user.noPassword && <Badge variant="destructive">no password</Badge>}
                      {!user.canLogin && <Badge variant="outline">no shell</Badge>}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 gap-1 px-2 text-xs"
                          onClick={() => setKeysFor(user.username)}
                        >
                          <KeyRound className="size-3.5" />
                          {user.sshKeyCount}
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          className="size-7"
                          title={user.locked ? "Unlock" : "Lock"}
                          onClick={() => setLocked(user, !user.locked)}
                        >
                          {user.locked ? <Unlock className="size-3.5" /> : <Lock className="size-3.5" />}
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          className="size-7 text-destructive"
                          title="Delete"
                          onClick={() =>
                            confirm({
                              title: "Delete system user",
                              phrase: user.username,
                              confirmLabel: "Delete",
                              description: (
                                <p className="text-destructive">
                                  Removes the account <b>{user.username}</b> from this host. Its home
                                  directory is left in place.
                                </p>
                              ),
                              action: async (c) => {
                                await del(`/system-users/${encodeURIComponent(user.username)}`, { confirm: c })
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
                {data.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={7} className="p-0">
                      <EmptyState icon={Users} title="No accounts to show" />
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <SSHKeysSheet username={keysFor} onOpenChange={(o) => !o && setKeysFor(null)} onChanged={refresh} />
      {dialog}
    </>
  )
}

function CreateUserDialog({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false)
  const [username, setUsername] = useState("")
  const [comment, setComment] = useState("")
  const [shell, setShell] = useState("/bin/bash")
  const [groups, setGroups] = useState("")
  const [sshKey, setSshKey] = useState("")

  const create = async () => {
    try {
      await post("/system-users/", {
        username,
        comment,
        shell,
        groups: groups.split(",").map((g) => g.trim()).filter(Boolean),
        sshKey: sshKey.trim(),
      })
      toast.success(`Created ${username}`)
      setOpen(false)
      setUsername("")
      setComment("")
      setGroups("")
      setSshKey("")
      onDone()
    } catch (err) {
      toast.error("Could not create account", { description: String(err) })
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <UserPlus className="size-4" />
          New account
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>New system account</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="new-username">Username</Label>
            <Input id="new-username" value={username} onChange={(e) => setUsername(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="new-comment">Full name</Label>
            <Input id="new-comment" value={comment} onChange={(e) => setComment(e.target.value)} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="new-shell">Shell</Label>
              <Input id="new-shell" value={shell} onChange={(e) => setShell(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="new-groups">Groups</Label>
              <Input
                id="new-groups"
                value={groups}
                onChange={(e) => setGroups(e.target.value)}
                placeholder="sudo, docker"
              />
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="new-key">SSH public key</Label>
            <Textarea
              id="new-key"
              value={sshKey}
              onChange={(e) => setSshKey(e.target.value)}
              className="font-mono text-xs"
              rows={3}
              placeholder="ssh-ed25519 AAAA…"
            />
            <p className="text-xs text-muted-foreground">
              The account is created with no password, so a key is the only way in. It is validated
              before the account exists.
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button onClick={create} disabled={!username}>
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function SSHKeysSheet({
  username,
  onOpenChange,
  onChanged,
}: {
  username: string | null
  onOpenChange: (open: boolean) => void
  onChanged: () => void
}) {
  const { confirm, dialog } = useConfirm()
  const [newKey, setNewKey] = useState("")
  const { data, error, loading, refresh } = usePoll(
    (signal) =>
      username
        ? get<{ path: string; keys: SSHKey[] }>(
            `/system-users/${encodeURIComponent(username)}/keys`,
            undefined,
            signal,
          )
        : Promise.resolve({ path: "", keys: [] }),
    0,
    [username],
  )

  const add = async () => {
    if (!username) return
    try {
      await post(`/system-users/${encodeURIComponent(username)}/keys`, { key: newKey.trim() })
      toast.success("Key authorised")
      setNewKey("")
      refresh()
      onChanged()
    } catch (err) {
      toast.error("Key rejected", { description: String(err) })
    }
  }

  return (
    <>
      <Sheet open={username !== null} onOpenChange={onOpenChange}>
        <SheetContent side="right" className="w-full sm:max-w-2xl">
          <SheetHeader>
            <SheetTitle>SSH keys for {username}</SheetTitle>
            <SheetDescription className="font-mono text-xs">{data?.path}</SheetDescription>
          </SheetHeader>

          <div className="space-y-4 px-4">
            {loading && <LoadingRows />}
            {error && <ErrorState error={error} />}

            {data?.keys.map((key) => (
              <div key={key.fingerprint} className="rounded-md border p-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <Badge variant="outline">{key.type}</Badge>
                      <span className="truncate text-sm">{key.comment || "no comment"}</span>
                    </div>
                    <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
                      {key.fingerprint}
                    </p>
                  </div>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-7 shrink-0 text-destructive"
                    onClick={() =>
                      confirm({
                        title: "Revoke SSH key",
                        phrase: username ?? "",
                        confirmLabel: "Revoke",
                        description: (
                          <p>
                            Whoever holds this key loses SSH access as <b>{username}</b>.
                          </p>
                        ),
                        action: async (c) => {
                          await del(`/system-users/${encodeURIComponent(username!)}/keys`, {
                            confirm: c,
                            query: { fingerprint: key.fingerprint },
                          })
                          refresh()
                          onChanged()
                        },
                      })
                    }
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              </div>
            ))}

            {data?.keys.length === 0 && (
              <EmptyState icon={KeyRound} title="No authorised keys" />
            )}

            <div className="space-y-2 border-t pt-4">
              <Label htmlFor="add-key">Authorise another key</Label>
              <Textarea
                id="add-key"
                value={newKey}
                onChange={(e) => setNewKey(e.target.value)}
                className="font-mono text-xs"
                rows={3}
                placeholder="ssh-ed25519 AAAA…"
              />
              <Button size="sm" onClick={add} disabled={!newKey.trim()}>
                <Plus className="size-4" />
                Add key
              </Button>
            </div>
          </div>
        </SheetContent>
      </Sheet>
      {dialog}
    </>
  )
}
