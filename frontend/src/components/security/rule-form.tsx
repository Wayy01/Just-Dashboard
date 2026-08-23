"use client"

import { useMemo, useState } from "react"
import { AlertTriangle, Plus } from "lucide-react"
import { toast } from "sonner"
import { get, post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { AppProfile, ServicePreset } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { Notice } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"

/**
 * Opening a port, for somebody who does not already know the numbers.
 *
 * The old form was an action, a port box and a source box — which assumes the
 * reader knows that Redis is 6379 and, more importantly, that opening it is
 * the same as handing over the machine. The catalogue comes from the server so
 * the names and the warnings are the same list the security audit reads, and
 * the warning appears at the moment of choosing rather than in a report
 * afterwards.
 *
 * Application profiles are the other half: a rule written as "Nginx Full" is
 * the host's own package speaking, and it keeps meaning what it says if that
 * package later adds a port.
 */
export function AddRuleDialog({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <Plus className="size-4" />
          Add rule
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        {/* Keyed so a second visit never opens with the previous rule still
            in the boxes — an almost-right rule is worse than a blank one. */}
        {open && (
          <RuleForm
            key="rule-form"
            onDone={() => {
              setOpen(false)
              onDone()
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

const SOURCE_PRESETS = [
  { key: "anywhere", label: "Anywhere", value: "", hint: "Every address on the internet" },
  { key: "private", label: "Private network", value: "10.0.0.0/8", hint: "RFC1918 — adjust to your range" },
  { key: "tailnet", label: "Tailnet", value: "100.64.0.0/10", hint: "Tailscale's address range" },
  { key: "custom", label: "Specific address", value: "", hint: "One IP or a CIDR" },
] as const

function RuleForm({ onDone }: { onDone: () => void }) {
  const [action, setAction] = useState("allow")
  const [mode, setMode] = useState<"service" | "profile">("service")
  const [preset, setPreset] = useState("")
  const [profile, setProfile] = useState("")
  const [port, setPort] = useState("")
  const [protocol, setProtocol] = useState("tcp")
  const [sourceKind, setSourceKind] = useState<string>("anywhere")
  const [from, setFrom] = useState("")
  const [comment, setComment] = useState("")
  const [busy, setBusy] = useState(false)

  const services = usePoll<ServicePreset[]>(
    (signal) => get("/security/services", undefined, signal),
    0,
  )
  const profiles = usePoll<AppProfile[]>((signal) => get("/firewall/apps", undefined, signal), 0)

  const chosen = useMemo(
    () => services.data?.find((s) => s.key === preset),
    [services.data, preset],
  )
  const source = sourceKind === "custom" ? from : (SOURCE_PRESETS.find((p) => p.key === sourceKind)?.value ?? "")
  const unrestricted = source === ""
  const dangerous = action === "allow" && unrestricted && !!chosen?.danger

  const applyPreset = (key: string) => {
    setPreset(key)
    const found = services.data?.find((s) => s.key === key)
    if (found) {
      setPort(found.port)
      setProtocol(found.protocol)
      if (!comment) setComment(found.name)
    }
  }

  const submit = async () => {
    setBusy(true)
    try {
      await post("/firewall/rules", {
        action,
        direction: "in",
        port: mode === "service" ? port : "",
        protocol: mode === "service" ? protocol : "",
        app: mode === "profile" ? profile : "",
        from: source,
        comment,
      })
      toast.success("Rule added")
      onDone()
    } catch (err) {
      toast.error("Rule rejected", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }

  const ready = mode === "service" ? port !== "" : profile !== ""

  return (
    <>
      <DialogHeader>
        <DialogTitle>New inbound rule</DialogTitle>
        <DialogDescription>
          Rules are checked in order and the first match wins. A rule that would block the address
          you are connected from is refused before it is applied.
        </DialogDescription>
      </DialogHeader>

      <div className="grid gap-3">
        <div className="space-y-1.5">
          <Label>Action</Label>
          <Select value={action} onValueChange={setAction}>
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="allow">allow — let it through</SelectItem>
              <SelectItem value="limit">limit — allow, but rate-limit repeat connections</SelectItem>
              <SelectItem value="deny">deny — drop silently</SelectItem>
              <SelectItem value="reject">reject — refuse and say so</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <Tabs value={mode} onValueChange={(v) => setMode(v as "service" | "profile")}>
          <TabsList className="w-full">
            <TabsTrigger value="service" className="flex-1">
              Port
            </TabsTrigger>
            <TabsTrigger value="profile" className="flex-1" disabled={!profiles.data?.length}>
              Application profile
            </TabsTrigger>
          </TabsList>

          <TabsContent value="service" className="mt-3 space-y-3">
            <div className="space-y-1.5">
              <Label>Service</Label>
              <Select value={preset} onValueChange={applyPreset}>
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="Pick one, or type a port below" />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectLabel>Common</SelectLabel>
                    {services.data
                      ?.filter((s) => !s.danger)
                      .map((s) => (
                        <SelectItem key={s.key} value={s.key}>
                          {s.name} · {s.port}/{s.protocol}
                        </SelectItem>
                      ))}
                  </SelectGroup>
                  <SelectGroup>
                    <SelectLabel>Keep these off the internet</SelectLabel>
                    {services.data
                      ?.filter((s) => s.danger)
                      .map((s) => (
                        <SelectItem key={s.key} value={s.key}>
                          {s.name} · {s.port}/{s.protocol}
                        </SelectItem>
                      ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {chosen && <p className="text-[11px] text-muted-foreground">{chosen.detail}</p>}
            </div>

            <div className="grid gap-3 sm:grid-cols-[1fr_8rem]">
              <div className="space-y-1.5">
                <Label htmlFor="rule-port">Port, range or list</Label>
                <Input
                  id="rule-port"
                  value={port}
                  onChange={(e) => {
                    setPort(e.target.value)
                    setPreset("")
                  }}
                  placeholder="443, 8000:8010 or 80,443"
                />
              </div>
              <div className="space-y-1.5">
                <Label>Protocol</Label>
                <Select value={protocol} onValueChange={setProtocol}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="tcp">tcp</SelectItem>
                    <SelectItem value="udp">udp</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
          </TabsContent>

          <TabsContent value="profile" className="mt-3 space-y-1.5">
            <Label>Profile</Label>
            <Select value={profile} onValueChange={setProfile}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Installed by the host's own packages" />
              </SelectTrigger>
              <SelectContent>
                {profiles.data?.map((p) => (
                  <SelectItem key={p.name} value={p.name}>
                    {p.name}
                    {p.ports.length > 0 && ` · ${p.ports.join(" ")}`}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-[11px] text-muted-foreground">
              A profile names its own ports, so the rule keeps meaning what it says if the package
              later adds one.
            </p>
          </TabsContent>
        </Tabs>

        <div className="space-y-1.5">
          <Label>Source</Label>
          <div className="flex flex-wrap gap-1.5">
            {SOURCE_PRESETS.map((p) => (
              <Button
                key={p.key}
                type="button"
                size="xs"
                variant={sourceKind === p.key ? "default" : "outline"}
                onClick={() => {
                  setSourceKind(p.key)
                  setFrom(p.value)
                }}
              >
                {p.label}
              </Button>
            ))}
          </div>
          {sourceKind === "custom" || sourceKind === "private" || sourceKind === "tailnet" ? (
            <Input
              value={sourceKind === "custom" ? from : source}
              onChange={(e) => {
                setSourceKind("custom")
                setFrom(e.target.value)
              }}
              placeholder="10.0.0.0/8 or 203.0.113.9"
              className="font-mono text-xs"
            />
          ) : (
            <p className="text-[11px] text-muted-foreground">
              {SOURCE_PRESETS.find((p) => p.key === sourceKind)?.hint}
            </p>
          )}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="rule-comment">Comment</Label>
          <Input
            id="rule-comment"
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder="What this is for"
          />
        </div>

        {dangerous && (
          <Notice tone="danger" icon={AlertTriangle} title={`${chosen?.name} open to the internet`}>
            {chosen?.danger} Choose a source above, or bind the service to 127.0.0.1 instead of
            opening the port at all.
          </Notice>
        )}
      </div>

      <DialogFooter className="flex-col gap-2 sm:flex-row sm:items-center">
        <span className={cn("mr-auto text-[11px] text-muted-foreground", !ready && "opacity-0")}>
          <Badge variant="outline" className="font-mono font-normal">
            ufw {action} in{source && ` from ${source}`}
            {mode === "service" ? ` to any port ${port}${protocol ? ` proto ${protocol}` : ""}` : ` app ${profile}`}
          </Badge>
        </span>
        <Button onClick={submit} disabled={!ready || busy}>
          Add rule
        </Button>
      </DialogFooter>
    </>
  )
}
