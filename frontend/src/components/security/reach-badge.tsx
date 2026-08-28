import { Globe } from "lucide-react"
import { Badge } from "@/components/ui/badge"

/**
 * How far a thing on this host can be reached from — an interface's address, a
 * connected peer's origin. One rendering, used by both the Network and the
 * Connections page, so the same distinction does not read two different ways
 * across the section.
 *
 * It is a *tag* (a fixed property of the row), not the app's status vocabulary,
 * so it stays a `Badge`: `warning` for the internet because that is the one
 * worth looking at, and everything private stays quiet so a column of them does
 * not compete with it.
 */
export function ReachBadge({ scope }: { scope: "internet" | "private" | "local" }) {
  if (scope === "internet") {
    return (
      <Badge variant="warning" className="font-normal">
        <Globe className="size-3" />
        internet
      </Badge>
    )
  }
  if (scope === "local") {
    return (
      <Badge variant="outline" className="font-normal">
        local only
      </Badge>
    )
  }
  return (
    <Badge variant="secondary" className="font-normal">
      private
    </Badge>
  )
}
