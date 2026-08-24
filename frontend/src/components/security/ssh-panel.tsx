"use client"

import { useMemo, useState } from "react"
import { AlertTriangle, CheckCircle2, KeyRound, TerminalSquare } from "lucide-react"
import { toast } from "sonner"
import { get, post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { SSHApplyResult, SSHDConfig, SSHSetting } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Panel, PanelBody, PanelFooter, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
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

  const dirty = useMemo(() => Object.keys(pending).length > 0, [pending])

  if (!admin) {
    return (
      <EmptyState
        icon={TerminalSquare}
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
        icon={TerminalSquare}
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
          const res = await post<SSHApplyResult>("/ssh/config", { settings: pending }, { confirm: c })
          if (res.reloadError) {
            toast.warning("Saved, but sshd was not reloaded", { description: res.reloadError })
          } else {
            toast.success("Applied and reloaded")
          }
          setPending({})
          refresh()
        } finally {
          setBusy(false)
        }
      },
    })

  return (
    <>
      <div className="flex min-w-0 flex-col gap-4">
        {noKeys && (
          <Notice tone="warning" icon={KeyRound} title="No account on this host has an SSH key">
            Password authentication cannot safely be turned off until one does — with no key
            anywhere, doing so would leave nobody a way in, and the server refuses the change for
            that reason. Add a key from the Users page first.
          </Notice>
        )}
        {data.hasMatchBlocks && (
          <Notice icon={AlertTriangle} title="This configuration has Match blocks">
            Some of these values are overridden for particular users or addresses. What is shown
            here is the unconditional configuration; the conditional parts are not editable from
            this page.
          </Notice>
        )}

        <Panel>
          <PanelHeader
            icon={TerminalSquare}
            title="SSH server"
            description={`Port ${data.ports.join(", ")} · read from ${data.source}`}
            actions={
              insecure === 0 ? (
                <Badge variant="success" className="font-normal">
                  <CheckCircle2 className="size-3" />
                  hardened
                </Badge>
              ) : (
                <Badge variant="warning" className="font-normal">
                  {insecure} {insecure === 1 ? "setting" : "settings"} below the recommendation
                </Badge>
              )
            }
          />
          <PanelBody className="space-y-2.5">
            {data.settings.map((setting) => (
              <div
                key={setting.key}
                className={cn(
                  "flex min-w-0 flex-wrap items-start justify-between gap-x-4 gap-y-2 rounded-lg border p-2.5",
                  changed(setting)
                    ? "border-primary/40 bg-primary/[0.06]"
                    : setting.secure
                      ? "border-hairline bg-surface-sunken"
                      : "border-warning/30 bg-warning/5",
                )}
              >
                <div className="min-w-0 flex-1 space-y-1">
                  <div className="flex flex-wrap items-baseline gap-x-2">
                    <span className="text-[13px] font-medium">{setting.label}</span>
                    <code className="font-mono text-[11px] text-muted-foreground">
                      {setting.key}
                    </code>
                    {!setting.secure && (
                      <span className="text-[11px] text-warning">
                        recommended: {setting.recommended}
                      </span>
                    )}
                  </div>
                  <p className="text-[11px] leading-relaxed text-muted-foreground">
                    {setting.detail}
                  </p>
                  {!setting.secure && setting.risk && (
                    <p className="text-[11px] leading-relaxed text-foreground/80">{setting.risk}</p>
                  )}
                </div>
                <div className={cn("shrink-0", setting.kind === "list" ? "w-full sm:w-72" : "w-40")}>
                  {setting.kind === "list" ? (
                    <Input
                      value={valueOf(setting)}
                      placeholder="deploy admin — empty allows everyone"
                      className="font-mono text-xs"
                      onChange={(e) => setPending((p) => ({ ...p, [setting.key]: e.target.value }))}
                    />
                  ) : setting.kind === "choice" ? (
                    <Select
                      value={valueOf(setting)}
                      onValueChange={(v) => setPending((p) => ({ ...p, [setting.key]: v }))}
                    >
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
                    <Input
                      value={valueOf(setting)}
                      inputMode="numeric"
                      onChange={(e) => setPending((p) => ({ ...p, [setting.key]: e.target.value }))}
                    />
                  )}
                </div>
              </div>
            ))}
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
            icon={KeyRound}
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
                    <KeyRound className="size-3" />
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
