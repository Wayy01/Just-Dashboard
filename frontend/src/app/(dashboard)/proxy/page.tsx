"use client"

import { useEffect, useState } from "react"
import dynamic from "next/dynamic"
import {
  CheckCircle2,
  Globe,
  Loader2,
  Plug,
  RefreshCw,
  ShieldCheck,
  ShieldX,
  XCircle,
} from "lucide-react"
import { toast } from "sonner"
import { del, get, post, put } from "@/lib/api"
import { timestamp } from "@/lib/format"
import type { Certificate, Listener, VHost } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

const MonacoEditor = dynamic(() => import("@monaco-editor/react"), { ssr: false })

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
    <>
      <PageHeader
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
            : undefined
        }
      />
      <Tabs defaultValue="vhosts">
        <TabsList>
          <TabsTrigger value="vhosts">Virtual hosts</TabsTrigger>
          <TabsTrigger value="certs">Certificates</TabsTrigger>
          <TabsTrigger value="ports">Listening ports</TabsTrigger>
        </TabsList>
        <TabsContent value="vhosts">
          <VHostsTab />
        </TabsContent>
        <TabsContent value="certs">
          <CertsTab />
        </TabsContent>
        <TabsContent value="ports">
          <PortsTab />
        </TabsContent>
      </Tabs>
    </>
  )
}

function VHostsTab() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [editing, setEditing] = useState<VHost | null>(null)
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<VHost[]>("/proxy/vhosts", undefined, signal),
    30000,
  )

  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />
  if (!data?.length) return <EmptyState icon={Globe} title="No virtual hosts found" />

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

  return (
    <>
      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Host</TableHead>
                <TableHead>Server names</TableHead>
                <TableHead>Upstreams</TableHead>
                <TableHead>TLS</TableHead>
                <TableHead className="w-24">Enabled</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((vhost) => (
                <TableRow key={vhost.path}>
                  <TableCell>
                    <button
                      className="font-medium hover:underline"
                      onClick={() => setEditing(vhost)}
                    >
                      {vhost.name}
                    </button>
                    <p className="truncate font-mono text-[11px] text-muted-foreground">
                      {vhost.path}
                    </p>
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
                      <Badge className="gap-1">
                        <ShieldCheck className="size-3" />
                        yes
                      </Badge>
                    ) : (
                      <Badge variant="secondary">no</Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    {vhost.kind === "caddy" ? (
                      <span className="text-xs text-muted-foreground">n/a</span>
                    ) : (
                      <Switch
                        checked={vhost.enabled}
                        disabled={!can("system.admin")}
                        onCheckedChange={(v) => toggle(vhost, v)}
                      />
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
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
  if (!vhost) return null
  // Keyed on the file so opening another vhost never inherits the previous
  // one's buffer — saving that to the wrong path would be a real outage.
  return (
    <ConfigEditorBody
      key={vhost.path}
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
  vhost: VHost
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { can } = useAuth()
  const [content, setContent] = useState("")
  const [original, setOriginal] = useState("")
  const [busy, setBusy] = useState(false)
  const [validation, setValidation] = useState<{ valid: boolean; output: string } | null>(null)

  useEffect(() => {
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
    <Sheet open onOpenChange={(o) => !busy && onOpenChange(o)}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-4xl">
        <SheetHeader className="border-b p-4">
          <SheetTitle>{vhost.name}</SheetTitle>
          <SheetDescription className="font-mono text-xs">{vhost.path}</SheetDescription>
        </SheetHeader>

        <Alert className="m-4 mb-0">
          <ShieldCheck className="size-4" />
          <AlertTitle>Validated before it takes effect</AlertTitle>
          <AlertDescription>
            The server runs its own config test first. A config that fails is rolled back and never
            reloaded, so a typo here cannot take your sites offline.
          </AlertDescription>
        </Alert>

        <div className="min-h-0 flex-1 p-4">
          <MonacoEditor
            height="100%"
            theme="vs-dark"
            language={vhost.kind === "caddy" ? "ini" : "ini"}
            value={content}
            onChange={(v) => {
              setContent(v ?? "")
              setValidation(null)
            }}
            options={{
              readOnly: !can("system.admin"),
              minimap: { enabled: false },
              fontSize: 13,
              automaticLayout: true,
              scrollBeyondLastLine: false,
            }}
          />
        </div>

        {validation && (
          <div
            className={
              validation.valid
                ? "mx-4 flex items-start gap-2 rounded-md border border-emerald-500/40 bg-emerald-500/10 p-3 text-xs"
                : "mx-4 flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-xs"
            }
          >
            {validation.valid ? (
              <CheckCircle2 className="size-4 shrink-0 text-emerald-400" />
            ) : (
              <XCircle className="size-4 shrink-0 text-destructive" />
            )}
            <pre className="whitespace-pre-wrap font-mono">
              {validation.output || "Config is valid."}
            </pre>
          </div>
        )}

        {can("system.admin") && (
          <SheetFooter className="flex-row gap-2 border-t p-4">
            <Button variant="outline" onClick={validate} disabled={busy}>
              {busy && <Loader2 className="size-4 animate-spin" />}
              Test config
            </Button>
            <span className="flex-1" />
            <Button
              variant="outline"
              onClick={() => save(false)}
              disabled={busy || content === original}
            >
              Save only
            </Button>
            <Button onClick={() => save(true)} disabled={busy || content === original}>
              Save and reload
            </Button>
          </SheetFooter>
        )}
      </SheetContent>
    </Sheet>
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
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Installed certificates</CardTitle>
          <CardDescription>From certbot and from the proxy configuration on disk</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {certs.loading && <LoadingRows className="p-4" />}
          {certs.error && <ErrorState error={certs.error} className="m-4" />}
          {certs.data && <CertTable certs={certs.data} />}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Watched domains</CardTitle>
          <CardDescription>
            Checked with a live TLS handshake, which catches a certificate renewed on disk but never
            reloaded
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {can("system.admin") && (
            <div className="flex max-w-md gap-2">
              <Input
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && addDomain()}
                placeholder="example.com"
              />
              <Button onClick={addDomain} disabled={!domain}>
                Watch
              </Button>
            </div>
          )}
          {watched.loading && <LoadingRows />}
          {watched.data?.length === 0 && (
            <EmptyState icon={ShieldX} title="No domains watched yet" />
          )}
          {watched.data && watched.data.length > 0 && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Domain</TableHead>
                  <TableHead>Issuer</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="w-px" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {watched.data.map((row) => (
                  <TableRow key={row.id}>
                    <TableCell className="font-medium">{row.domain}</TableCell>
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
                          size="sm"
                          variant="ghost"
                          className="h-7 text-xs"
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
          <Button variant="outline" size="sm" onClick={() => watched.refresh()}>
            <RefreshCw className="size-4" />
            Re-check now
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}

function CertTable({ certs }: { certs: Certificate[] }) {
  if (certs.length === 0) return <EmptyState icon={ShieldX} title="No certificates found" />
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Domains</TableHead>
          <TableHead>Issuer</TableHead>
          <TableHead>Expires</TableHead>
          <TableHead>Status</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {certs.map((cert) => (
          <TableRow key={cert.path || cert.name}>
            <TableCell>
              <div className="font-medium">{cert.name}</div>
              <p className="truncate font-mono text-[11px] text-muted-foreground">{cert.source}</p>
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
  if (!cert) return <Badge variant="secondary">unchecked</Badge>
  if (cert.error) return <Badge variant="destructive">{cert.error.slice(0, 40)}</Badge>
  if (cert.expired) return <Badge variant="destructive">expired</Badge>
  if (cert.expiring) return <Badge variant="destructive">{cert.daysLeft}d left</Badge>
  return <Badge>{cert.daysLeft}d left</Badge>
}

function PortsTab() {
  const { data, error, loading } = usePoll(
    (signal) => get<Listener[]>("/ports", undefined, signal),
    15000,
  )
  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />

  const exposed = data?.filter((l) => l.exposed).length ?? 0

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Listening ports</CardTitle>
        <CardDescription>
          {exposed} of {data?.length ?? 0} bound to a wildcard address and therefore reachable from
          off the machine
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-24">Port</TableHead>
              <TableHead className="w-20">Proto</TableHead>
              <TableHead>Bound to</TableHead>
              <TableHead>Process</TableHead>
              <TableHead>User</TableHead>
              <TableHead>Reach</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.map((listener, i) => (
              <TableRow key={`${listener.protocol}-${listener.address}-${listener.port}-${i}`}>
                <TableCell className="font-mono text-sm tabular-nums">{listener.port}</TableCell>
                <TableCell className="text-xs uppercase text-muted-foreground">
                  {listener.protocol}
                </TableCell>
                <TableCell className="font-mono text-xs">{listener.address || "*"}</TableCell>
                <TableCell>
                  <div className="text-sm">{listener.process || "unknown"}</div>
                  <p className="max-w-sm truncate font-mono text-[11px] text-muted-foreground">
                    {listener.cmdline}
                  </p>
                </TableCell>
                <TableCell className="text-xs">{listener.user ?? "—"}</TableCell>
                <TableCell>
                  {listener.exposed ? (
                    <Badge variant="destructive" className="gap-1">
                      <Plug className="size-3" />
                      exposed
                    </Badge>
                  ) : (
                    <Badge variant="secondary">loopback</Badge>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
