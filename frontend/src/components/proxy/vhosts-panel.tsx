"use client"

import { useEffect, useState } from "react"
import { CheckCircle2, FileCode, Globe, Plus, ShieldCheck, Trash2, XCircle } from "lucide-react"
import { notify } from "@/lib/toast"
import { del, get, post, put } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { VHost } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { CodeEditor } from "@/components/code-editor"
import { RowLink } from "@/components/page"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { SidePanel } from "@/components/side-panel"
import { EmptyState, ErrorState, LoadingPanel, Notice, Spinner } from "@/components/state"
import { Status } from "@/components/status-dot"
import { AuthFilesPanel } from "@/components/proxy/auth-files-panel"
import { SiteForm } from "@/components/proxy/site-form"
import { Button } from "@/components/ui/button"
import { IconAction } from "@/components/icon-action"
import { Switch } from "@/components/ui/switch"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/**
 * Every site this host serves, nginx and Caddy alike — editable through the
 * form that writes the config, or as raw text for the ones the form does not
 * own. The password files the form's auth field draws from sit beside it.
 */
export function VHostsPanel({ hasNginx }: { hasNginx: boolean }) {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [editing, setEditing] = useState<VHost | null>(null)
  const [formSite, setFormSite] = useState<string | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<VHost[]>("/proxy/vhosts", undefined, signal),
    30000,
  )
  const admin = can("system.admin")

  const openForm = (name: string | null) => {
    setFormSite(name)
    setFormOpen(true)
  }

  const toggle = async (vhost: VHost, enabled: boolean) => {
    const body = { enabled, reload: true }
    if (!enabled) {
      confirm({
        title: "Disable virtual host",
        confirmLabel: "Disable and reload",
        description: (
          <p>
            <b>{vhost.name}</b> stops serving as soon as nginx reloads. The config file stays on
            disk.
          </p>
        ),
        action: async (c) => {
          await post(`/proxy/vhosts/${encodeURIComponent(vhost.name)}/enabled`, body, {
            confirm: c,
          })
          refresh()
        },
      })
      return
    }
    try {
      await post(`/proxy/vhosts/${encodeURIComponent(vhost.name)}/enabled`, body)
      notify.success(`${vhost.name} enabled`)
      refresh()
    } catch (err) {
      notify.error("Could not enable", err)
    }
  }

  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  const hosts = data ?? []
  const secured = hosts.filter((v) => v.tls).length

  return (
    <>
      <div className="flex min-w-0 flex-col gap-4">
        <Panel>
          <PanelHeader
            icon={Globe}
            title="Sites"
            description={`${hosts.length} defined · ${secured} on TLS`}
            actions={
              admin &&
              hasNginx && (
                <Button size="sm" onClick={() => openForm(null)}>
                  <Plus className="size-4" />
                  New site
                </Button>
              )
            }
          />
          <PanelBody flush>
            {hosts.length === 0 ? (
              <EmptyState
                icon={Globe}
                title="No virtual hosts found"
                description={
                  hasNginx
                    ? "Put a domain in front of something running on this machine — the form writes the nginx config for you."
                    : "No nginx configuration directory was found on this host."
                }
                action={
                  admin &&
                  hasNginx && (
                    <Button size="sm" onClick={() => openForm(null)}>
                      <Plus className="size-4" />
                      New site
                    </Button>
                  )
                }
              />
            ) : (
              <Table containerClassName="max-h-[calc(100svh-22rem)]">
                <TableHeader className={stickyTableHeader}>
                  <TableRow>
                    <TableHead className="w-full">Host</TableHead>
                    <TableHead>Server names</TableHead>
                    <TableHead>Upstreams</TableHead>
                    <TableHead>TLS</TableHead>
                    <TableHead className="w-24">Enabled</TableHead>
                    <TableHead className="w-px" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {hosts.map((vhost) => (
                    <TableRow key={vhost.path} className="group">
                      <TableCell>
                        <div className="max-w-[20rem] min-w-0">
                          <RowLink
                            onClick={() =>
                              vhost.kind === "nginx" ? openForm(vhost.name) : setEditing(vhost)
                            }
                          >
                            {vhost.name}
                          </RowLink>
                          <p className="truncate font-mono text-[11px] text-muted-foreground">
                            {vhost.path}
                          </p>
                        </div>
                      </TableCell>
                      <TableCell className="text-xs">
                        {vhost.serverNames.join(", ") || (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell className="font-mono text-[11px] text-muted-foreground">
                        {vhost.upstreams.slice(0, 2).join(", ") || "—"}
                      </TableCell>
                      <TableCell>
                        {vhost.tls ? (
                          <Status state="active" label="TLS" icon={ShieldCheck} />
                        ) : (
                          <span className="text-xs text-muted-foreground">plaintext</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {vhost.kind === "caddy" ? (
                          <span className="text-xs text-muted-foreground">n/a</span>
                        ) : (
                          <Switch
                            checked={vhost.enabled}
                            disabled={!admin}
                            onCheckedChange={(v) => toggle(vhost, v)}
                            aria-label={`${vhost.name} enabled`}
                          />
                        )}
                      </TableCell>
                      <TableCell>
                        <span className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
                          <IconAction label="Edit the raw config" onClick={() => setEditing(vhost)}>
                            <FileCode />
                          </IconAction>
                          {admin && vhost.kind === "nginx" && (
                            <IconAction
                              label="Delete site"
                              className="text-destructive"
                              onClick={() =>
                                confirm({
                                  title: `Delete ${vhost.name}`,
                                  phrase: vhost.name,
                                  confirmLabel: "Delete and reload",
                                  description: (
                                    <p>
                                      The file and its symlink are removed and nginx reloads. The
                                      previous content is kept as{" "}
                                      <code className="font-mono">{vhost.name}.bak</code>.
                                    </p>
                                  ),
                                  action: async (c) => {
                                    await del(`/proxy/sites/${encodeURIComponent(vhost.name)}`, {
                                      confirm: c,
                                    })
                                    refresh()
                                  },
                                })
                              }
                            >
                              <Trash2 />
                            </IconAction>
                          )}
                        </span>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </PanelBody>
        </Panel>

        {admin && <AuthFilesPanel />}
      </div>

      <SiteForm open={formOpen} editing={formSite} onOpenChange={setFormOpen} onSaved={refresh} />
      <ConfigEditor vhost={editing} onOpenChange={(o) => !o && setEditing(null)} onSaved={refresh} />
      {dialog}
    </>
  )
}

function ConfigEditor({
  vhost,
  onOpenChange,
  onSaved,
}: {
  vhost: VHost | null
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  // Keyed on the file so opening another vhost never inherits the previous
  // one's buffer — saving that to the wrong path would be a real outage.
  return (
    <ConfigEditorBody
      key={vhost?.path ?? "none"}
      vhost={vhost}
      onOpenChange={onOpenChange}
      onSaved={onSaved}
    />
  )
}

function ConfigEditorBody({
  vhost,
  onOpenChange,
  onSaved,
}: {
  vhost: VHost | null
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { can } = useAuth()
  const [content, setContent] = useState("")
  const [original, setOriginal] = useState("")
  const [busy, setBusy] = useState(false)
  const [validation, setValidation] = useState<{ valid: boolean; output: string } | null>(null)

  useEffect(() => {
    if (!vhost) return
    const controller = new AbortController()
    get<{ content: string }>("/proxy/config", { path: vhost.path }, controller.signal)
      .then((r) => {
        setContent(r.content)
        setOriginal(r.content)
      })
      .catch((err) => !controller.signal.aborted && notify.error(String(err)))
    return () => controller.abort()
  }, [vhost])

  const validate = async () => {
    if (!vhost) return
    setBusy(true)
    try {
      setValidation(await post("/proxy/validate", { kind: vhost.kind, path: vhost.path, content }))
    } catch (err) {
      notify.error("Validation failed", err)
    } finally {
      setBusy(false)
    }
  }

  const save = async (reload: boolean) => {
    if (!vhost) return
    setBusy(true)
    try {
      await put("/proxy/config", { kind: vhost.kind, path: vhost.path, content, reload })
      notify.success(reload ? "Saved and reloaded" : "Saved")
      setOriginal(content)
      onSaved()
    } catch (err) {
      notify.error("Not applied", err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <SidePanel
      open={vhost !== null}
      onOpenChange={(o) => !busy && onOpenChange(o)}
      width="xl"
      icon={FileCode}
      title={vhost?.name ?? "Configuration"}
      description={vhost?.path}
      bodyClassName="flex min-h-0 flex-1 flex-col gap-3 p-4"
      footer={
        can("system.admin") && vhost ? (
          <>
            <Button size="sm" variant="outline" onClick={validate} disabled={busy}>
              {busy && <Spinner className="size-4" />}
              Test config
            </Button>
            <span className="flex-1" />
            <Button
              size="sm"
              variant="outline"
              onClick={() => save(false)}
              disabled={busy || content === original}
            >
              Save only
            </Button>
            <Button size="sm" onClick={() => save(true)} disabled={busy || content === original}>
              Save and reload
            </Button>
          </>
        ) : undefined
      }
    >
      <Notice icon={ShieldCheck} title="Validated before it takes effect">
        The server runs its own config test first. A config that fails is rolled back and never
        reloaded, so a typo here cannot take your sites offline.
      </Notice>

      <div className="min-h-0 flex-1 overflow-hidden rounded-xl border border-hairline bg-surface-sunken">
        <CodeEditor
          className="h-full"
          language="ini"
          value={content}
          readOnly={!can("system.admin")}
          onChange={(v) => {
            setContent(v)
            setValidation(null)
          }}
        />
      </div>

      {validation && (
        <div
          className={cn(
            "flex shrink-0 items-start gap-2 rounded-lg border p-3 text-xs",
            validation.valid
              ? "border-success/40 bg-success/10"
              : "border-destructive/40 bg-destructive/10",
          )}
        >
          {validation.valid ? (
            <CheckCircle2 className="size-4 shrink-0 text-success" />
          ) : (
            <XCircle className="size-4 shrink-0 text-destructive" />
          )}
          <pre className="font-mono whitespace-pre-wrap">
            {validation.output || "Config is valid."}
          </pre>
        </div>
      )}
    </SidePanel>
  )
}
