"use client"

import { AlertTriangle, Lock, RotateCcw, Shield, ShieldAlert, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { del, post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { FirewallStatus, Posture } from "@/lib/types"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel, Notice } from "@/components/state"
import { AreaFindings } from "@/components/security/posture-panel"
import { AddRuleDialog } from "@/components/security/rule-form"
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
}: {
  status: FirewallStatus | undefined
  posture: Posture | undefined
  loading: boolean
  error: Error | undefined
  refresh: () => void
}) {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
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
          toast.success(`Default ${direction} set to ${policy}`)
          refresh()
        })
        .catch((err) => toast.error("Not applied", { description: String(err) }))
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
        <AreaFindings posture={posture} area="firewall" />

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
            title={`${status.backend} · ${status.enabled ? "active" : "inactive"}`}
            description={
              [status.defaultPolicy ?? "no default policy reported", status.zone && `zone ${status.zone}`]
                .filter(Boolean)
                .join(" · ")
            }
            actions={
              writable && (
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
              )
            }
          />
          {writable && (caps.defaultPolicy || caps.logging || caps.reset) && (
            <PanelToolbar className="gap-4">
              {caps.defaultPolicy && (
                <PolicySelect
                  label="Inbound"
                  value={status.policy?.incoming}
                  onChange={(v) => setPolicy("incoming", v)}
                  hint="What happens to a connection no rule matched"
                />
              )}
              {caps.defaultPolicy && status.backend === "ufw" && (
                <PolicySelect
                  label="Outbound"
                  value={status.policy?.outgoing}
                  onChange={(v) => setPolicy("outgoing", v)}
                  hint="What this host may reach"
                />
              )}
              {caps.logging && (
              <div className="space-y-1">
                <Label className="eyebrow">Logging</Label>
                <Select
                  value={loggingLevel(status.logging)}
                  onValueChange={(level) =>
                    post("/firewall/logging", { level })
                      .then(() => {
                        toast.success(`Logging set to ${level}`)
                        refresh()
                      })
                      .catch((err) => toast.error("Not applied", { description: String(err) }))
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
              )}
              <span className="flex-1" />
              {caps.reset && (
              <Button
                size="sm"
                variant="ghost"
                className="text-destructive"
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
          )}
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
                        variant={
                          rule.action === "ALLOW"
                            ? "success"
                            : rule.action === "LIMIT"
                              ? "warning"
                              : "destructive"
                        }
                        className="font-normal"
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
                    <TableCell>
                      {writable && rule.number !== undefined && (
                        <IconAction
                          label="Delete rule"
                          className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                          onClick={() =>
                            confirm({
                              title: "Delete firewall rule",
                              phrase: `delete rule ${rule.number}`,
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

/** ufw prints "on (low)" or "off"; the control offers the level. */
function loggingLevel(logging: string | undefined) {
  if (!logging || logging.startsWith("off")) return "off"
  const match = logging.match(/\(([^)]+)\)/)
  return match ? match[1] : "low"
}
