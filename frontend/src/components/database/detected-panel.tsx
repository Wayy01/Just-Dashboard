"use client"

import { useState } from "react"
import { Database, Link2, Plus, ServerCog } from "lucide-react"
import { toast } from "sonner"
import { errorMessage, get, post } from "@/lib/api"
import type { DbConnection, DbDetected, DbDetectedServer, DbProvisionOption } from "@/lib/types"
import { useAuth } from "@/hooks/use-auth"
import { usePoll } from "@/hooks/use-poll"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { Spinner } from "@/components/state"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

/**
 * The databases already running on this server, and a way to start another.
 *
 * "Add connection" can only connect to something that already exists, and it
 * needs a DSN to do it — which left the answer to "I want a database" as: open
 * the Docker page, start one, work out what its connection string would be from
 * the variables you just typed, come back, paste it in. Every fact in that
 * string is one the server can already read off the container.
 *
 * So this is two things in one panel, because they are the same question asked
 * at two moments. What is already here gets a Connect button; what is not yet
 * here gets started and then connected the moment it answers. Neither path
 * shows the operator a password, because neither path needs to: the credentials
 * are read or generated on the server and sealed there.
 *
 * It hides itself when there is nothing to say — a host with no Docker socket,
 * or one where every database found is already connected — rather than sitting
 * on the page as an empty box explaining its own absence.
 */
export function DetectedPanel({ onConnected }: { onConnected: () => void }) {
  const { can } = useAuth()
  const [busy, setBusy] = useState<string | null>(null)

  const detected = usePoll(
    (signal) => get<DbDetected>("/databases/detected", undefined, signal),
    15000,
  )
  const options = usePoll(
    (signal) => get<DbProvisionOption[]>("/databases/provision/options", undefined, signal),
    0,
  )

  if (!can("system.admin")) return null
  // A host with no Docker socket has nothing to detect and nowhere to start a
  // server, which is information the Docker page already gives; repeating it
  // here would be a permanent empty panel on every install without Docker.
  if (detected.error) return null

  const servers = detected.data?.servers ?? []
  const unconnected = servers.filter((s) => !s.adopted)
  if (!detected.loading && unconnected.length === 0 && !options.data?.length) return null

  const connect = async (server: DbDetectedServer) => {
    setBusy(server.container)
    try {
      const conn = await post<DbConnection>("/databases/adopt", { container: server.container })
      toast.success(`Connected to ${conn.name}`, {
        description: `${conn.driver} · ${conn.host}:${conn.port}`,
      })
      detected.refresh()
      onConnected()
    } catch (err) {
      toast.error("Could not connect", { description: errorMessage(err) })
    } finally {
      setBusy(null)
    }
  }

  /**
   * Start a server, then keep asking to adopt it until it answers.
   *
   * The provision request returns as soon as the container is started; the
   * engine inside it takes seconds to a minute more to accept a connection, and
   * there is no way to know which without asking. Polling here rather than
   * holding the request open is what keeps a slow first boot looking like
   * progress instead of a hung dashboard, and it means giving up is the
   * operator closing the page rather than a timeout they cannot see.
   */
  const provision = async (option: DbProvisionOption) => {
    setBusy(option.engine)
    try {
      const started = await post<{ container: string }>("/databases/provision", {
        engine: option.engine,
      })
      toast.success(`Starting ${option.label}`, { description: "Connecting when it is ready…" })
      detected.refresh()

      const deadline = Date.now() + 3 * 60_000
      for (;;) {
        try {
          const conn = await post<DbConnection>("/databases/adopt", {
            container: started.container,
          })
          toast.success(`${option.label} is ready`, { description: `Connected as ${conn.name}` })
          detected.refresh()
          onConnected()
          return
        } catch (err) {
          if (Date.now() > deadline) {
            toast.error(`${option.label} started but did not become reachable`, {
              description: errorMessage(err),
            })
            return
          }
          await new Promise((r) => setTimeout(r, 2000))
        }
      }
    } catch (err) {
      toast.error(`Could not start ${option.label}`, { description: errorMessage(err) })
    } finally {
      setBusy(null)
    }
  }

  return (
    <Panel>
      <PanelHeader
        icon={ServerCog}
        title="Databases on this server"
        description={
          unconnected.length
            ? "Found running here — connect without a connection string"
            : "Nothing running here that is not already connected"
        }
        actions={
          options.data &&
          options.data.length > 0 && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="sm" disabled={busy !== null}>
                  {busy && options.data.some((o) => o.engine === busy) ? (
                    <Spinner />
                  ) : (
                    <Plus className="size-4" />
                  )}
                  New database
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                {options.data.map((o) => (
                  <DropdownMenuItem key={o.engine} onClick={() => provision(o)}>
                    <Database className="size-3.5" />
                    <span className="flex-1">{o.label}</span>
                    <span className="font-mono text-[10px] text-muted-foreground">{o.image}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )
        }
      />
      {unconnected.length > 0 && (
        <PanelBody className="space-y-1.5">
          {unconnected.map((s) => (
            <div
              key={s.container}
              className="flex min-w-0 items-center gap-3 rounded-md px-2 py-1.5 hover:bg-accent"
            >
              <Database className="size-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="truncate font-mono text-[13px]">{s.container}</div>
                <div className="truncate text-[11px] text-muted-foreground">
                  {s.image}
                  {s.port > 0 && ` · ${s.host}:${s.port}`}
                  {s.user && ` · ${s.user}`}
                  {s.database && ` · ${s.database}`}
                </div>
              </div>
              {s.reason ? (
                // A container that is running and unreachable is the commonest
                // reason a database somebody can see is one they cannot use, so
                // it says why rather than being quietly dropped from the list.
                <Badge variant="secondary" className="font-normal" title={s.reason}>
                  not reachable
                </Badge>
              ) : (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy !== null}
                  onClick={() => connect(s)}
                >
                  {busy === s.container ? <Spinner /> : <Link2 className="size-3.5" />}
                  Connect
                </Button>
              )}
            </div>
          ))}
        </PanelBody>
      )}
    </Panel>
  )
}
