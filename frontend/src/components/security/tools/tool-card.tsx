"use client"

import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Notice, Spinner } from "@/components/state"
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
import type { ToolDef } from "./tool-defs"
import { useToolRun } from "./use-tool-run"
import { ToolResult } from "./tool-result"

/**
 * One network tool: its own inputs, its own run, its own answer.
 *
 * Cards share nothing — each mounts its own `useToolRun`, so a target typed
 * into DNS stays in DNS when the port check runs, and a slow traceroute
 * never disables another card's Run button.
 */
export function ToolCard({ def }: { def: ToolDef }) {
  const t = useToolRun(def)
  const base = `tool-${def.key}`

  return (
    <Panel>
      <PanelHeader title={def.label} description={def.hint} />
      <PanelBody className="space-y-3">
        <div className="flex flex-wrap items-end gap-3">
          {def.needsTarget && (
            <div className="min-w-0 flex-1 space-y-1.5">
              <Label htmlFor={`${base}-target`}>Target</Label>
              <Input
                id={`${base}-target`}
                value={t.target}
                onChange={(e) => t.setTarget(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && t.canRun && t.run()}
                placeholder={def.targetPlaceholder ?? "example.com or 203.0.113.9"}
                className="font-mono text-xs"
              />
            </div>
          )}
          {def.recordOptions && (
            <div className="w-32 space-y-1.5">
              <Label>Record</Label>
              <Select value={t.record} onValueChange={t.setRecord}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {def.recordOptions.map((r) => (
                    <SelectItem key={r} value={r}>
                      {r}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
          {def.optionOptions && (
            <div className="w-32 space-y-1.5">
              <Label>{def.optionLabel ?? "Option"}</Label>
              <Select value={t.option} onValueChange={t.setOption}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {def.optionOptions.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
          {def.needsPort && (
            <div className="w-28 space-y-1.5">
              <Label htmlFor={`${base}-port`}>Port</Label>
              <Input
                id={`${base}-port`}
                value={t.port}
                inputMode="numeric"
                onChange={(e) => t.setPort(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && t.canRun && t.run()}
              />
            </div>
          )}
          <div className="flex items-center gap-1.5">
            <Button onClick={t.run} disabled={t.busy || !t.canRun}>
              {t.busy && <Spinner className="size-4" />}
              Run
            </Button>
            {(t.result || t.past.length > 0) && (
              <Button variant="ghost" size="sm" onClick={t.clear} disabled={t.busy}>
                Clear
              </Button>
            )}
          </div>
        </div>

        {def.outward && (
          <Notice title="This checks outward, not inward">
            It proves what <b>this server</b> can reach, not what can reach it. Pointed at your
            own public address the traffic can hairpin or be admitted by rules that never apply
            to an outside visitor, so an open port here is not proof of exposure — the Ports tab
            and the exposure grade are.
          </Notice>
        )}

        {t.result && <ToolResult result={t.result} />}

        {t.past.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-[11px] text-muted-foreground">Previous:</span>
            {t.past.map((p) => (
              <button
                key={`${p.target}-${p.duration}`}
                type="button"
                onClick={() => t.restore(p)}
                className="rounded-md border border-hairline px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground"
              >
                <Badge
                  variant={p.ok ? "success" : "destructive"}
                  className="mr-1.5 font-normal"
                >
                  {p.ok ? "✓" : "✗"}
                </Badge>
                {p.target} · {p.duration}
              </button>
            ))}
          </div>
        )}
      </PanelBody>
    </Panel>
  )
}
