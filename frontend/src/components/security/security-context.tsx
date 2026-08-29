"use client"

import { createContext, useContext } from "react"
import type { FirewallStatus, Posture, SecurityFinding } from "@/lib/types"

/**
 * The two verdicts the whole Security section is built around.
 *
 * `posture` (slow) and `firewall` (fast) are read on every sub-page — the
 * Overview shows both, the Firewall page shows the rules and the findings
 * about them, the SSH page shows its findings — so the layout polls each once
 * and the pages read them here rather than each starting its own poll.
 *
 * `applyFix` lives here too, not copied into each page: it is the whole
 * difference between a warning and a remedy, and the two on it that can cost
 * access to the machine (enabling the firewall, changing sshd) go through the
 * typed-phrase confirmation. A second copy is where one of them quietly stops
 * asking.
 */
export type SecurityContextValue = {
  posture: Posture | undefined
  postureLoading: boolean
  firewall: FirewallStatus | undefined
  firewallLoading: boolean
  firewallError: Error | undefined
  refreshPosture: () => void
  refreshFirewall: () => void
  /** A finding's server-named remedy, wrapped in the confirmation it deserves. */
  applyFix: (finding: SecurityFinding) => void
}

const SecurityContext = createContext<SecurityContextValue | null>(null)

export const SecurityProvider = SecurityContext.Provider

export function useSecurity() {
  const value = useContext(SecurityContext)
  if (!value) throw new Error("useSecurity must be used inside the security layout")
  return value
}
