"use client"

import { useEffect, useMemo, useState } from "react"
import { Key, NetworkDevice, TerminalWindow, Warning } from "@/components/icons"
import { get, post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { Job, SSHDConfig, SSHSetting } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { JobConsole, RecentJobs, useJobConsole } from "@/components/job-console"
import { Panel, PanelBody, PanelFooter, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { Status } from "@/components/status-dot"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

/**
 * The SSH server's own settings, which is where a single-server operator is
 * actually attacked.
 *
 * The firewall decides who may knock. sshd decides what happens next, and its
 * defaults are a compromise struck for compatibility across twenty years of
 * clients rather than for a machine on a public address. Three of these are
 * one line each in a file nobody opens, and the difference between them being
 * right and wrong is the difference between a bot wasting its time and a bot
 * getting in.
 *
 * Changes are staged and applied together: sshd is tested with its own parser
 * before the daemon is asked to reload, and the file is put back if the test
 * fails. The one refusal that is not about syntax is the important one —
 * turning off password authentication on a host where nobody has a key.
 */
export function SSHPanel() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const admin = can("system.admin")
  const { data, error, loading, refresh } = usePoll<SSHDConfig>(
    (signal) => get("/ssh/config", undefined, signal),
    0,
    [],
    { enabled: admin },
  )
  const [pending, setPending] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const console_ = useJobConsole()

  // The effective configuration is only right once sshd has reloaded.
  const jobStatus = console_.job?.status
  useEffect(() => {
    if (jobStatus === "succeeded") refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jobStatus])

  const dirty = useMemo(() => Object.keys(pending).length > 0, [pending])

  if (!admin) {
    return (
      <EmptyState
        icon={TerminalWindow}
        title="SSH settings need the admin capability"
        description="They name the accounts that hold keys, which is a map of who can reach this machine."
      />
    )
  }
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />
  if (!data?.available) {
    return (
      <EmptyState
        icon={TerminalWindow}
        title="No SSH server on this host"
        description={data?.error ?? "Neither sshd nor its configuration was found."}
      />
    )
  }

  const valueOf = (setting: SSHSetting) => pending[setting.key] ?? setting.value
  const changed = (setting: SSHSetting) =>
    pending[setting.key] !== undefined && pending[setting.key] !== setting.value

  const noKeys = data.keyedAccounts.length === 0
  const insecure = data.settings.filter((s) => !s.secure).length

  const apply = () =>
    confirm({
      title: "Apply SSH changes",
      phrase: "change ssh",
      confirmLabel: "Test and apply",
      description: (
        <div className="space-y-2">
          <p>
            The new configuration is written to <code className="font-mono">{data.managedFile}</code>
            , tested with sshd&rsquo;s own parser and put back if the test fails. Existing sessions are
            not disconnected by a reload.
          </p>
          <p className="text-destructive">
            Keep this session open and confirm you can still log in from a second terminal before
            closing it.
          </p>
        </div>
      ),
      action: async (c) => {
        setBusy(true)
        try {
          // The plan — including every lockout guard — is checked before this
          // returns, so a refusal is this dialog's answer. What comes back is
          // a job for the write, the sshd -t and the reload, which is the part
          // worth watching: this is the one operation where "it said it
          // worked" is not the same as knowing the daemon came back.
          const job = await post<Job>("/ssh/config", { settings: pending }, { confirm: c })
          console_.attach(job)
          setPending({})
        } finally {
          setBusy(false)
        }
      },
    })

  return (
    <>
      <div className="flex min-w-0 flex-col gap-4">
        <JobConsole
          job={console_.job}
          lines={console_.lines}
          onDismiss={console_.dismiss}
          onCancel={console_.cancel}
        />

        {noKeys && (
          <Notice tone="warning" icon={Key} title="No account on this host has an SSH key">
            Password authentication cannot safely be turned off until one does — with no key
            anywhere, doing so would leave nobody a way in, and the server refuses the change for
            that reason. Add a key from the Users page first.
          </Notice>
        )}
        {data.socket?.unit && (
          <Notice icon={NetworkDevice} title={`The port belongs to ${data.socket.unit}, not to sshd_config`}>
            This host runs socket-activated SSH — systemd holds the listener and hands sshd a
            connection, so sshd never binds a port of its own and the Port directive is read and
            ignored. Changing it here writes the directive <em>and</em> a drop-in for{" "}
            <code className="font-mono">{data.socket.unit}</code>, then restarts the socket, which
            is the half that actually moves where connections land. Existing sessions are separate
            processes and are not disconnected.
          </Notice>
        )}
        {data.hasMatchBlocks && (
          <Notice icon={Warning} title="This configuration has Match blocks">
            Some of these values are overridden for particular users or addresses. What is shown
            here is the unconditional configuration; the conditional parts are not editable from
            this page.
          </Notice>
        )}

        <Panel>
          <PanelHeader
            icon={TerminalWindow}
            title="SSH server"
            description={
              data.socket?.unit
                ? `Port ${data.ports.join(", ")} · held by ${data.socket.unit} · from ${data.source}`
                : `Port ${data.ports.join(", ")} · from ${data.source}`
            }
            actions={
              <>
                <RecentJobs kinds={["ssh."]} onOpen={console_.open} />
                <Status
                  verdict={insecure === 0 ? "ok" : "warning"}
                  label={insecure === 0 ? "Hardened" : `${insecure} below recommendation`}
                />
              </>
            }
          />
          <PanelBody flush>
            <div className="divide-y divide-hairline">
              {data.settings.map((setting) => (
                <SettingRow
                  key={setting.key}
                  setting={setting}
                  value={valueOf(setting)}
                  changed={changed(setting)}
                  onChange={(v) => setPending((p) => ({ ...p, [setting.key]: v }))}
                />
              ))}
            </div>
          </PanelBody>
          {dirty && (
            <PanelFooter>
              <span className="text-xs text-muted-foreground">
                {Object.keys(pending).length} pending — written to {data.managedFile}
              </span>
              <span className="flex-1" />
              <Button size="sm" variant="outline" onClick={() => setPending({})} disabled={busy}>
                Discard
              </Button>
              <Button size="sm" onClick={apply} disabled={busy}>
                Test and apply
              </Button>
            </PanelFooter>
          )}
        </Panel>

        <Panel>
          <PanelHeader
            icon={Key}
            title="Accounts with an authorized key"
            description="Who could still log in with password authentication switched off"
          />
          <PanelBody>
            {noKeys ? (
              <p className="text-[13px] text-muted-foreground">
                None. Every login on this host currently depends on a password.
              </p>
            ) : (
              <div className="flex flex-wrap gap-1.5">
                {data.keyedAccounts.map((account) => (
                  <Badge key={account.user} variant="outline" className="font-normal">
                    <Key className="size-3" />
                    {account.user}
                    <span className="text-muted-foreground">
                      {account.keys} {account.keys === 1 ? "key" : "keys"}
                    </span>
                  </Badge>
                ))}
              </div>
            )}
          </PanelBody>
        </Panel>
      </div>
      {dialog}
    </>
  )
}

/**
 * One sshd directive as a row in a plain divided list — the label in words, the
 * directive name beside it, one line of what it does, and the control on the
 * right at a fixed column so every row's answer lines up.
 *
 * The row only raises its voice when the setting is below the recommendation:
 * a warning-tinted left edge and the "recommended … because …" line appear
 * there and nowhere else. Every row used to be its own bordered card in one of
 * three colours, which made a list of twelve mostly-fine settings look like
 * twelve problems.
 *
 * A two-value choice (password auth on/off, root login) is a segmented control
 * rather than a dropdown — the two states are the whole decision and both
 * should be visible without opening a menu.
 */
function SettingRow({
  setting,
  value,
  changed,
  onChange,
}: {
  setting: SSHSetting
  value: string
  changed: boolean
  onChange: (value: string) => void
}) {
  const below = !setting.secure
  const segmented =
    setting.kind === "choice" &&
    setting.options?.length === 2 &&
    setting.options.includes(value)

  return (
    <div
      className={cn(
        "flex min-w-0 flex-wrap items-start justify-between gap-x-4 gap-y-2 border-l-2 border-transparent px-4 py-3",
        changed && "border-primary/60 bg-primary/[0.04]",
        !changed && below && "border-warning/50 bg-warning/[0.035]",
      )}
    >
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-baseline gap-x-2">
          <span className="text-[13px] font-medium">{setting.label}</span>
          <code className="font-mono text-[11px] text-muted-foreground">{setting.key}</code>
        </div>
        <p className="text-[11px] leading-relaxed text-muted-foreground">{setting.detail}</p>
        {below && (
          <p className="text-[11px] leading-relaxed">
            <span className="font-medium text-warning">Recommended {setting.recommended}.</span>
            {setting.risk && <span className="text-foreground/75"> {setting.risk}</span>}
          </p>
        )}
      </div>
      <div className={cn("shrink-0", setting.kind === "list" ? "w-full sm:w-72" : "w-44")}>
        {setting.kind === "list" ? (
          <Input
            value={value}
            placeholder="deploy admin — empty allows everyone"
            className="font-mono text-xs"
            onChange={(e) => onChange(e.target.value)}
          />
        ) : segmented ? (
          <ToggleGroup
            type="single"
            value={value}
            onValueChange={(v) => v && onChange(v)}
            variant="outline"
            size="sm"
            className="w-full"
            aria-label={setting.label}
          >
            {setting.options!.map((option) => (
              <ToggleGroupItem key={option} value={option} className="flex-1 text-xs capitalize">
                {option}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        ) : setting.kind === "choice" ? (
          <Select value={value} onValueChange={onChange}>
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {setting.options?.map((option) => (
                <SelectItem key={option} value={option}>
                  {option}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <Input value={value} inputMode="numeric" onChange={(e) => onChange(e.target.value)} />
        )}
      </div>
    </div>
  )
}
