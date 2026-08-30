"use client"

import { useEffect, useState } from "react"
import { Linked, NetworkDevice, Plus, Slash, Trash } from "@/components/icons"
import { notify } from "@/lib/toast"
import { del, get, post } from "@/lib/api"
import type { Container, DockerNetwork, NetworkDetail } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingPanel, LoadingRows, Spinner } from "@/components/state"
import { IconAction } from "@/components/icon-action"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { SidePanel } from "@/components/side-panel"
import { Detail, DetailList, RowLink } from "@/components/page"
import type { ConfirmFn } from "@/components/docker/shared"
import { Hint } from "@/components/docker/explain"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

/**
 * Networks, and — the part that was missing — who is on them.
 *
 * "These two containers cannot see each other" is the most common Docker
 * problem there is, and its answer is almost always that they are on different
 * networks. The Engine will tell you, and no panel in this class puts it on
 * screen next to the name each container answers to. Attaching one is two
 * clicks here rather than a shell.
 */
export function NetworksTab({ confirm }: { confirm: ConfirmFn }) {
  const { can } = useAuth()
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  const { data, error, loading, refresh } = usePoll(
    (signal) => get<DockerNetwork[]>("/docker/networks/", undefined, signal),
    30000,
  )
  if (loading) return <LoadingPanel />
  if (error) return <ErrorState error={error} />

  // The three the Engine owns are never removable, so they are not "unused"
  // in any sense the prune button should count.
  const unused = (data ?? []).filter(
    (n) => n.usedBy.length === 0 && !SYSTEM_NETWORKS.includes(n.name),
  )

  return (
    <div className="space-y-4">
      <Panel>
        <PanelHeader
          icon={NetworkDevice}
          title="Networks"
          description={`${data?.length ?? 0} defined on this daemon`}
          actions={
            <>
              {can("service.control") && (
                <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
                  <Plus className="size-4" />
                  New network
                </Button>
              )}
              {/* POST /docker/networks/prune has existed since this tab did and
                  nothing ever called it. A network left behind by a removed
                  stack is invisible clutter that also holds a subnet out of the
                  pool, which is what makes a later `compose up` fail to find
                  one. */}
              {can("destructive") && unused.length > 0 && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() =>
                    confirm({
                      title: "Remove unused networks",
                      confirmLabel: "Remove",
                      description: (
                        <p>
                          Removes the {unused.length} network
                          {unused.length === 1 ? "" : "s"} nothing is attached to:{" "}
                          <b>{unused.map((n) => n.name).join(", ")}</b>. Docker recreates a compose
                          network the next time its stack comes up.
                        </p>
                      ),
                      action: async () => {
                        const rep = await post<{ items: string[] }>("/docker/networks/prune")
                        notify.success(
                          rep.items.length
                            ? `Removed ${rep.items.length} network${rep.items.length === 1 ? "" : "s"}`
                            : "Nothing to remove",
                        )
                        refresh()
                      },
                    })
                  }
                >
                  <Trash className="size-4" />
                  Prune
                </Button>
              )}
            </>
          }
        />
        <PanelBody flush>
          <Table containerClassName="max-h-[calc(100svh-24rem)]">
            <TableHeader className={stickyTableHeader}>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Driver</TableHead>
                <TableHead className="w-full">Subnets</TableHead>
                <TableHead className="text-right">Containers</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {data?.map((network) => (
                <TableRow
                  key={network.id}
                  className="group"
                  onActivate={() => setSelected(network.id)}
                >
                  <TableCell>
                    <RowLink onClick={() => setSelected(network.id)}>{network.name}</RowLink>
                    {network.internal && (
                      <Badge variant="outline" className="ml-2 text-[10px] font-normal">
                        no internet
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-xs">{network.driver}</TableCell>
                  <TableCell className="font-mono text-[11px] text-muted-foreground">
                    {network.subnets.join(", ") || "—"}
                  </TableCell>
                  <TableCell className="numeric text-right text-xs">
                    {network.usedBy.length > 0 ? (
                      <Tooltip>
                        <TooltipTrigger className="cursor-default">
                          {network.usedBy.length}
                        </TooltipTrigger>
                        <TooltipContent>{network.usedBy.join(", ")}</TooltipContent>
                      </Tooltip>
                    ) : (
                      <span className="text-muted-foreground">0</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {can("destructive") && !SYSTEM_NETWORKS.includes(network.name) && (
                      <IconAction
                        label="Remove"
                        className="text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                        onClick={() =>
                          confirm({
                            title: "Delete network",
                            confirmLabel: "Delete",
                            description: (
                              <p>
                                Removes <b>{network.name}</b>.
                                {network.usedBy.length > 0
                                  ? ` ${network.usedBy.join(", ")} ${
                                      network.usedBy.length === 1 ? "is" : "are"
                                    } attached and will lose the names they use to reach each other on it.`
                                  : " Nothing is attached to it."}
                              </p>
                            ),
                            action: async (c) => {
                              await del(`/docker/networks/${network.id}`, { confirm: c })
                              refresh()
                            },
                          })
                        }
                      >
                        <Trash />
                      </IconAction>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {!data?.length && (
                <TableRow>
                  <TableCell colSpan={5} className="p-0">
                    <EmptyState icon={NetworkDevice} title="No networks" />
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </PanelBody>
      </Panel>

      <NetworkDetailPanel
        id={selected}
        onOpenChange={(o) => !o && setSelected(null)}
        onChanged={refresh}
      />
      <NewNetworkDialog open={creating} onOpenChange={setCreating} onCreated={refresh} />
    </div>
  )
}

/** The three Docker creates and will not let you remove. */
const SYSTEM_NETWORKS = ["bridge", "host", "none"]

function NetworkDetailPanel({
  id,
  onOpenChange,
  onChanged,
}: {
  id: string | null
  onOpenChange: (open: boolean) => void
  onChanged: () => void
}) {
  const { can } = useAuth()
  const [attaching, setAttaching] = useState(false)

  const { data, error, loading, refresh } = usePoll<NetworkDetail>(
    (signal) =>
      get<NetworkDetail>(`/docker/networks/${encodeURIComponent(id ?? "")}`, undefined, signal),
    0,
    [id],
    { enabled: id !== null },
  )

  const disconnect = async (containerId: string, containerName: string) => {
    try {
      await post(`/docker/networks/${id}/disconnect`, { container: containerId })
      notify.success(`${containerName} left ${data?.name}`)
      refresh()
      onChanged()
    } catch (err) {
      notify.error("Could not detach it", err)
    }
  }

  return (
    <SidePanel
      open={id !== null}
      onOpenChange={onOpenChange}
      icon={NetworkDevice}
      title={data?.name ?? "Network"}
      description={data?.subnets.join(", ")}
      actions={
        can("service.control") &&
        data &&
        !data.system && (
          <Button size="sm" variant="outline" onClick={() => setAttaching(true)}>
            <Linked className="size-3.5" />
            Attach a container
          </Button>
        )
      }
    >
      {error && <ErrorState error={error} />}
      {loading && !data && <LoadingRows />}
      {data && (
        <div className="space-y-5">
          <DetailList>
            <Detail label="Driver">{data.driver}</Detail>
            <Detail label="Subnet">{data.subnets.join(", ") || "assigned by Docker"}</Detail>
            <Detail label="Gateway">{data.gateway || "—"}</Detail>
            <Detail label="Reaches the internet">{data.internal ? "no" : "yes"}</Detail>
            <Detail label="IPv6">{data.ipv6 ? "on" : "off"}</Detail>
          </DetailList>

          <section className="space-y-1.5">
            <p className="eyebrow">On this network</p>
            <Hint>
              Each of these can reach the others at the names listed beside it. A connection string
              on this network uses one of those names, not an IP address —{" "}
              <span className="font-mono">postgres:5432</span> rather than a number that changes
              when the container restarts.
            </Hint>
            {data.members.length === 0 ? (
              <Hint className="italic">Nothing is attached.</Hint>
            ) : (
              <div className="space-y-1">
                {data.members.map((m) => (
                  <div
                    key={m.id}
                    className="group flex flex-wrap items-center justify-between gap-2 rounded-md border border-hairline px-2.5 py-1.5 text-xs"
                  >
                    <span className="min-w-0">
                      <span className="font-medium">{m.name}</span>
                      <span className="ml-2 font-mono text-[11px] text-muted-foreground">
                        {m.ipv4 || "no address"}
                      </span>
                    </span>
                    <span className="flex shrink-0 items-center gap-1">
                      {m.aliases
                        .filter((a) => a !== m.name && !m.id.startsWith(a))
                        .map((a) => (
                          <Badge
                            key={a}
                            variant="outline"
                            className="font-mono text-[10px] font-normal"
                          >
                            {a}
                          </Badge>
                        ))}
                      {can("service.control") && !data.system && (
                        <IconAction
                          label={`Detach ${m.name}`}
                          className="opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                          onClick={() => disconnect(m.id, m.name)}
                        >
                          <Slash />
                        </IconAction>
                      )}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </section>
        </div>
      )}

      <AttachDialog
        open={attaching}
        networkId={id}
        networkName={data?.name}
        onOpenChange={setAttaching}
        onAttached={() => {
          refresh()
          onChanged()
        }}
        attached={new Set((data?.members ?? []).map((m) => m.id))}
      />
    </SidePanel>
  )
}

function AttachDialog({
  open,
  networkId,
  networkName,
  onOpenChange,
  onAttached,
  attached,
}: {
  open: boolean
  networkId: string | null
  networkName?: string
  onOpenChange: (open: boolean) => void
  onAttached: () => void
  attached: Set<string>
}) {
  const [containers, setContainers] = useState<Container[]>([])
  const [picked, setPicked] = useState("")
  const [alias, setAlias] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    get<Container[]>("/docker/containers/", undefined, controller.signal)
      .then(setContainers)
      .catch(() => undefined)
    return () => controller.abort()
  }, [open])

  const attach = async () => {
    setBusy(true)
    try {
      await post(`/docker/networks/${networkId}/connect`, {
        container: picked,
        aliases: alias.trim() ? [alias.trim()] : undefined,
      })
      notify.success(`Attached to ${networkName}`)
      onAttached()
      onOpenChange(false)
      setPicked("")
      setAlias("")
    } catch (err) {
      notify.error("Could not attach it", err)
    } finally {
      setBusy(false)
    }
  }

  const available = containers.filter((c) => !attached.has(c.id))

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Linked className="size-4" />
            Attach a container
          </DialogTitle>
          <DialogDescription>
            It joins <b>{networkName}</b> immediately, without restarting, and can reach everything
            else on it by name.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label className="text-xs">Container</Label>
            <Select value={picked} onValueChange={setPicked}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Pick one" />
              </SelectTrigger>
              <SelectContent>
                {available.map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {available.length === 0 && <Hint>Every container is already on this network.</Hint>}
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">Extra name (optional)</Label>
            <Input
              value={alias}
              spellCheck={false}
              className="font-mono text-xs"
              placeholder="db"
              onChange={(e) => setAlias(e.target.value)}
            />
            <Hint>
              An additional hostname the others can use. Useful when an application&apos;s config
              expects a name that is not the container&apos;s.
            </Hint>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={attach} disabled={busy || !picked}>
            {busy && <Spinner className="size-4" />}
            Attach
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function NewNetworkDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: () => void
}) {
  const [name, setName] = useState("")
  const [internal, setInternal] = useState(false)
  const [subnet, setSubnet] = useState("")
  const [busy, setBusy] = useState(false)

  const create = async () => {
    setBusy(true)
    try {
      await post("/docker/networks/", { name, internal, subnet: subnet.trim() || undefined })
      notify.success(`${name} created`)
      onCreated()
      onOpenChange(false)
      setName("")
      setSubnet("")
      setInternal(false)
    } catch (err) {
      notify.error("Could not create the network", err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Plus className="size-4" />
            New network
          </DialogTitle>
          <DialogDescription>
            A private network for containers that need to reach each other. On it, a
            container&apos;s name is its hostname.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="network-name" className="text-xs">
              Name
            </Label>
            <Input
              id="network-name"
              value={name}
              spellCheck={false}
              className="font-mono"
              placeholder="app-internal"
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-hairline p-2.5">
            <Switch
              checked={internal}
              onCheckedChange={setInternal}
              className="mt-0.5"
              aria-label="No internet access"
            />
            <span>
              <span className="block text-xs font-medium">Cut it off from the internet</span>
              <Hint>
                Containers on this network can reach each other and nothing else. The right choice
                for a database that only needs to talk to the application in front of it.
              </Hint>
            </span>
          </label>
          <div className="space-y-1.5">
            <Label htmlFor="network-subnet" className="text-xs">
              Subnet (optional)
            </Label>
            <Input
              id="network-subnet"
              value={subnet}
              spellCheck={false}
              className="font-mono text-xs"
              placeholder="Docker picks one"
              onChange={(e) => setSubnet(e.target.value)}
            />
            <Hint>
              Only worth setting if it has to avoid a range already used on your own network — a VPN
              or an office LAN.
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
