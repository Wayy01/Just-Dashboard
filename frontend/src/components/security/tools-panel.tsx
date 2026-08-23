"use client"

import { useState } from "react"
import { Loader2, Radar } from "lucide-react"
import { toast } from "sonner"
import { post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { ProbeResult } from "@/lib/types"
import { useAuth } from "@/hooks/use-auth"
import { Panel, PanelBody, PanelHeader, PanelToolbar } from "@/components/panel"
import { EmptyState, Notice } from "@/components/state"
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
] as const

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
        icon={Radar}
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
      toast.error("Could not run the probe", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }

  const active = TOOLS.find((t) => t.key === tool)

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Panel>
        <PanelHeader icon={Radar} title="Network tools" description={active?.hint} />
        <PanelToolbar>
          <ToggleGroup
            type="single"
            value={tool}
            onValueChange={(next) => next && setTool(next)}
            variant="outline"
            size="sm"
            aria-label="Which tool to run"
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
            {tool === "port" && (
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
              {busy && <Loader2 className="size-4 animate-spin" />}
              Run
            </Button>
          </div>

          {tool === "port" && (
            <Notice title="This checks outward, not inward">
              It proves the <b>server</b> can reach that address. Whether the internet can reach
              this server cannot be answered from inside it — the Ports tab and the exposure grade
              are the closest thing.
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
