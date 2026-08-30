"use client"

import { useState } from "react"
import { Cross, SettingsSliders, Slash } from "@/components/icons"
import { notify } from "@/lib/toast"
import { get, post } from "@/lib/api"
import type { Fail2banJail, JailConfig, JailParamResult } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Notice } from "@/components/state"
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
} from "@/components/ui/dialog"

/**
 * One jail, with the knobs that decide whether it is doing anything.
 *
 * A dashboard that can only unban one address at a time is a viewer with a
 * button. The three numbers below — how many failures, in what window, for how
 * long — are the whole policy, and they live in a file whose layout differs by
 * distribution. fail2ban-client can set them on the running server, which is
 * both easier to get right and honest about what is in force now rather than
 * what a file says.
 */
export function JailPanel({
  jail,
  canManage,
  onChanged,
}: {
  jail: Fail2banJail
  canManage: boolean
  onChanged: () => void
}) {
  const [tuning, setTuning] = useState(false)
  const [busy, setBusy] = useState(false)

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true)
    try {
      await fn()
      notify.success(ok)
      onChanged()
    } catch (err) {
      notify.error("Could not apply", err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Panel>
      <PanelHeader
        icon={Slash}
        title={jail.name}
        description={`${jail.currentlyBanned} banned now · ${jail.totalBanned} total · ${jail.currentlyFailed} failing`}
        actions={
          canManage && (
            <>
              <Button size="xs" variant="ghost" onClick={() => setTuning(true)}>
                <SettingsSliders className="size-3.5" />
                Tune
              </Button>
              <Button
                size="xs"
                variant="ghost"
                disabled={busy || jail.bannedIps.length === 0}
                onClick={() =>
                  act(
                    () => post(`/fail2ban/${encodeURIComponent(jail.name)}/unban-all`, {}),
                    `Released every ban in ${jail.name}`,
                  )
                }
              >
                Release all
              </Button>
            </>
          )
        }
      />
      <PanelBody>
        {jail.bannedIps.length === 0 ? (
          <p className="text-[13px] text-muted-foreground">
            Nothing currently banned. Bans expire, so an empty list is not the same as a quiet
            night — the ban activity below is the record.
          </p>
        ) : (
          <div className="space-y-0.5">
            {jail.bannedIps.map((ip) => (
              <div
                key={ip}
                className="flex items-center justify-between gap-2 rounded-md px-2 py-1 hover:bg-[var(--row-hover)]"
              >
                <span className="font-mono text-xs">{ip}</span>
                {canManage && (
                  <span className="flex items-center gap-1">
                    <Button
                      size="xs"
                      variant="ghost"
                      disabled={busy}
                      onClick={() =>
                        act(
                          () => post(`/fail2ban/${encodeURIComponent(jail.name)}/unban`, { ip }),
                          `${ip} unbanned`,
                        )
                      }
                    >
                      Unban
                    </Button>
                    <Button
                      size="xs"
                      variant="ghost"
                      disabled={busy}
                      title="Never ban this address again"
                      onClick={() =>
                        act(
                          () =>
                            post(`/fail2ban/${encodeURIComponent(jail.name)}/ignore`, {
                              ip,
                              add: true,
                            }),
                          `${ip} added to the allowlist`,
                        )
                      }
                    >
                      Allowlist
                    </Button>
                  </span>
                )}
              </div>
            ))}
          </div>
        )}
      </PanelBody>
      <JailTuning
        jail={jail.name}
        open={tuning}
        onOpenChange={setTuning}
        onSaved={onChanged}
      />
    </Panel>
  )
}

function JailTuning({
  jail,
  open,
  onOpenChange,
  onSaved,
}: {
  jail: string
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { data, refresh } = usePoll<JailConfig>(
    (signal) => get(`/fail2ban/${encodeURIComponent(jail)}/config`, undefined, signal),
    0,
    [jail],
    { enabled: open },
  )
  const [pending, setPending] = useState<Record<string, string>>({})
  const [ignore, setIgnore] = useState("")
  const [busy, setBusy] = useState(false)

  const value = (key: keyof JailConfig, fallback: number) =>
    pending[key] ?? String((data?.[key] as number | undefined) ?? fallback)

  const save = async () => {
    setBusy(true)
    try {
      // One request for the whole policy: the three numbers are one setting —
      // this many failures inside this window earns this long a ban — and
      // sending them separately leaves a jail half-tuned if the tab closes.
      const params: Record<string, number> = {}
      for (const [param, raw] of Object.entries(pending)) {
        const parsed = Number(raw)
        if (Number.isFinite(parsed)) params[param] = parsed
      }
      const res = await post<JailParamResult>(
        `/fail2ban/${encodeURIComponent(jail)}/config`,
        { params },
      )
      if (res.warning) {
        notify.warning(`${jail} partly updated`, { description: res.warning })
      } else {
        notify.success(`${jail} updated`, {
          description: res.persisted
            ? `Applied now and written to ${res.file}, so it survives a restart.`
            : undefined,
        })
      }
      setPending({})
      refresh()
      onSaved()
    } catch (err) {
      notify.error("Could not update the jail", err)
    } finally {
      setBusy(false)
    }
  }

  const addIgnore = async () => {
    try {
      await post(`/fail2ban/${encodeURIComponent(jail)}/ignore`, { ip: ignore, add: true })
      setIgnore("")
      refresh()
    } catch (err) {
      notify.error("Could not allowlist", err)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Tune {jail}</DialogTitle>
          <DialogDescription>
            This many failures inside this window earns a ban of this length.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="grid gap-3 sm:grid-cols-3">
            <NumberField
              label="Failures"
              hint="before a ban"
              value={value("maxRetry", 5)}
              onChange={(v) => setPending((p) => ({ ...p, maxretry: v }))}
            />
            <NumberField
              label="Window"
              hint="seconds"
              value={value("findTime", 600)}
              onChange={(v) => setPending((p) => ({ ...p, findtime: v }))}
            />
            <NumberField
              label="Ban for"
              hint="seconds"
              value={value("banTime", 600)}
              onChange={(v) => setPending((p) => ({ ...p, bantime: v }))}
            />
          </div>

          <Notice title="In force until fail2ban restarts">
            The change is applied to the running server, not written to jail.local — that file&rsquo;s
            layout differs by distribution, and getting it wrong disables intrusion prevention
            without saying so. Tighten it here, then make it permanent in the file.
          </Notice>

          <div className="space-y-1.5">
            <Label>Never ban</Label>
            <div className="flex flex-wrap gap-1.5">
              {data?.ignoreIp.map((ip) => (
                <Badge key={ip} variant="outline" className="gap-1 font-mono font-normal">
                  {ip}
                  <button
                    type="button"
                    aria-label={`Stop allowlisting ${ip}`}
                    className="text-muted-foreground hover:text-destructive"
                    onClick={async () => {
                      try {
                        await post(`/fail2ban/${encodeURIComponent(jail)}/ignore`, {
                          ip,
                          add: false,
                        })
                        refresh()
                      } catch (err) {
                        notify.error("Could not remove", err)
                      }
                    }}
                  >
                    <Cross className="size-3" />
                  </button>
                </Badge>
              ))}
              {data?.ignoreIp.length === 0 && (
                <span className="text-[11px] text-muted-foreground">Nothing allowlisted.</span>
              )}
            </div>
            <div className="flex gap-2">
              <Input
                value={ignore}
                onChange={(e) => setIgnore(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && ignore && addIgnore()}
                placeholder="203.0.113.9 or 10.0.0.0/8"
                className="font-mono text-xs"
              />
              <Button size="sm" variant="outline" onClick={addIgnore} disabled={!ignore}>
                Add
              </Button>
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button onClick={save} disabled={busy || Object.keys(pending).length === 0}>
            Apply
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function NumberField({
  label,
  hint,
  value,
  onChange,
}: {
  label: string
  hint: string
  value: string
  onChange: (value: string) => void
}) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <Input value={value} inputMode="numeric" onChange={(e) => onChange(e.target.value)} />
      <p className="text-[11px] text-muted-foreground">{hint}</p>
    </div>
  )
}
