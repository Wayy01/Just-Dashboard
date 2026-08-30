"use client"

import { useState } from "react"
import { Globe, RefreshClockwise, ShieldCheck, ShieldOff } from "@/components/icons"
import { notify } from "@/lib/toast"
import { del, get, post } from "@/lib/api"
import { timestamp } from "@/lib/format"
import type { Certificate } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"
import { CertbotPanel } from "@/components/proxy/certbot-panel"
import { ExpiryStatus } from "@/components/proxy/expiry-status"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
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
 * Certbot's own issue/renew/revoke, the certificates on disk, and the domains
 * checked with a live handshake — which catches one renewed on disk but never
 * reloaded, the failure that is invisible in a file.
 */
export function CertsPanel() {
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
      notify.error("Could not watch domain", err)
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
              <RefreshClockwise className="size-3.5" />
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
            <EmptyState icon={ShieldOff} title="No domains watched yet" />
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
                      <ExpiryStatus cert={row.certificate} />
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
  if (certs.length === 0) return <EmptyState icon={ShieldOff} title="No certificates found" />
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
                <p className="truncate font-mono text-[11px] text-muted-foreground">{cert.source}</p>
              </div>
            </TableCell>
            <TableCell className="max-w-xs truncate text-xs">{cert.domains.join(", ")}</TableCell>
            <TableCell className="text-xs text-muted-foreground">{cert.issuer}</TableCell>
            <TableCell className="text-xs">{timestamp(cert.notAfter)}</TableCell>
            <TableCell>
              <ExpiryStatus cert={cert} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
