"use client"

import { useMemo, useState } from "react"
import {
  CheckCircle,
  Globe,
  Lightning,
  LockClosed,
  RefreshClockwise,
  Router,
  ShieldCheck,
  Warning,
  Wrench,
} from "@/components/icons"
import { errorMessage } from "@/lib/api"
import { notify } from "@/lib/toast"
import type { DashboardSettings } from "@/lib/types"
import { useAuth } from "@/hooks/use-auth"
import { useSelfConfig } from "@/hooks/use-self-config"
import { useConfirm } from "@/components/confirm-dialog"
import { RestartProgress } from "@/components/config/restart-progress"
import { Page, PageHeader, Section } from "@/components/page"
import { Panel, PanelBody, PanelFooter, PanelHeader } from "@/components/panel"
import { StatTile } from "@/components/stat-tile"
import { EmptyState, ErrorState, LoadingPanel, Notice, Spinner } from "@/components/state"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

/**
 * The dashboard's own configuration: where it listens, how it is trusted, who
 * may reach it, and the two buttons that restart it.
 *
 * Every setting here used to be an ssh session and a hand-edited .env, which
 * is fine until the thing you need to change is the port you would have to
 * reach the dashboard on to change it. The page exists because that circle has
 * to be broken somewhere.
 *
 * The design rule throughout is that nothing happens silently. A change is
 * shown as a list of before-and-afters before it is applied, the restart is
 * narrated while it runs, an address that moves is stated as a URL to open,
 * and a configuration that does not come back up is undone on its own and said
 * to have been undone. An operator should never be left wondering whether
 * their dashboard is coming back.
 */
export default function DashboardConfigurationPage() {
  const { can } = useAuth()
  const { report, loading, error, restarting, running, apply, restart, dismiss, refresh } =
    useSelfConfig()
  const { confirm, dialog } = useConfirm()
  const [local, setLocal] = useState<DashboardSettings | null>(null)
  const [busy, setBusy] = useState(false)

  const saved = report?.settings
  // The form follows the server until the operator types into it, and does so
  // by *deriving* rather than by copying: a draft mirrored into state on every
  // poll would wipe half-typed input every two seconds during a restart, and
  // one that never re-synced would show settings a restart has already
  // replaced. Null means "whatever the server says"; discarding returns to it.
  const draft = local ?? saved
  const changes = useMemo(() => (saved && draft ? diff(saved, draft) : []), [saved, draft])
  const dirty = changes.length > 0
  const admin = can("system.admin")
  const editable = Boolean(report?.supported) && admin && !running

  if (loading && !report) {
    return (
      <Page>
        <PageHeader eyebrow="Operations" title="Configuration" />
        <LoadingPanel />
      </Page>
    )
  }
  if (error) {
    return (
      <Page>
        <PageHeader eyebrow="Operations" title="Configuration" />
        <ErrorState error={error} />
      </Page>
    )
  }
  if (!report || !draft) return null

  const set = <K extends keyof DashboardSettings>(key: K, value: DashboardSettings[K]) =>
    setLocal({ ...draft, [key]: value })

  const applyChanges = () => {
    // The phrase the server will demand, computed the same way it computes it,
    // so the dialog asks for exactly what the API expects rather than for
    // something that merely looks like it.
    const moves =
      saved!.site !== draft.site ||
      saved!.port !== draft.port ||
      saved!.tls !== draft.tls ||
      saved!.bind !== draft.bind
    // Bracketed for IPv6, because the server builds this phrase with
    // net.JoinHostPort and a dialog asking for something the API will reject is
    // worse than no dialog at all.
    const host = draft.site.includes(":") ? `[${draft.site}]` : draft.site
    const phrase = moves ? `${host}:${draft.port}` : undefined

    confirm({
      title: "Apply and restart",
      phrase,
      confirmLabel: "Apply and restart",
      description: (
        <>
          <p>The dashboard restarts into these settings:</p>
          <ul className="space-y-1 rounded-lg border border-hairline bg-surface-sunken p-2.5 text-xs">
            {changes.map((change) => (
              <li key={change.key} className="flex flex-wrap items-baseline gap-x-2">
                <span className="font-medium">{change.label}</span>
                <span className="font-mono text-muted-foreground line-through">
                  {change.from || "—"}
                </span>
                <span className="text-muted-foreground">→</span>
                <span className="font-mono">{change.to || "—"}</span>
              </li>
            ))}
          </ul>
          {moves && (
            <p>
              It will then answer at <b>{endpointOf(draft)}</b>, not at the address this tab is
              using. That is why the phrase below is that address — type it and you have read it.
            </p>
          )}
          <p className="text-muted-foreground">
            If the new configuration does not come back up, the previous one is restored
            automatically and the dashboard returns exactly as it is now.
          </p>
        </>
      ),
      action: async (typed) => {
        await apply(draft, typed || undefined)
        // Back to following the server: the settings just sent are the ones it
        // is restarting into, and the form should show what it reports rather
        // than what this tab last typed.
        setLocal(null)
      },
    })
  }

  const startRestart = (rebuild: boolean) =>
    confirm({
      title: rebuild ? "Rebuild and restart" : "Restart the dashboard",
      confirmLabel: rebuild ? "Rebuild" : "Restart",
      description: rebuild ? (
        <>
          <p>
            Rebuilds every image in this stack from the checkout at{" "}
            <code className="font-mono">{report.dir}</code>, then recreates the containers. It takes
            a few minutes.
          </p>
          <p className="text-muted-foreground">
            Nothing about your settings, data or accounts changes. This is what to do after editing
            the code on disk by hand.
          </p>
        </>
      ) : (
        <>
          <p>
            Recreates every container in this stack on the settings already on disk. It takes a few
            seconds, and the page will lose contact with the server while it happens.
          </p>
          <p className="text-muted-foreground">
            Sessions survive: they live in the database, not in the process.
          </p>
        </>
      ),
      action: async () => {
        await restart(rebuild)
      },
    })

  return (
    <Page>
      {dialog}
      <PageHeader
        eyebrow="Operations"
        title="Configuration"
        description="Where this dashboard listens, how it is trusted, and who may reach it"
        actions={
          <>
            <Button variant="outline" size="sm" onClick={refresh} disabled={busy}>
              <RefreshClockwise className="size-4" />
              Refresh
            </Button>
            {admin && report.supported && (
              <>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={running}
                  onClick={() => startRestart(false)}
                >
                  <Lightning className="size-4" />
                  Restart
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={running}
                  onClick={() => startRestart(true)}
                >
                  <Wrench className="size-4" />
                  Rebuild
                </Button>
              </>
            )}
          </>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4 [&>*]:min-w-0">
        <StatTile
          label="Address"
          icon={Globe}
          value={report.settings.site}
          hint={report.endpoint}
        />
        <StatTile
          label="Certificate"
          icon={LockClosed}
          value={certLabel(report.settings.tls)}
          tone={report.settings.tls === "internal" ? "warning" : "success"}
          hint={certHint(report.settings.tls)}
        />
        <StatTile
          label="Ports"
          icon={Router}
          value={String(report.settings.port)}
          hint={`frontend ${report.settings.frontendPort} · backend ${report.settings.backendPort}`}
        />
        <StatTile
          label="Two-factor"
          icon={ShieldCheck}
          value={report.settings.require2fa ? "Required" : "Optional"}
          hint={
            report.settings.require2fa
              ? "every account must enrol"
              : "enrolled accounts are still asked"
          }
        />
      </div>

      {report.run && (
        <Panel>
          <PanelHeader
            icon={running ? RefreshClockwise : CheckCircle}
            title={running ? "Restarting" : "Last restart"}
            description={report.run.dir}
          />
          <PanelBody>
            <RestartProgress
              run={report.run}
              log={report.log}
              restarting={restarting}
              onDismiss={
                admin
                  ? () => {
                      dismiss().catch((err) => notify.error("Could not dismiss", errorMessage(err)))
                    }
                  : undefined
              }
            />
          </PanelBody>
        </Panel>
      )}

      {!report.supported && (
        <Notice title="This install is configured by hand" icon={Warning}>
          {report.reason ??
            "The dashboard could not find the compose project it was deployed from."}{" "}
          The settings below are read from disk and shown for reference; changing them here is not
          possible, so edit <code className="font-mono">.env</code> and restart the stack over ssh.
        </Notice>
      )}

      {report.drift && report.drift.length > 0 && (
        <Notice title="The file on disk and the running dashboard disagree" tone="warning">
          <p className="mb-1">
            <code className="font-mono">{report.envPath}</code> has been edited since this dashboard
            started. Restarting adopts these; nothing is lost by leaving them.
          </p>
          <ul className="space-y-0.5">
            {report.drift.map((change) => (
              <li key={change.key} className="font-mono text-[11px]">
                {change.label}: {change.from || "—"} → {change.to || "—"}
              </li>
            ))}
          </ul>
        </Notice>
      )}

      <Section
        title="How it is reached"
        description="The address in your browser, the certificate behind it, and the port it answers on"
      >
        <Panel>
          <PanelHeader
            icon={Globe}
            title="Address and certificate"
            description={`Currently ${report.endpoint}`}
          />
          <PanelBody className="grid gap-4 sm:grid-cols-2">
            <Field
              label="Dashboard address"
              hint="What you type into the browser, and the name on the certificate."
            >
              <Input
                value={draft.site}
                disabled={!editable}
                onChange={(e) => set("site", e.target.value)}
              />
            </Field>

            <Field
              label="Dashboard port"
              hint="The one port anybody has to remember. Everything else is internal."
            >
              <Input
                type="number"
                inputMode="numeric"
                value={draft.port}
                disabled={!editable}
                onChange={(e) => set("port", Number(e.target.value))}
              />
            </Field>

            <Field
              label="Certificate"
              hint={certHint(draft.tls)}
              className={draft.tls === "internal" ? "sm:col-span-2" : undefined}
            >
              <Select
                value={draft.tls}
                disabled={!editable}
                onValueChange={(value) => set("tls", value as DashboardSettings["tls"])}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="tailscale">Tailscale — trusted, no warning</SelectItem>
                  <SelectItem value="internal">Self-signed — the browser warns</SelectItem>
                  <SelectItem value="off">Plain HTTP — localhost only</SelectItem>
                </SelectContent>
              </Select>
            </Field>

            <Field
              label="Listening interface"
              hint="Blank uses the address above. A Tailscale install answers for a MagicDNS name and listens on the tailnet IP behind it."
            >
              <Input
                value={draft.bind}
                placeholder={draft.site}
                disabled={!editable}
                onChange={(e) => set("bind", e.target.value)}
              />
            </Field>
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader
            icon={Router}
            title="Internal ports"
            description="Bound to loopback and reached only by the proxy. Change them if something else on this machine wants the number."
          />
          <PanelBody className="grid gap-4 sm:grid-cols-2">
            <Field label="Frontend port" hint="Next.js, published on 127.0.0.1 only.">
              <Input
                type="number"
                inputMode="numeric"
                value={draft.frontendPort}
                disabled={!editable}
                onChange={(e) => set("frontendPort", Number(e.target.value))}
              />
            </Field>
            <Field label="Backend port" hint="The Go API, bound to 127.0.0.1 only.">
              <Input
                type="number"
                inputMode="numeric"
                value={draft.backendPort}
                disabled={!editable}
                onChange={(e) => set("backendPort", Number(e.target.value))}
              />
            </Field>
          </PanelBody>
        </Panel>
      </Section>

      <Section
        title="Who may reach it"
        description="Checked before authentication: an address outside this list never reaches the login page"
      >
        <Panel>
          <PanelHeader icon={ShieldCheck} title="Access" />
          <PanelBody className="space-y-4">
            <Field
              label="Network allowlist"
              hint="Comma-separated addresses or ranges. 127.0.0.1/32 has to stay — it is what makes an SSH tunnel work, and it is the way back in if everything else fails."
            >
              <Input
                value={draft.allowedCidrs}
                disabled={!editable}
                onChange={(e) => set("allowedCidrs", e.target.value)}
                className="font-mono text-[13px]"
              />
            </Field>

            <Toggle
              label="Require two-factor for every account"
              hint="With this off, an account that has not enrolled signs in with its password alone. An account that has enrolled is always asked for its code either way."
              checked={draft.require2fa}
              disabled={!editable}
              onChange={(value) => set("require2fa", value)}
            />

            <Toggle
              label="Web terminal"
              hint="A real shell with this dashboard's privileges. Turning it off removes the routes, not just the page."
              checked={draft.terminalEnabled}
              disabled={!editable}
              onChange={(value) => set("terminalEnabled", value)}
            />

            <Toggle
              label="Check for new versions"
              hint="The only outbound request this dashboard makes on its own initiative."
              checked={draft.updateCheck}
              disabled={!editable}
              onChange={(value) => set("updateCheck", value)}
            />

            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                label="Session lifetime"
                hint="How long a sign-in lasts at most. For example 12h."
              >
                <Input
                  value={draft.sessionTtl}
                  disabled={!editable}
                  onChange={(e) => set("sessionTtl", e.target.value)}
                />
              </Field>
              <Field
                label="Idle timeout"
                hint="How long an unused session survives. For example 60m."
              >
                <Input
                  value={draft.idleTtl}
                  disabled={!editable}
                  onChange={(e) => set("idleTtl", e.target.value)}
                />
              </Field>
            </div>
          </PanelBody>
          {editable && (
            <PanelFooter className="justify-between">
              <p className="text-xs text-muted-foreground">
                {dirty
                  ? `${changes.length} change${changes.length === 1 ? "" : "s"} — applying restarts the dashboard`
                  : "Nothing to apply"}
              </p>
              <div className="flex gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={!dirty || busy}
                  onClick={() => setLocal(null)}
                >
                  Discard
                </Button>
                <Button
                  size="sm"
                  disabled={!dirty || busy}
                  onClick={() => {
                    setBusy(true)
                    try {
                      applyChanges()
                    } finally {
                      setBusy(false)
                    }
                  }}
                >
                  {busy && <Spinner className="size-4" />}
                  Apply and restart
                </Button>
              </div>
            </PanelFooter>
          )}
        </Panel>
      </Section>

      {report.supported && (
        <Section
          title="Where this install lives"
          description="What the dashboard concluded about itself, which is what a restart acts on"
        >
          <Panel>
            <PanelBody className="grid gap-3 text-[13px] sm:grid-cols-2">
              <Fact label="Checkout" value={report.dir ?? "—"} />
              <Fact label="Compose file" value={report.compose ?? "—"} />
              <Fact label="Settings file" value={report.envPath ?? "—"} />
              <Fact label="Endpoint" value={report.endpoint} />
            </PanelBody>
          </Panel>
        </Section>
      )}

      {!report.supported && !report.settings && (
        <EmptyState icon={Warning} title="No settings could be read" description={report.reason} />
      )}
    </Page>
  )
}

function Field({
  label,
  hint,
  className,
  children,
}: {
  label: string
  hint?: string
  className?: string
  children: React.ReactNode
}) {
  return (
    <div className={className}>
      <div className="space-y-1.5">
        <Label>{label}</Label>
        {children}
        {hint && <p className="text-xs leading-relaxed text-muted-foreground">{hint}</p>}
      </div>
    </div>
  )
}

function Toggle({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string
  hint: string
  checked: boolean
  disabled?: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-xl border border-hairline bg-surface-sunken p-3">
      <div className="min-w-0 space-y-0.5">
        <p className="text-[13px] font-medium">{label}</p>
        <p className="text-xs leading-relaxed text-muted-foreground">{hint}</p>
      </div>
      <Switch checked={checked} disabled={disabled} onCheckedChange={onChange} />
    </div>
  )
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="truncate font-mono text-[12px]">{value}</p>
    </div>
  )
}

function certLabel(tls: DashboardSettings["tls"]) {
  switch (tls) {
    case "tailscale":
      return "Trusted"
    case "off":
      return "None"
    default:
      return "Self-signed"
  }
}

function certHint(tls: DashboardSettings["tls"]) {
  switch (tls) {
    case "tailscale":
      return "A real certificate for your MagicDNS name, renewed automatically. No browser warning."
    case "off":
      return "Plain HTTP on loopback. Browsers treat localhost as secure, so nothing warns — the SSH tunnel is the encryption."
    default:
      return "Caddy's own CA. Encrypted, but every browser says “not secure” because nothing off this machine knows the issuer."
  }
}

function endpointOf(s: DashboardSettings) {
  return `${s.tls === "off" ? "http" : "https"}://${s.site}:${s.port}`
}

/**
 * The same comparison the server makes, so the dialog lists exactly what the
 * server is about to write. Kept in the page rather than shared: it exists to
 * describe a form, and the authority is and remains the backend.
 */
function diff(saved: DashboardSettings, draft: DashboardSettings) {
  const fields: { key: keyof DashboardSettings; label: string }[] = [
    { key: "site", label: "Address" },
    { key: "bind", label: "Listening interface" },
    { key: "tls", label: "Certificate" },
    { key: "port", label: "Dashboard port" },
    { key: "frontendPort", label: "Frontend port" },
    { key: "backendPort", label: "Backend port" },
    { key: "allowedCidrs", label: "Network allowlist" },
    { key: "terminalEnabled", label: "Web terminal" },
    { key: "require2fa", label: "Two-factor required" },
    { key: "sessionTtl", label: "Session lifetime" },
    { key: "idleTtl", label: "Idle timeout" },
    { key: "updateCheck", label: "Version checks" },
  ]
  return fields
    .filter((field) => saved[field.key] !== draft[field.key])
    .map((field) => ({
      key: field.key,
      label: field.label,
      from: String(saved[field.key]),
      to: String(draft[field.key]),
    }))
}
