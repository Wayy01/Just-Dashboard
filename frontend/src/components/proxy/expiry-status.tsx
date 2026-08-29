import type { Certificate } from "@/lib/types"
import { Status } from "@/components/status-dot"

/** A certificate's expiry as a verdict — a dot and a word, not a filled pill. */
export function ExpiryStatus({ cert }: { cert?: Certificate }) {
  if (!cert) return <Status state="created" label="unchecked" />
  if (cert.error) return <Status verdict="critical" label={cert.error.slice(0, 40)} />
  if (cert.expired) return <Status verdict="critical" label="expired" />
  if (cert.expiring) return <Status verdict="warning" label={`${cert.daysLeft}d left`} />
  return <Status verdict="ok" label={`${cert.daysLeft}d left`} />
}
