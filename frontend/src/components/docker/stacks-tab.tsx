"use client"

import { Layers, Play, RotateCw, Square } from "lucide-react"
import { toast } from "sonner"
import { get, post } from "@/lib/api"
import type { ComposeStack } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { StatusBadge } from "@/components/status-dot"
import type { ConfirmFn } from "@/components/docker/shared"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export function StacksTab({ confirm }: { confirm: ConfirmFn }) {
  const { can } = useAuth()
  const { data, error, loading, refresh } = usePoll(
    (signal) => get<ComposeStack[]>("/docker/stacks/", undefined, signal),
    15000,
  )
  if (loading) return <LoadingRows />
  if (error) return <ErrorState error={error} />
  if (!data?.length) return <EmptyState icon={Layers} title="No compose stacks found" />

  const run = async (stack: ComposeStack, action: string, confirmText?: string) => {
    const res = await post<{ exitCode: number; output: string }>(
      `/docker/stacks/${encodeURIComponent(stack.name)}/${action}`,
      undefined,
      { confirm: confirmText },
    )
    if (res.exitCode !== 0) throw new Error(res.output.slice(-400))
    refresh()
  }

  return (
    <div className="grid items-start gap-4 lg:grid-cols-2 [&>*]:min-w-0">
      {data.map((stack) => (
        <Card key={stack.name}>
          <CardHeader>
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0">
                <CardTitle className="truncate text-base">{stack.name}</CardTitle>
                <CardDescription className="truncate font-mono text-xs">
                  {stack.workingDir || "location unknown"}
                </CardDescription>
              </div>
              <Badge
                variant={stack.running === stack.total && stack.total > 0 ? "default" : "secondary"}
              >
                {stack.running}/{stack.total}
              </Badge>
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="space-y-1">
              {stack.services.map((svc) => (
                <div
                  key={svc.container}
                  className="flex items-center justify-between gap-2 text-sm"
                >
                  <span className="truncate">{svc.name}</span>
                  <StatusBadge state={svc.state} />
                </div>
              ))}
            </div>
            {!stack.managed ? (
              <p className="text-xs text-muted-foreground">
                No compose file reachable from this dashboard, so this stack is read-only here.
              </p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {can("service.control") && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => run(stack, "up").catch((e) => toast.error(String(e)))}
                  >
                    <Play className="size-3.5" />
                    Up
                  </Button>
                )}
                {can("destructive") && (
                  <>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        confirm({
                          title: "Restart stack",
                          phrase: stack.name,
                          confirmLabel: "Restart",
                          description: (
                            <p>
                              Every service in <b>{stack.name}</b> restarts.
                            </p>
                          ),
                          action: (c) => run(stack, "restart", c),
                        })
                      }
                    >
                      <RotateCw className="size-3.5" />
                      Restart
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="text-destructive"
                      onClick={() =>
                        confirm({
                          title: "Take stack down",
                          phrase: stack.name,
                          confirmLabel: "Down",
                          description: (
                            <p>
                              Stops and removes every container in <b>{stack.name}</b>. Named
                              volumes survive.
                            </p>
                          ),
                          action: (c) => run(stack, "down", c),
                        })
                      }
                    >
                      <Square className="size-3.5" />
                      Down
                    </Button>
                  </>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      ))}
    </div>
  )
}
