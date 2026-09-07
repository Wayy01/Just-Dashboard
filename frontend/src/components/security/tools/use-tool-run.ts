"use client"

import { useState } from "react"
import { notify } from "@/lib/toast"
import { post } from "@/lib/api"
import type { ProbeResult } from "@/lib/types"
import type { ToolDef } from "./tool-defs"

/**
 * One card's whole state: its inputs, its run, its current result and the
 * three runs before it.
 *
 * A hook instance per card is what makes the tools independent — no shared
 * target, no shared busy flag, and one card's slow traceroute never blocks
 * another card's quick DNS lookup.
 */
export function useToolRun(def: ToolDef) {
  const [target, setTarget] = useState("")
  const [port, setPort] = useState(def.portDefault ?? "443")
  const [record, setRecord] = useState(def.recordOptions?.[0] ?? "")
  const [option, setOption] = useState(def.optionDefault ?? def.optionOptions?.[0]?.value ?? "")
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<ProbeResult | null>(null)
  const [past, setPast] = useState<ProbeResult[]>([])

  const canRun = !def.needsTarget || target.trim().length > 0

  const run = async () => {
    if (busy || !canRun) return
    setBusy(true)
    try {
      const res = await post<ProbeResult>("/network/probe", {
        tool: def.key,
        target: target.trim(),
        ...(def.needsPort ? { port: Number(port) || 0 } : {}),
        ...(def.recordOptions ? { record } : {}),
        ...(def.optionOptions ? { option } : {}),
      })
      setPast((prev) => (result ? [result, ...prev].slice(0, 3) : prev))
      setResult(res)
    } catch (err) {
      notify.error(`Could not run ${def.label}`, err)
    } finally {
      setBusy(false)
    }
  }

  const restore = (res: ProbeResult) => {
    setPast((prev) => (result ? [result, ...prev.filter((p) => p !== res)].slice(0, 3) : prev))
    setResult(res)
  }

  const clear = () => {
    setResult(null)
    setPast([])
  }

  return {
    target,
    setTarget,
    port,
    setPort,
    record,
    setRecord,
    option,
    setOption,
    busy,
    result,
    past,
    canRun,
    run,
    restore,
    clear,
  }
}

export type ToolRun = ReturnType<typeof useToolRun>
