"use client"

import { useState } from "react"
import Link from "next/link"
import { FolderPlus, Layers, Play, Plus } from "@/components/icons"
import { notify } from "@/lib/toast"
import { get, post } from "@/lib/api"
import type { ComposeStack } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingPanel, Spinner } from "@/components/state"
import { StatusDot } from "@/components/status-dot"
import { Panel, PanelBody, PanelFooter, PanelHeader } from "@/components/panel"
import { RowLink } from "@/components/page"
import { PortLink, type ConfirmFn } from "@/components/docker/shared"
import { StackDetailPanel } from "@/components/docker/stack-detail"
import { Hint, Term } from "@/components/docker/explain"
import { Badge } from "@/components/ui/badge"
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

/**
 * The stack list, which is now a way in rather than the whole feature.
 *
 * Everything a stack can do moved into its own panel, because a card with six
 * buttons on it is a card nobody reads and there was nowhere to put the
 * compose file, the merged logs, or the output of the command you just ran.
 * What is left here is the question the list should answer at a glance: which
 * applications exist, are they up, and where do I reach them.
 */
export function StacksTab({ confirm }: { confirm: ConfirmFn }) {
  const { can } = useAuth()
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  const { data, error, loading, refresh } = usePoll(
    (signal) => get<ComposeStack[]>("/docker/stacks/", undefined, signal),
    15000,
  )

  if (loading) return <LoadingPanel rows={3} />
  if (error) return <ErrorState error={error} />

  const newStack = can("file.write") && (
    <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
      <FolderPlus className="size-3.5" />
      New stack
    </Button>
  )

  return (
    <div className="space-y-4">
      {!data?.length ? (
        <EmptyState
          icon={Layers}
          title="No compose stacks found"
          description={
            <>
              A <Term name="stack">stack</Term> is a directory with a compose file in it. The
              dashboard finds them by the labels compose puts on the containers it creates, and by
              looking under the configured compose directories.
            </>
          }
          action={newStack}
        />
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Hint>
              {data.length} stack{data.length === 1 ? "" : "s"}. Open one to edit its compose file,
              watch a deploy, or read every service&apos;s logs together.
            </Hint>
            {newStack}
          </div>
          <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
            {data.map((stack) => (
              <StackCard
                key={stack.name}
                stack={stack}
                onOpen={() => setSelected(stack.name)}
                onChanged={refresh}
              />
            ))}
          </div>
        </>
      )}

      <StackDetailPanel
        name={selected}
        onOpenChange={(open) => !open && setSelected(null)}
        onChanged={refresh}
        confirm={confirm}
      />
      <NewStackDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={(name) => {
          refresh()
          setSelected(name)
        }}
      />
    </div>
  )
}

function StackCard({
  stack,
  onOpen,
  onChanged,
}: {
  stack: ComposeStack
  onOpen: () => void
  onChanged: () => void
}) {
  const { can } = useAuth()
  const [busy, setBusy] = useState(false)
  const healthy = stack.running === stack.total && stack.total > 0
  const unhealthy = stack.services.filter((s) => s.health === "unhealthy").length

  // The one action worth having on a card: an application that is down and
  // should not be. Everything else needs the panel, where the output is.
  const bringUp = async () => {
    setBusy(true)
    try {
      await post(`/docker/stacks/${encodeURIComponent(stack.name)}/up`)
      notify.success(`${stack.name} is up`)
      onChanged()
    } catch (err) {
      notify.error(`Could not start ${stack.name}`, err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Panel>
      <PanelHeader
        icon={Layers}
        title={
          <RowLink className="text-[13px] leading-tight" onClick={onOpen}>
            {stack.name}
          </RowLink>
        }
        description={stack.workingDir || "location unknown"}
        actions={
          <>
            {unhealthy > 0 && (
              <Badge variant="destructive" className="font-normal">
                {unhealthy} unhealthy
              </Badge>
            )}
            <Badge variant={healthy ? "success" : "secondary"} className="font-normal">
              {stack.running}/{stack.total} up
            </Badge>
          </>
        }
      />
      <PanelBody className="space-y-1.5">
        {stack.services.map((svc) => (
          <div
            key={svc.container || svc.name}
            className="flex min-w-0 items-center justify-between gap-2 text-[13px]"
          >
            <span className="flex min-w-0 items-center gap-2">
              <StatusDot state={svc.state} />
              <span className="truncate">{svc.name}</span>
            </span>
            <span className="flex shrink-0 flex-wrap justify-end gap-1">
              {svc.ports
                .filter((p) => p.publicPort)
                .slice(0, 2)
                .map((p, i) => (
                  <PortLink key={i} ip={p.ip} port={p.publicPort ?? 0} target={p.privatePort} />
                ))}
            </span>
          </div>
        ))}
        {stack.services.length === 0 && (
          <p className="text-xs text-muted-foreground">
            Nothing running. Its compose file is on disk and can be brought up.
          </p>
        )}
      </PanelBody>
      <PanelFooter>
        {!stack.managed ? (
          <p className="text-[11px] text-muted-foreground">
            No compose file reachable from this dashboard, so this stack is read-only here.
          </p>
        ) : (
          <>
            <Button size="sm" variant="outline" onClick={onOpen}>
              Open
            </Button>
            {can("service.control") && stack.running < stack.total && (
              <Button size="sm" variant="ghost" onClick={bringUp} disabled={busy}>
                {busy ? (
                  <Spinner className="size-3.5" />
                ) : (
                  <Play className="size-3.5" />
                )}
                Bring up
              </Button>
            )}
            {stack.workingDir && (
              <Button size="sm" variant="ghost" asChild className="ml-auto">
                <Link href={`/files?path=${encodeURIComponent(stack.workingDir)}`}>Files</Link>
              </Button>
            )}
          </>
        )}
      </PanelFooter>
    </Panel>
  )
}

/**
 * A new stack is a directory and a compose file — nothing more, which is the
 * point. It is created with a working starter file rather than an empty one,
 * because the format is exactly the part somebody new does not know, and a
 * blank editor is the least useful thing to hand them.
 */
function NewStackDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (name: string) => void
}) {
  const [name, setName] = useState("")
  const [dir, setDir] = useState("")
  const [busy, setBusy] = useState(false)

  const create = async () => {
    setBusy(true)
    try {
      const res = await post<{ name: string; dir: string }>("/docker/stacks/", {
        name,
        dir: dir.trim() || undefined,
      })
      notify.success(`${res.name} created`, { description: res.dir })
      onOpenChange(false)
      onCreated(res.name)
      setName("")
      setDir("")
    } catch (err) {
      notify.error("Could not create the stack", err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Plus className="size-4" />
            New stack
          </DialogTitle>
          <DialogDescription>
            Creates a directory with a starter compose file in it. Nothing runs until you bring it
            up.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="stack-name" className="text-xs">
              Name
            </Label>
            <Input
              id="stack-name"
              value={name}
              spellCheck={false}
              placeholder="my-app"
              onChange={(e) => setName(e.target.value)}
            />
            <Hint>
              Lower-case letters, digits, dashes and underscores. Compose uses it to name the
              containers and the network it creates.
            </Hint>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="stack-dir" className="text-xs">
              Directory
            </Label>
            <Input
              id="stack-dir"
              value={dir}
              spellCheck={false}
              className="font-mono text-xs"
              placeholder="leave empty for the default compose directory"
              onChange={(e) => setDir(e.target.value)}
            />
            <Hint>
              It has to be under one of the server&apos;s configured compose directories, or the
              dashboard will not find the stack again once it is stopped.
            </Hint>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={create} disabled={busy || !name.trim()}>
            {busy && <Spinner className="size-4" />}
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
