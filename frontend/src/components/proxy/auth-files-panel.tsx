"use client"

import { useState } from "react"
import { KeyRound, Plus, Trash2, UserMinus } from "lucide-react"
import { toast } from "sonner"
import { del, get, post } from "@/lib/api"
import type { AuthFile } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useConfirm } from "@/components/confirm-dialog"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"

/**
 * Passwords for the site form's basic-auth option.
 *
 * The field existed and there was nothing to put in it, which made the feature
 * useless from here: pointing at /etc/nginx/.htpasswd only helps somebody who
 * has already been to a terminal and run htpasswd — the thing this page exists
 * to avoid. Putting a staging site behind a password is one of the two or
 * three commonest reasons to reach for a reverse proxy at all.
 */
export function AuthFilesPanel() {
  const { confirm, dialog } = useConfirm()
  const { data, error, loading, refresh } = usePoll<AuthFile[]>(
    (signal) => get("/proxy/auth-files/", undefined, signal),
    0,
  )

  if (loading) return <LoadingPanel rows={3} />
  if (error) return <ErrorState error={error} />

  const removeUser = async (file: string, user: string) => {
    try {
      await del(`/proxy/auth-files/${encodeURIComponent(file)}/users/${encodeURIComponent(user)}`)
      toast.success(`${user} removed`)
      refresh()
    } catch (err) {
      toast.error("Could not remove", { description: String(err) })
    }
  }

  return (
    <>
      <Panel>
        <PanelHeader
          icon={KeyRound}
          title="Password files"
          description="For sites put behind a login. Hashed with bcrypt here, so the password never reaches a command line."
          actions={<AuthUserDialog files={data ?? []} onDone={refresh} />}
        />
        <PanelBody className={data?.length ? "space-y-2.5" : undefined}>
          {!data?.length ? (
            <EmptyState
              icon={KeyRound}
              title="No password files yet"
              description="Create one, then choose it in a site's password field to put that site behind a login."
            />
          ) : (
            data.map((file) => (
              <div
                key={file.name}
                className="flex min-w-0 flex-wrap items-start justify-between gap-x-4 gap-y-2 rounded-lg border border-hairline bg-surface-sunken p-2.5"
              >
                <div className="min-w-0 space-y-1.5">
                  <div className="flex flex-wrap items-baseline gap-x-2">
                    <span className="text-[13px] font-medium">{file.name}</span>
                    <code className="font-mono text-[11px] text-muted-foreground">
                      {file.path}
                    </code>
                  </div>
                  <div className="flex flex-wrap gap-1.5">
                    {file.users.length === 0 ? (
                      <span className="text-[11px] text-muted-foreground">
                        Empty — this file admits nobody.
                      </span>
                    ) : (
                      file.users.map((user) => (
                        <Badge key={user} variant="outline" className="gap-1 font-normal">
                          {user}
                          <button
                            type="button"
                            aria-label={`Remove ${user}`}
                            className="text-muted-foreground hover:text-destructive"
                            onClick={() => removeUser(file.name, user)}
                          >
                            <UserMinus className="size-3" />
                          </button>
                        </Badge>
                      ))
                    )}
                  </div>
                </div>
                <Button
                  size="xs"
                  variant="ghost"
                  className="text-destructive"
                  onClick={() =>
                    confirm({
                      title: `Delete ${file.name}`,
                      phrase: file.name,
                      confirmLabel: "Delete",
                      description: (
                        <p className="text-destructive">
                          Any site pointing at this file stops nginx from starting at its next
                          reload. Change those sites first.
                        </p>
                      ),
                      action: async (c) => {
                        await del(`/proxy/auth-files/${encodeURIComponent(file.name)}`, {
                          confirm: c,
                        })
                        refresh()
                      },
                    })
                  }
                >
                  <Trash2 className="size-3.5" />
                  Delete file
                </Button>
              </div>
            ))
          )}
        </PanelBody>
      </Panel>
      {dialog}
    </>
  )
}

function AuthUserDialog({ files, onDone }: { files: AuthFile[]; onDone: () => void }) {
  const [open, setOpen] = useState(false)
  const [file, setFile] = useState("")
  const [user, setUser] = useState("")
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    try {
      await post("/proxy/auth-files/", { file: file.trim(), user: user.trim(), password })
      toast.success(`${user} can sign in to ${file}`)
      setOpen(false)
      setUser("")
      setPassword("")
      onDone()
    } catch (err) {
      toast.error("Not saved", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <Plus className="size-4" />
          Add user
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add a login</DialogTitle>
          <DialogDescription>
            Creates the password file if it does not exist, and replaces the entry if the user is
            already in it.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="auth-file">File</Label>
            <Input
              id="auth-file"
              value={file}
              onChange={(e) => setFile(e.target.value)}
              list="auth-file-names"
              placeholder="staging"
              className="font-mono text-xs"
            />
            <datalist id="auth-file-names">
              {files.map((f) => (
                <option key={f.name} value={f.name} />
              ))}
            </datalist>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="auth-user">User</Label>
            <Input id="auth-user" value={user} onChange={(e) => setUser(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="auth-password">Password</Label>
            <Input
              id="auth-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
            />
            <p className="text-[11px] leading-relaxed text-muted-foreground">
              At least 8 characters, at most 72 — bcrypt truncates anything longer, which would
              quietly make it a different password from the one you typed.
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button
            onClick={submit}
            disabled={busy || !file.trim() || !user.trim() || password.length < 8}
          >
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
