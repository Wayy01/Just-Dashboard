"use client"

import { useEffect, useState } from "react"
import {
  BadgeCheck,
  CheckCircle2,
  FileCode,
  Globe,
  Loader2,
  Plug,
  Plus,
  RefreshCw,
  ScanLine,
  ShieldCheck,
  ShieldX,
  Trash2,
  XCircle,
} from "lucide-react"
import { toast } from "sonner"
import { del, get, post, put } from "@/lib/api"
import { timestamp } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { Certificate, Listener, VHost } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { CodeEditor } from "@/components/code-editor"
import { Page, PageHeader } from "@/components/page"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { SidePanel } from "@/components/side-panel"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { CertbotPanel } from "@/components/proxy/certbot-panel"
import { SiteForm } from "@/components/proxy/site-form"
import { TLSReport } from "@/components/proxy/tls-report"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { IconAction } from "@/components/icon-action"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

type ProxyStatus = {
  nginx: boolean
  caddy: boolean
  nginxVersion?: string
  caddyVersion?: string
  certbot: boolean
}

export default function ProxyPage() {
  const status = usePoll((signal) => get<ProxyStatus>("/proxy/status", undefined, signal), 60000)

  return (
    <Page>
      <PageHeader
        eyebrow="Network"
        title="Proxy and TLS"
        description={
          status.data
            ? [
                status.data.nginx && status.data.nginxVersion,
                status.data.caddy && status.data.caddyVersion,
                status.data.certbot && "certbot",
              ]
                .filter(Boolean)
                .join(" · ") || "No reverse proxy detected"
            : "Checking what this host runs…"
        }
      />
      <Tabs defaultValue="vhosts" className="min-w-0 gap-4">
        <TabsList>
          <TabsTrigger value="vhosts">
            <Globe className="size-3.5" />
            Sites
          </TabsTrigger>
          <TabsTrigger value="certs">
            <BadgeCheck className="size-3.5" />
            Certificates
          </TabsTrigger>
          <TabsTrigger value="report">
            <ScanLine className="size-3.5" />
            TLS report
          </TabsTrigger>
          <TabsTrigger value="ports">
            <Plug className="size-3.5" />
            Listening ports
          </TabsTrigger>
        </TabsList>
        <TabsContent value="vhosts" className="min-w-0">
          <VHostsTab hasNginx={status.data?.nginx ?? false} />
        </TabsContent>
        <TabsContent value="certs" className="min-w-0">
          <CertsTab />
        </TabsContent>
        <TabsContent value="report" className="min-w-0">
          <TLSReport />
        </TabsContent>
        <TabsContent value="ports" className="min-w-0">
          <PortsTab />
        </TabsContent>
      </Tabs>
    </Page>
  )
}

function VHostsTab({ hasNginx }: { hasNginx: boolean }) {
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
        phrase: vhost.name,
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
      toast.success(`${vhost.name} enabled`)
      refresh()
    } catch (err) {
      toast.error("Could not enable", { description: String(err) })
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
                          <button
                            className="truncate text-[13px] font-medium hover:underline"
                            onClick={() =>
                              vhost.kind === "nginx" ? openForm(vhost.name) : setEditing(vhost)
                            }
                          >
                            {vhost.name}
                          </button>
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
                          <Badge variant="success" className="font-normal">
                            <ShieldCheck className="size-3" />
                            yes
                          </Badge>
                        ) : (
                          <Badge variant="secondary" className="font-normal">
                            no
                          </Badge>
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
                                    await del(
                                      `/proxy/sites/${encodeURIComponent(vhost.name)}`,
                                      { confirm: c },
                                    )
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
      </div>

      <SiteForm
        open={formOpen}
        editing={formSite}
        onOpenChange={setFormOpen}
        onSaved={refresh}
      />
      <ConfigEditor
        vhost={editing}
        onOpenChange={(o) => !o && setEditing(null)}
        onSaved={refresh}
      />
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
      .catch((err) => !controller.signal.aborted && toast.error(String(err)))
    return () => controller.abort()
  }, [vhost])

  const validate = async () => {
    if (!vhost) return
    setBusy(true)
    try {
      setValidation(await post("/proxy/validate", { kind: vhost.kind, path: vhost.path, content }))
    } catch (err) {
      toast.error("Validation failed", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }

  const save = async (reload: boolean) => {
    if (!vhost) return
    setBusy(true)
    try {
      await put("/proxy/config", { kind: vhost.kind, path: vhost.path, content, reload })
      toast.success(reload ? "Saved and reloaded" : "Saved")
      setOriginal(content)
      onSaved()
    } catch (err) {
      toast.error("Not applied", { description: String(err) })
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
              {busy && <Loader2 className="size-4 animate-spin" />}
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

function CertsTab() {
  const { can } = useAuth()
  const [domain, setDomain] = useState("")
  const certs = usePoll((signal) => get<Certificate[]>("/certificates/", undefined, signal), 300000)
  const watched = usePoll(
    (signal) =>
      get<{ id: number; domain: string; port: number; certificate?: Certificate }[]>(
        "/certificates/watched",
        undefined,
        signal,
      ),
    300000,
  )

  const addDomain = async () => {
    try {
      await post("/certificates/watched", { domain, port: 443 })
      setDomain("")
      watched.refresh()
    } catch (err) {
      toast.error("Could not watch domain", { description: String(err) })
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <CertbotPanel onChanged={certs.refresh} />

      <Panel>
        <PanelHeader
          icon={ShieldCheck}
          title="Installed certificates"
          description="Every certificate on disk, including the ones certbot did not put there"
        />
        <PanelBody flush>
          {certs.loading && <LoadingPanel />}
          {certs.error && <ErrorState error={certs.error} className="m-4" />}
          {certs.data && <CertTable certs={certs.data} />}
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader
          icon={Globe}
          title="Watched domains"
          description="Checked with a live TLS handshake, which catches a certificate renewed on disk but never reloaded"
          actions={
            <Button variant="outline" size="sm" onClick={() => watched.refresh()}>
              <RefreshCw className="size-3.5" />
              Re-check now
            </Button>
          }
        />
        {can("system.admin") && (
          <PanelToolbar>
            <Input
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && addDomain()}
              placeholder="example.com"
              className="h-8 w-full text-[13px] sm:w-72"
            />
            <Button size="sm" onClick={addDomain} disabled={!domain}>
              Watch
            </Button>
          </PanelToolbar>
        )}
        <PanelBody flush>
          {watched.loading && <LoadingPanel rows={3} />}
          {watched.data?.length === 0 && (
            <EmptyState icon={ShieldX} title="No domains watched yet" />
          )}
          {watched.data && watched.data.length > 0 && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-full">Domain</TableHead>
                  <TableHead>Issuer</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="w-px" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {watched.data.map((row) => (
                  <TableRow key={row.id} className="group">
                    <TableCell className="text-[13px] font-medium">{row.domain}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {row.certificate?.issuer ?? "—"}
                    </TableCell>
                    <TableCell className="text-xs">
                      {row.certificate ? timestamp(row.certificate.notAfter) : "—"}
                    </TableCell>
                    <TableCell>
                      <ExpiryBadge cert={row.certificate} />
                    </TableCell>
                    <TableCell>
                      {can("system.admin") && (
                        <Button
                          size="xs"
                          variant="ghost"
                          className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                          onClick={async () => {
                            await del(`/certificates/watched/${row.id}`)
                            watched.refresh()
                          }}
                        >
                          Remove
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </PanelBody>
      </Panel>
    </div>
  )
}

function CertTable({ certs }: { certs: Certificate[] }) {
  if (certs.length === 0) return <EmptyState icon={ShieldX} title="No certificates found" />
  return (
    <Table containerClassName="max-h-[26rem]">
      <TableHeader className={stickyTableHeader}>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead className="w-full">Domains</TableHead>
          <TableHead>Issuer</TableHead>
          <TableHead>Expires</TableHead>
          <TableHead>Status</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {certs.map((cert) => (
          <TableRow key={cert.path || cert.name}>
            <TableCell>
              <div className="max-w-[16rem] min-w-0">
                <div className="truncate text-[13px] font-medium">{cert.name}</div>
                <p className="truncate font-mono text-[11px] text-muted-foreground">
                  {cert.source}
                </p>
              </div>
            </TableCell>
            <TableCell className="max-w-xs truncate text-xs">{cert.domains.join(", ")}</TableCell>
            <TableCell className="text-xs text-muted-foreground">{cert.issuer}</TableCell>
            <TableCell className="text-xs">{timestamp(cert.notAfter)}</TableCell>
            <TableCell>
              <ExpiryBadge cert={cert} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function ExpiryBadge({ cert }: { cert?: Certificate }) {
  if (!cert)
    return (
      <Badge variant="secondary" className="font-normal">
        unchecked
      </Badge>
    )
  if (cert.error)
    return (
      <Badge variant="destructive" className="font-normal">
        {cert.error.slice(0, 40)}
      </Badge>
    )
  if (cert.expired)
    return (
      <Badge variant="destructive" className="font-normal">
        expired
      </Badge>
    )
  if (cert.expiring)
    return (
      <Badge variant="warning" className="font-normal">
        {cert.daysLeft}d left
      </Badge>
    )
  return (
    <Badge variant="success" className="font-normal">
      {cert.daysLeft}d left
    </Badge>
  )
}

function PortsTab() {
  const { data, error, loading } = usePoll(
    (signal) => get<Listener[]>("/ports", undefined, signal),
    15000,
  )
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  const exposed = data?.filter((l) => l.exposed).length ?? 0

  return (
    <Panel>
      <PanelHeader
        icon={Plug}
        title="Listening ports"
        description={`${exposed} of ${data?.length ?? 0} bound to a wildcard address and therefore reachable from off the machine`}
      />
      <PanelBody flush>
        <Table containerClassName="max-h-[calc(100svh-20rem)]">
          <TableHeader className={stickyTableHeader}>
            <TableRow>
              <TableHead className="w-20">Port</TableHead>
              <TableHead className="w-20">Proto</TableHead>
              <TableHead>Bound to</TableHead>
              <TableHead className="w-full">Process</TableHead>
              <TableHead>User</TableHead>
              <TableHead>Reach</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((listener, i) => (
              <TableRow key={`${listener.protocol}-${listener.address}-${listener.port}-${i}`}>
                <TableCell className="numeric font-mono text-[13px]">{listener.port}</TableCell>
                <TableCell className="text-xs uppercase text-muted-foreground">
                  {listener.protocol}
                </TableCell>
                <TableCell className="font-mono text-xs">{listener.address || "*"}</TableCell>
                <TableCell>
                  <div className="max-w-[22rem] min-w-0">
                    <div className="truncate text-[13px]">{listener.process || "unknown"}</div>
                    <p className="truncate font-mono text-[11px] text-muted-foreground">
                      {listener.cmdline}
                    </p>
                  </div>
                </TableCell>
                <TableCell className="text-xs">{listener.user ?? "—"}</TableCell>
                <TableCell>
                  {listener.exposed ? (
                    <Badge variant="destructive" className="font-normal">
                      <Plug className="size-3" />
                      exposed
                    </Badge>
                  ) : (
                    <Badge variant="secondary" className="font-normal">
                      loopback
                    </Badge>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </PanelBody>
    </Panel>
  )
}
