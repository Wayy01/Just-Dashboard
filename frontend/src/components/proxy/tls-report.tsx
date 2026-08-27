"use client"

import { useState } from "react"
import {
  AlertTriangle,
  CheckCircle2,
  Fingerprint,
  Info,
  Loader2,
  Lock,
  ScanLine,
  ShieldAlert,
  XCircle,
} from "lucide-react"
import { notify } from "@/lib/toast"
import { get } from "@/lib/api"
import { relativeTime, timestamp } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { ScanFinding, TLSScan } from "@/lib/types"
import { Detail, DetailList } from "@/components/page"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { EmptyState, Notice } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

/**
 * What a visitor actually gets, graded.
 *
 * Everything else on this page reads files from disk, which answers a question
 * nobody has. A certificate renewed and never reloaded, a proxy still offering
 * TLS 1.0 because the config came from a 2015 blog post, a redirect to HTTPS
 * that quietly stopped working — none of those show up in a file. SSL Labs
 * answers this, takes two minutes and needs a public hostname; every panel in
 * this class leaves you to go there.
 *
 * The grade is coarse on purpose and every finding carries its reasoning: a
 * letter with no working is a number to optimise rather than a thing to fix.
 */
export function TLSReport() {
  const [domain, setDomain] = useState("")
  const [scan, setScan] = useState<TLSScan | null>(null)
  const [busy, setBusy] = useState(false)

  const run = async () => {
    setBusy(true)
    setScan(null)
    try {
      setScan(await get<TLSScan>("/certificates/scan", { domain: domain.trim() }))
    } catch (err) {
      notify.error("Could not scan", err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Panel>
        <PanelHeader
          icon={ScanLine}
          title="Live TLS report"
          description="A handshake, a version probe, the chain as presented and the headers the site actually sends"
        />
        <PanelToolbar>
          <Input
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && domain.trim() && run()}
            placeholder="app.example.com"
            className="h-8 w-full text-[13px] sm:w-72"
          />
          <Button size="sm" onClick={run} disabled={busy || !domain.trim()}>
            {busy && <Loader2 className="size-4 animate-spin" />}
            Scan
          </Button>
        </PanelToolbar>
        <PanelBody>
          {!scan && !busy && (
            <EmptyState
              icon={ScanLine}
              title="Nothing scanned yet"
              description="Enter a domain this server should be serving. The scan reaches it the way a browser would, from the outside."
            />
          )}
          {busy && (
            <p className="text-[13px] text-muted-foreground">
              Handshaking, probing each TLS version separately, and fetching the headers…
            </p>
          )}
          {scan && <ScanSummary scan={scan} />}
        </PanelBody>
      </Panel>

      {scan?.reachable && (
        <>
          <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
            <Panel>
              <PanelHeader icon={Lock} title="Protocol versions" />
              <PanelBody className="space-y-1.5">
                {scan.protocols.map((protocol) => (
                  <div
                    key={protocol.name}
                    className="flex items-start justify-between gap-3 text-[13px]"
                  >
                    <span className="font-mono text-xs">{protocol.name}</span>
                    <span className="flex min-w-0 flex-col items-end gap-0.5">
                      <Badge
                        variant={
                          protocol.status === "offered"
                            ? isOldProtocol(protocol.name)
                              ? "destructive"
                              : "success"
                            : protocol.status === "unknown"
                              ? "secondary"
                              : "outline"
                        }
                        className="font-normal"
                      >
                        {protocol.status}
                      </Badge>
                      {protocol.detail && (
                        <span className="max-w-56 text-right text-[11px] leading-tight text-muted-foreground">
                          {protocol.detail}
                        </span>
                      )}
                    </span>
                  </div>
                ))}
                <p className="pt-1 text-[11px] leading-relaxed text-muted-foreground">
                  Each version is asked for on a connection of its own, so the answer is the
                  server&rsquo;s rather than a negotiation. &ldquo;unknown&rdquo; means this
                  dashboard&rsquo;s own TLS library would not make the request — reporting that as
                  absent would be a false reassurance.
                </p>
              </PanelBody>
            </Panel>

            <Panel>
              <PanelHeader icon={Fingerprint} title="Certificate" />
              <PanelBody>
                <DetailList>
                  <Detail label="Subject">{scan.certificate?.name ?? "—"}</Detail>
                  <Detail label="Names">{scan.certificate?.domains.join(", ") || "—"}</Detail>
                  <Detail label="Issuer">{scan.certificate?.issuer ?? "—"}</Detail>
                  <Detail label="Valid until">
                    {scan.certificate ? timestamp(scan.certificate.notAfter) : "—"}
                    {scan.certificate && (
                      <span className="ml-2 text-muted-foreground">
                        {scan.certificate.daysLeft} days left
                      </span>
                    )}
                  </Detail>
                  <Detail label="Key">
                    {scan.keyType}
                    {scan.keyBits ? ` ${scan.keyBits} bits` : ""}
                  </Detail>
                  <Detail label="Signature">{scan.signatureAlgorithm ?? "—"}</Detail>
                  <Detail label="Negotiated">
                    {scan.negotiated} · {scan.cipherSuite}
                  </Detail>
                  <Detail label="OCSP stapled">{scan.ocspStapled ? "yes" : "no"}</Detail>
                  <Detail label="SHA-256" className="font-mono text-[10px] break-all">
                    {scan.fingerprint}
                  </Detail>
                </DetailList>
              </PanelBody>
            </Panel>
          </div>

          <Panel>
            <PanelHeader
              icon={Lock}
              title="Chain as presented"
              description={
                scan.chainComplete
                  ? `${scan.chain.length} certificates sent`
                  : "Only the leaf was sent — desktop browsers paper over this from cache; phones, curl and payment gateways do not"
              }
            />
            <PanelBody className="space-y-1.5">
              {scan.chain.map((link, i) => (
                <div
                  key={`${link.subject}-${i}`}
                  className="flex flex-wrap items-baseline gap-x-3 gap-y-1 rounded-lg border border-hairline bg-surface-sunken p-2.5"
                >
                  <span className="text-[13px] font-medium">{link.subject}</span>
                  <span className="text-[11px] text-muted-foreground">
                    issued by {link.issuer}
                  </span>
                  <span className="ml-auto text-[11px] text-muted-foreground">
                    {link.keyType}
                    {link.keyBits ? ` ${link.keyBits}` : ""} · expires {relativeTime(link.notAfter)}
                  </span>
                  {link.isCa && (
                    <Badge variant="secondary" className="font-normal">
                      CA
                    </Badge>
                  )}
                </div>
              ))}
            </PanelBody>
          </Panel>

          {scan.http && (
            <Panel>
              <PanelHeader
                icon={ShieldAlert}
                title="HTTP behaviour"
                description={`Answered ${scan.http.statusCode}${scan.http.server ? ` · ${scan.http.server}` : ""}`}
              />
              <PanelBody className="space-y-2.5">
                <div className="flex flex-wrap items-center gap-2 text-[13px]">
                  <span>Plain HTTP</span>
                  {scan.http.plainError ? (
                    <Badge variant="secondary" className="font-normal">
                      refused connection
                    </Badge>
                  ) : scan.http.plainRedirects ? (
                    <Badge variant="success" className="font-normal">
                      redirects to HTTPS
                    </Badge>
                  ) : (
                    <Badge variant="destructive" className="font-normal">
                      answers {scan.http.plainStatus} without redirecting
                    </Badge>
                  )}
                  {scan.http.plainLocation && (
                    <code className="font-mono text-[11px] text-muted-foreground">
                      {scan.http.plainLocation}
                    </code>
                  )}
                </div>

                <div className="flex flex-wrap items-center gap-2 text-[13px]">
                  <span>HSTS</span>
                  {scan.http.hsts ? (
                    <Badge
                      variant={scan.http.hsts.maxAge >= 15552000 ? "success" : "warning"}
                      className="font-normal"
                    >
                      max-age {scan.http.hsts.maxAge}
                      {scan.http.hsts.includeSubDomains && " · subdomains"}
                      {scan.http.hsts.preload && " · preload"}
                    </Badge>
                  ) : (
                    <Badge variant="secondary" className="font-normal">
                      not set
                    </Badge>
                  )}
                </div>

                <div className="space-y-1 pt-1">
                  {scan.http.headers.map((header) => (
                    <div key={header.name} className="flex items-start gap-2">
                      {header.present ? (
                        <CheckCircle2 className="mt-0.5 size-3.5 shrink-0 text-success" />
                      ) : (
                        <XCircle
                          className={cn(
                            "mt-0.5 size-3.5 shrink-0",
                            header.level === "important"
                              ? "text-warning"
                              : "text-muted-foreground/60",
                          )}
                        />
                      )}
                      <div className="min-w-0 flex-1">
                        <p className="font-mono text-[11px]">
                          {header.name}
                          {header.value && (
                            <span className="ml-2 text-muted-foreground">{header.value}</span>
                          )}
                        </p>
                        {!header.present && (
                          <p className="text-[11px] leading-relaxed text-muted-foreground">
                            {header.detail}
                          </p>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </PanelBody>
            </Panel>
          )}
        </>
      )}
    </div>
  )
}

function ScanSummary({ scan }: { scan: TLSScan }) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <GradeBadge grade={scan.grade} />
        <div className="min-w-0">
          <p className="text-[13px] font-medium">{scan.domain}</p>
          <p className="text-xs text-muted-foreground">{scan.summary}</p>
        </div>
        <span className="ml-auto text-[11px] text-muted-foreground">
          checked {relativeTime(scan.checkedAt)}
        </span>
      </div>

      {!scan.reachable && (
        <Notice tone="danger" icon={XCircle} title="Nothing answered">
          {scan.error}
        </Notice>
      )}

      {scan.findings.length > 0 && (
        <div className="space-y-2">
          {scan.findings.map((finding) => (
            <FindingRow key={finding.id} finding={finding} />
          ))}
        </div>
      )}
      {scan.reachable && scan.findings.length === 0 && (
        <Notice tone="success" icon={CheckCircle2} title="Nothing to fix">
          Trusted chain, current protocols, and the headers that matter are in place.
        </Notice>
      )}
    </div>
  )
}

function FindingRow({ finding }: { finding: ScanFinding }) {
  return (
    <div
      className={cn(
        "flex min-w-0 gap-2.5 rounded-lg border p-2.5",
        finding.level === "critical"
          ? "border-destructive/30 bg-destructive/5"
          : finding.level === "warning"
            ? "border-warning/30 bg-warning/5"
            : "border-hairline bg-surface-sunken",
      )}
    >
      <LevelIcon
        level={finding.level}
        className={cn(
          "mt-0.5 size-4 shrink-0",
          finding.level === "critical"
            ? "text-destructive"
            : finding.level === "warning"
              ? "text-warning"
              : "text-muted-foreground",
        )}
      />
      <div className="min-w-0 flex-1 space-y-1">
        <p className="text-[13px] font-medium">{finding.title}</p>
        <p className="text-[11px] leading-relaxed text-muted-foreground">{finding.detail}</p>
        {finding.advice && (
          <p className="text-[11px] leading-relaxed text-foreground/80">{finding.advice}</p>
        )}
      </div>
    </div>
  )
}

function LevelIcon({ level, className }: { level: string; className?: string }) {
  if (level === "critical") return <ShieldAlert className={className} />
  if (level === "warning") return <AlertTriangle className={className} />
  return <Info className={className} />
}

export function GradeBadge({ grade, className }: { grade: string; className?: string }) {
  const tone =
    grade === "A+" || grade === "A"
      ? "border-success/30 bg-success/10 text-success"
      : grade === "B"
        ? "border-warning/30 bg-warning/10 text-warning"
        : grade === "C"
          ? "border-warning/40 bg-warning/15 text-warning"
          : "border-destructive/30 bg-destructive/10 text-destructive"
  return (
    <span
      className={cn(
        "flex size-11 shrink-0 items-center justify-center rounded-xl border text-base font-semibold",
        tone,
        className,
      )}
    >
      {grade}
    </span>
  )
}

function isOldProtocol(name: string) {
  return name === "TLS 1.0" || name === "TLS 1.1"
}
