"use client"

import { useState } from "react"
import { Crosshair } from "@/components/icons"
import { notify } from "@/lib/toast"
import { post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { ProbeResult } from "@/lib/types"
import { useAuth } from "@/hooks/use-auth"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { EmptyState, Notice, Spinner } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

const TOOLS = [
  { key: "dns", label: "DNS", hint: "Resolve a name using this host's own resolver" },
  { key: "ping", label: "Ping", hint: "Can this server reach that host at all" },
  { key: "traceroute", label: "Traceroute", hint: "What is between them" },
  { key: "port", label: "Port check", hint: "Can this server open a TCP connection there" },
  { key: "scan", label: "Port scan", hint: "Which of the common service ports answer on that host" },
  { key: "http", label: "HTTP", hint: "What that host serves — status, redirects and headers" },
  { key: "tls", label: "TLS cert", hint: "The certificate a TLS port presents, and whether it is trusted" },
  { key: "whois", label: "Whois", hint: "Registration details for a domain or address" },
] as const

// Which tools take a port, and so show the port field.
const PORT_TOOLS = new Set(["port", "http", "tls"])
// Which tools reach outward from this host, and so carry the directionality
// warning: they prove what the server can reach, never what can reach it.
const OUTWARD_TOOLS = new Set(["port", "scan"])

/**
 * The tools an operator opens a terminal for, on the page where the question
 * arose.
 *
 * "Is this domain pointing at me yet", "can this box reach that host", "what
 * is in the way" — three questions that come up constantly while setting up a
 * proxy or debugging a firewall rule, and every panel in this class makes you
 * leave and find a shell.
 */
export function ToolsPanel() {
  const { can } = useAuth()
  const [tool, setTool] = useState<string>("dns")
  const [target, setTarget] = useState("")
  const [record, setRecord] = useState("A")
  const [port, setPort] = useState("443")
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<ProbeResult | null>(null)

  if (!can("system.admin")) {
    return (
      <EmptyState
        icon={Crosshair}
        title="Diagnostics need the admin capability"
        description="A probe makes the server send traffic to an address the caller chose, which is a scanner if it is handed to everybody."
      />
    )
  }

  const run = async () => {
    setBusy(true)
    setResult(null)
    try {
      setResult(
        await post<ProbeResult>("/network/probe", {
          tool,
          target: target.trim(),
          record,
          port: Number(port) || 0,
        }),
      )
    } catch (err) {
      notify.error("Could not run the probe", err)
    } finally {
      setBusy(false)
    }
  }

  const active = TOOLS.find((t) => t.key === tool)

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Panel>
        <PanelHeader icon={Crosshair} title="Network tools" description={active?.hint} />
        <PanelToolbar>
          <ToggleGroup
            type="single"
            value={tool}
            onValueChange={(next) => next && setTool(next)}
            variant="outline"
            size="sm"
            aria-label="Which tool to run"
            className="flex-wrap"
          >
            {TOOLS.map((t) => (
              <ToggleGroupItem key={t.key} value={t.key} className="px-2.5 text-[11px]">
                {t.label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </PanelToolbar>
        <PanelBody className="space-y-3">
          <div className="flex flex-wrap items-end gap-3">
            <div className="min-w-0 flex-1 space-y-1.5">
              <Label htmlFor="probe-target">Target</Label>
              <Input
                id="probe-target"
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && target && run()}
                placeholder="example.com or 203.0.113.9"
                className="font-mono text-xs"
              />
            </div>
            {tool === "dns" && (
              <div className="w-32 space-y-1.5">
                <Label>Record</Label>
                <Select value={record} onValueChange={setRecord}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {["A", "AAAA", "CNAME", "MX", "TXT", "NS", "PTR"].map((r) => (
                      <SelectItem key={r} value={r}>
                        {r}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            {PORT_TOOLS.has(tool) && (
              <div className="w-28 space-y-1.5">
                <Label htmlFor="probe-port">Port</Label>
                <Input
                  id="probe-port"
                  value={port}
                  inputMode="numeric"
                  onChange={(e) => setPort(e.target.value)}
                />
              </div>
            )}
            <Button onClick={run} disabled={busy || !target.trim()}>
              {busy && <Spinner className="size-4" />}
              Run
            </Button>
          </div>

          {OUTWARD_TOOLS.has(tool) && (
            <Notice title="This checks outward, not inward">
              It proves what <b>this server</b> can reach, not what can reach it. Pointed at your
              own public address the traffic can hairpin or be admitted by rules that never apply
              to an outside visitor, so an open port here is not proof of exposure — the Ports tab
              and the exposure grade are.
            </Notice>
          )}

          {result && (
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={result.ok ? "success" : "destructive"} className="font-normal">
                  {result.ok ? "answered" : "no answer"}
                </Badge>
                <span className="font-mono text-xs">{result.target}</span>
                <span className="text-[11px] text-muted-foreground">{result.duration}</span>
              </div>
              {result.records && result.records.length > 0 && (
                <div className="flex flex-wrap gap-1.5">
                  {result.records.map((r) => (
                    <Badge key={r} variant="outline" className="font-mono font-normal">
                      {r}
                    </Badge>
                  ))}
                </div>
              )}
              <pre
                className={cn(
                  "max-h-80 overflow-auto rounded-lg border border-hairline bg-surface-sunken p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap",
                  !result.ok && "text-muted-foreground",
                )}
              >
                {result.output || result.error || "No output."}
              </pre>
            </div>
          )}
        </PanelBody>
      </Panel>
    </div>
  )
}
