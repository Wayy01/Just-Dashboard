"use client"

import { useState } from "react"
import { AlertTriangle, Loader2 } from "lucide-react"
import { toast } from "sonner"
import { ApiError } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

export type ConfirmRequest = {
  /** Short title, e.g. "Remove container". */
  title: string
  /** What will happen, in plain language. */
  description: React.ReactNode
  /** The exact phrase the server expects echoed in X-Confirm. */
  phrase: string
  confirmLabel?: string
  /** Runs the action; the typed phrase is passed through to the API call. */
  action: (confirm: string) => Promise<void>
  onDone?: () => void
}

/**
 * The typed-confirmation dialog. The phrase it collects is sent as X-Confirm
 * and independently re-checked by the server, so this is a usability guard
 * against a mis-click rather than the enforcement point.
 */
export function ConfirmDialog({
  request,
  onOpenChange,
}: {
  request: ConfirmRequest | null
  onOpenChange: (open: boolean) => void
}) {
  if (!request) return null
  // Keyed on the phrase so a second dialog never opens pre-filled with what
  // was typed into the previous one — which would defeat the whole point.
  return <ConfirmBody key={request.phrase} request={request} onOpenChange={onOpenChange} />
}

function ConfirmBody({
  request,
  onOpenChange,
}: {
  request: ConfirmRequest
  onOpenChange: (open: boolean) => void
}) {
  const [typed, setTyped] = useState("")
  const [busy, setBusy] = useState(false)
  const matches = typed === request.phrase

  const run = async () => {
    if (!matches || busy) return
    setBusy(true)
    try {
      await request.action(typed)
      toast.success(`${request.title} completed`)
      onOpenChange(false)
      request.onDone?.()
    } catch (err) {
      const message = err instanceof ApiError ? err.message : String(err)
      toast.error(`${request.title} failed`, { description: message })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !busy && onOpenChange(open)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="size-4 text-destructive" />
            {request.title}
          </DialogTitle>
          <DialogDescription asChild>
            <div className="space-y-2 text-sm">{request.description}</div>
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <Label htmlFor="confirm-phrase" className="text-xs text-muted-foreground">
            Type <span className="font-mono font-semibold text-foreground">{request.phrase}</span>{" "}
            to confirm
          </Label>
          <Input
            id="confirm-phrase"
            autoFocus
            autoComplete="off"
            spellCheck={false}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && run()}
            className="font-mono"
            placeholder={request.phrase}
          />
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={run} disabled={!matches || busy}>
            {busy && <Loader2 className="size-4 animate-spin" />}
            {request.confirmLabel ?? request.title}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Drives a single ConfirmDialog from anywhere in a page. */
export function useConfirm() {
  const [request, setRequest] = useState<ConfirmRequest | null>(null)
  const dialog = <ConfirmDialog request={request} onOpenChange={(o) => !o && setRequest(null)} />
  return { confirm: setRequest, dialog }
}
