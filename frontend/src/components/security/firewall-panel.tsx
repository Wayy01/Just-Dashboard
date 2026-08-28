"use client"

import { useState } from "react"
import { AlertTriangle, Lock, Pencil, RotateCcw, Shield, ShieldAlert, Trash2 } from "lucide-react"
import { notify } from "@/lib/toast"
import { del, post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { FirewallRule, FirewallStatus, Posture, SecurityFinding } from "@/lib/types"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Metric } from "@/components/page"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { AreaFindings } from "@/components/security/posture-panel"
import { AddRuleDialog, EditRuleDialog } from "@/components/security/rule-form"
import { Status } from "@/components/status-dot"
import { Badge } from "@/components/ui/badge"
import { IconAction } from "@/components/icon-action"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
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
 * The firewall, with the two controls that decide what it actually does.
 *
 * A rule list on its own is only half the picture: the default policy is what
 * happens to everything the list did not mention, and a list of allows in
 * front of a default of allow is decoration. The other half is logging — a
 * firewall that drops silently leaves an incident with no record of what was
 * refused, and "off" should be a choice somebody made rather than one they
 * inherited.
 */
export function FirewallPanel({
  status,
  posture,
  loading,
  error,
  refresh,
  onFix,
}: {
  status: FirewallStatus | undefined
  posture: Posture | undefined
  loading: boolean
  error: Error | undefined
  refresh: () => void
  onFix?: (finding: SecurityFinding) => void
}) {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [editing, setEditing] = useState<FirewallRule | null>(null)
  const admin = can("system.admin")

  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />
  if (!status?.available) {
    return (
      <EmptyState
        icon={Shield}
        title="No firewall on this host"
        description={
          status?.error ??
          "Neither ufw, firewalld nor iptables answered. Install one — without it, every port anything on this machine opens is reachable from wherever the machine is."
        }
      />
    )
  }

  // Controls are keyed off what the backend says it can do rather than off its
  // name, so adding a fourth firewall does not mean revisiting this file.
  const caps = status.capabilities ?? {
    editable: false,
    toggle: false,
    defaultPolicy: false,
    logging: false,
    reset: false,
    profiles: false,
  }
  const writable = admin && caps.editable
  // ufw prints every rule twice on a dual-stack host and distinguishes the
  // pair only by a "(v6)" suffix. Folding the duplicate away is what keeps
  // eight rules from reading as sixteen.
  const rules = status.rules.filter((r) => !r.ipv6)
  const hidden = status.rules.length - rules.length

  const setPolicy = (direction: string, policy: string) => {
    const risky = direction === "incoming" && policy !== "allow"
    const send = (c?: string) =>
      post("/firewall/policy", { direction, policy }, c ? { confirm: c } : undefined)
    if (!risky) {
      send()
        .then(() => {
          notify.success(`Default ${direction} set to ${policy}`)
          refresh()
        })
        .catch((err) => notify.error("Not applied", err))
      return
    }
    confirm({
      title: "Deny inbound by default",
      phrase: "deny incoming",
      confirmLabel: "Apply",
      description: (
        <p className="text-destructive">
          Everything not covered by an allow rule stops being reachable, including this dashboard
          if no rule admits the port you are reading it on. The server refuses the change outright
          when there is no inbound allow rule at all.
        </p>
      ),
      action: async (c) => {
        await send(c)
        refresh()
      },
    })
  }

  return (
    <>
      <div className="flex min-w-0 flex-col gap-4">
        <AreaFindings posture={posture} area="firewall" onFix={onFix} />

        {caps.readOnlyReason && (
          <Notice icon={Lock} title={`${status.backend} can be read here, not changed`}>
            {caps.readOnlyReason}
          </Notice>
        )}

        <Notice icon={ShieldAlert} title="Lockout protection">
          A rule that would block the address you are connected from is refused before it is
          applied, and so is an inbound default of deny on a host with no allow rule at all. A
          firewall change should never be the thing that costs you access to the box.
        </Notice>

        <Panel>
          <PanelHeader
            icon={Shield}
            eyebrow={status.backend}
            title="Firewall"
            description={status.zone ? `zone ${status.zone}` : undefined}
            actions={
              <>
                <Status
                  verdict={status.enabled ? "ok" : "warning"}
                  label={status.enabled ? "Active" : "Inactive"}
                />
                {writable && (
                <>
                  <AddRuleDialog onDone={refresh} hasProfiles={caps.profiles} />
                  {caps.toggle && (
                  <Switch
                    aria-label="Firewall enabled"
                    checked={status.enabled}
                    onCheckedChange={(enabled) =>
                      confirm({
                        title: enabled ? "Enable firewall" : "Disable firewall",
                        phrase: enabled ? "enable firewall" : "disable firewall",
                        confirmLabel: enabled ? "Enable" : "Disable",
                        description: enabled ? (
                          <p className="text-destructive">
                            ufw applies its default-deny policy immediately. If the port this
                            dashboard listens on is not already allowed, you will lose access.
                          </p>
                        ) : (
                          <p className="text-destructive">
                            Every rule stops being enforced and the host is left unfiltered.
                          </p>
                        ),
                        action: async (c) => {
                          await post("/firewall/enabled", { enabled }, { confirm: c })
                          refresh()
                        },
                      })
                    }
                  />
                  )}
                </>
                )}
              </>
            }
          />
          {/* One strip whether or not this firewall can be changed from here.
              Read-only backends (iptables) used to show none of this — just a
              "·"-joined string in the description — so the same facts now read
              the same way: the default policy, the logging level and the rule
              count as figures, and where the backend can take an instruction,
              the figure becomes the control that sets it. */}
          <PanelToolbar className="gap-x-6 gap-y-3">
            {caps.defaultPolicy && writable ? (
              <PolicySelect
                label="Inbound"
                value={status.policy?.incoming}
                onChange={(v) => setPolicy("incoming", v)}
                hint="What happens to a connection no rule matched"
              />
            ) : (
              <Metric
                label="inbound default"
                value={policyLabel(status.policy?.incoming, status.defaultPolicy)}
                hint="a connection no rule matched"
              />
            )}
            {status.backend === "ufw" &&
              (caps.defaultPolicy && writable ? (
                <PolicySelect
                  label="Outbound"
                  value={status.policy?.outgoing}
                  onChange={(v) => setPolicy("outgoing", v)}
                  hint="What this host may reach"
                />
              ) : (
                <Metric
                  label="outbound default"
                  value={policyLabel(status.policy?.outgoing)}
                  hint="what this host may reach"
                />
              ))}
            {caps.logging && writable ? (
              <div className="space-y-1">
                <Label className="eyebrow">Logging</Label>
                <Select
                  value={loggingLevel(status.logging)}
                  onValueChange={(level) =>
                    post("/firewall/logging", { level })
                      .then(() => {
                        notify.success(`Logging set to ${level}`)
                        refresh()
                      })
                      .catch((err) => notify.error("Not applied", err))
                  }
                >
                  <SelectTrigger size="sm" className="w-32">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {["off", "low", "medium", "high", "full"].map((level) => (
                      <SelectItem key={level} value={level}>
                        {level}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            ) : (
              <Metric
                label="logging"
                value={loggingLevel(status.logging)}
                hint={
                  !status.logging || status.logging.startsWith("off")
                    ? "a silent drop leaves no record"
                    : undefined
                }
              />
            )}
            <Metric label="rules" value={rules.length} />
            <span className="flex-1" />
            {writable && caps.reset && (
              <Button
                size="sm"
                variant="ghost"
                className="self-center text-destructive"
                onClick={() =>
                  confirm({
                    title: "Reset the firewall",
                    phrase: "reset firewall",
                    confirmLabel: "Reset",
                    description: (
                      <p className="text-destructive">
                        Every rule is removed and ufw is disabled. There is no undo, and the host
                        is left unfiltered until you configure it again.
                      </p>
                    ),
                    action: async (c) => {
                      await post("/firewall/reset", {}, { confirm: c })
                      refresh()
                    },
                  })
                }
              >
                <RotateCcw className="size-3.5" />
                Reset
              </Button>
            )}
          </PanelToolbar>
          <PanelBody flush>
            <Table containerClassName="max-h-[calc(100svh-30rem)]">
              <TableHeader className={stickyTableHeader}>
                <TableRow>
                  <TableHead className="w-12">#</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>To</TableHead>
                  <TableHead>From</TableHead>
                  <TableHead className="w-full">Comment</TableHead>
                  <TableHead className="w-px" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((rule, i) => (
                  <TableRow
                    key={`${rule.number}-${i}`}
                    className={cn("group", rule.danger && "bg-destructive/[0.04]")}
                  >
                    <TableCell className="numeric font-mono text-xs">{rule.number}</TableCell>
                    <TableCell>
                      <Badge
                        variant="outline"
                        className={cn(
                          "font-mono font-normal",
                          // ALLOW / DENY / LIMIT is a property of the rule, not
                          // the app's running/stopped status language, so it
                          // stays a plain tag. Deny-family rules read quieter —
                          // an allow to the world is the line worth catching,
                          // and rule.danger already tints that row red.
                          rule.action !== "ALLOW" && "text-muted-foreground",
                        )}
                      >
                        {rule.action}
                      </Badge>
                      {rule.direction && (
                        <span className="ml-1 text-[11px] text-muted-foreground">
                          {rule.direction}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="text-xs">
                      <span className="font-mono">{rule.to || "—"}</span>
                      {rule.service && (
                        <span className="ml-1.5 text-muted-foreground">{rule.service}</span>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{rule.from || "anywhere"}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {rule.danger ? (
                        <span className="flex items-start gap-1.5 text-destructive">
                          <AlertTriangle className="mt-px size-3 shrink-0" />
                          <span>{rule.danger}</span>
                        </span>
                      ) : (
                        rule.comment
                      )}
                    </TableCell>
                    <TableCell className="whitespace-nowrap">
                      {writable && rule.number !== undefined && (
                        <IconAction
                          label="Edit rule"
                          className="opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                          onClick={() => setEditing(rule)}
                        >
                          <Pencil />
                        </IconAction>
                      )}
                      {writable && rule.number !== undefined && (
                        <IconAction
                          label="Delete rule"
                          className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                          onClick={() =>
                            confirm({
                              // No typed phrase. A rule is one line, visible on
                              // the row being deleted and re-addable from the
                              // form beside it — and a phrase in front of
                              // something done a dozen times a day is a phrase
                              // that gets typed without being read, which is
                              // what makes the phrase worthless on the routes
                              // that keep it.
                              title: "Delete firewall rule",
                              confirmLabel: "Delete",
                              description: <p className="font-mono text-xs">{rule.raw}</p>,
                              action: async (c) => {
                                await del(`/firewall/rules/${rule.number}`, { confirm: c })
                                refresh()
                              },
                            })
                          }
                        >
                          <Trash2 />
                        </IconAction>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
                {rules.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={6} className="p-0">
                      <EmptyState icon={Shield} title="No rules configured" />
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </PanelBody>
          {hidden > 0 && (
            <div className="border-t border-hairline px-4 py-2 text-[11px] text-muted-foreground">
              {hidden} IPv6 {hidden === 1 ? "counterpart" : "counterparts"} folded away — ufw
              writes each rule into both tables and prints them separately.
            </div>
          )}
        </Panel>
      </div>
      {dialog}
      {editing && (
        <EditRuleDialog
          rule={editing}
          open
          onOpenChange={(o) => !o && setEditing(null)}
          onDone={refresh}
          hasProfiles={caps.profiles}
        />
      )}
    </>
  )
}

function PolicySelect({
  label,
  value,
  onChange,
  hint,
}: {
  label: string
  value: string | undefined
  onChange: (value: string) => void
  hint: string
}) {
  return (
    <div className="space-y-1" title={hint}>
      <Label className="eyebrow">{label}</Label>
      <Select value={value ?? "allow"} onValueChange={onChange}>
        <SelectTrigger size="sm" className="w-28">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="deny">deny</SelectItem>
          <SelectItem value="reject">reject</SelectItem>
          <SelectItem value="allow">allow</SelectItem>
        </SelectContent>
      </Select>
    </div>
  )
}

/**
 * The default policy as a word. Prefer the structured field the write controls
 * also read, so a read-only backend and a writable one describe the same thing
 * the same way; fall back to the raw string ufw's verbose output carries.
 */
function policyLabel(structured: string | undefined, raw?: string) {
  return structured || raw || "—"
}

/** ufw prints "on (low)" or "off"; the control offers the level. */
function loggingLevel(logging: string | undefined) {
  if (!logging || logging.startsWith("off")) return "off"
  const match = logging.match(/\(([^)]+)\)/)
  return match ? match[1] : "low"
}
