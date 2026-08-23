"use client"

import { useMemo } from "react"
import { Network } from "lucide-react"
import { get } from "@/lib/api"
import type { DbConnection, DbForeignKey, DbRelations, DbTable } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { Panel, PanelBody, PanelHeader } from "@/components/panel"
import { EmptyState, ErrorState, LoadingPanel } from "@/components/state"

const BOX_W = 190
const BOX_H = 44
const GAP_X = 90
const GAP_Y = 26
const PAD = 24

type Node = { name: string; x: number; y: number; layer: number }
type Edge = { from: Node; to: Node; label: string }

/**
 * The foreign-key graph, drawn.
 *
 * Every panel in this class lists foreign keys in a table and stops there,
 * which answers "what does this column reference" and never answers "what does
 * this schema look like" — the question somebody arriving at an unfamiliar
 * database actually has.
 *
 * The layout is deterministic rather than force-directed: tables are layered by
 * how deep their references go, so the things nothing depends on sit on the
 * left and the leaves on the right. A physics simulation would look livelier
 * and would put the same schema somewhere different every time you opened it,
 * which is the opposite of what a reference diagram is for. Fixed box sizes are
 * what let the positions be computed without measuring the DOM.
 */
export function ErDiagram({ conn, schema }: { conn: DbConnection; schema: string }) {
  const relations = usePoll(
    (signal) => get<DbRelations>(`/databases/${conn.id}/relations`, { schema }, signal),
    0,
    [conn.id, schema],
  )
  const tables = usePoll(
    (signal) => get<DbTable[]>(`/databases/${conn.id}/tables`, { schema }, signal),
    0,
    [conn.id, schema],
  )

  const { nodes, edges, width, height, isolated } = useMemo(
    () => layout(relations.data ?? {}, tables.data ?? []),
    [relations.data, tables.data],
  )

  if (relations.loading || tables.loading) return <LoadingPanel />
  if (relations.error) return <ErrorState error={relations.error} />

  return (
    <Panel>
      <PanelHeader
        icon={Network}
        title="Relationships"
        description={
          edges.length
            ? `${nodes.length} tables · ${edges.length} foreign keys`
            : "No foreign keys in this schema"
        }
      />
      <PanelBody flush>
        {edges.length === 0 ? (
          <EmptyState
            icon={Network}
            title="Nothing to draw"
            description={
              isolated > 0
                ? `${isolated} tables, none of them referencing another. Either this schema keeps its relationships in application code, or the constraints were never declared.`
                : "No tables in this schema."
            }
          />
        ) : (
          <div className="overflow-auto p-4">
            <svg
              width={width}
              height={height}
              viewBox={`0 0 ${width} ${height}`}
              className="max-w-none"
              role="img"
              aria-label="Entity relationship diagram"
            >
              <defs>
                <marker
                  id="er-arrow"
                  viewBox="0 0 10 10"
                  refX="9"
                  refY="5"
                  markerWidth="6"
                  markerHeight="6"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" className="fill-muted-foreground/70" />
                </marker>
              </defs>

              {edges.map((e, i) => (
                <g key={i}>
                  <path
                    d={edgePath(e)}
                    fill="none"
                    className="stroke-muted-foreground/40"
                    strokeWidth={1.25}
                    markerEnd="url(#er-arrow)"
                  />
                </g>
              ))}

              {nodes.map((n) => (
                <g key={n.name}>
                  <rect
                    x={n.x}
                    y={n.y}
                    width={BOX_W}
                    height={BOX_H}
                    rx={6}
                    className="fill-card stroke-border"
                    strokeWidth={1}
                  />
                  <rect
                    x={n.x}
                    y={n.y}
                    width={4}
                    height={BOX_H}
                    className="fill-primary/60"
                    rx={2}
                  />
                  <text
                    x={n.x + 14}
                    y={n.y + BOX_H / 2 + 4}
                    className="fill-foreground font-mono"
                    fontSize={12}
                  >
                    {truncate(n.name, 22)}
                  </text>
                </g>
              ))}
            </svg>
          </div>
        )}
      </PanelBody>
    </Panel>
  )
}

function truncate(s: string, n: number) {
  return s.length > n ? s.slice(0, n - 1) + "…" : s
}

// edgePath draws a horizontal bezier between two boxes, leaving the right edge
// of the source and arriving at the left edge of the target. A straight line
// between box centres would run through the boxes themselves.
function edgePath(e: Edge) {
  const x1 = e.from.x + BOX_W
  const y1 = e.from.y + BOX_H / 2
  const x2 = e.to.x
  const y2 = e.to.y + BOX_H / 2
  const dx = Math.max(40, Math.abs(x2 - x1) / 2)
  return `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`
}

/**
 * layout assigns each table a layer from how far its references reach, then
 * lays the layers out left to right.
 *
 * The depth walk carries a visited set because schemas contain reference
 * cycles — a pair of tables pointing at each other is unusual but legal, and
 * without the guard it is an infinite recursion rather than a diagram.
 */
function layout(relations: DbRelations, tables: DbTable[]) {
  const names = new Set<string>()
  for (const t of tables) if (!/view/i.test(t.type)) names.add(t.name)
  for (const k of Object.keys(relations)) names.add(k)

  const outgoing = new Map<string, DbForeignKey[]>()
  for (const [table, fks] of Object.entries(relations)) outgoing.set(table, fks)

  const connected = new Set<string>()
  for (const [table, fks] of outgoing) {
    for (const fk of fks) {
      if (!fk.refTable) continue
      connected.add(table)
      connected.add(fk.refTable)
      names.add(fk.refTable)
    }
  }

  const depth = new Map<string, number>()
  const compute = (name: string, seen: Set<string>): number => {
    if (depth.has(name)) return depth.get(name)!
    if (seen.has(name)) return 0
    seen.add(name)
    let d = 0
    for (const fk of outgoing.get(name) ?? []) {
      if (!fk.refTable || fk.refTable === name) continue
      d = Math.max(d, compute(fk.refTable, seen) + 1)
    }
    seen.delete(name)
    depth.set(name, d)
    return d
  }
  for (const n of connected) compute(n, new Set())

  const byLayer = new Map<number, string[]>()
  for (const n of [...connected].sort()) {
    const d = depth.get(n) ?? 0
    byLayer.set(d, [...(byLayer.get(d) ?? []), n])
  }

  const nodes: Node[] = []
  const index = new Map<string, Node>()
  for (const [layer, members] of [...byLayer.entries()].sort((a, b) => a[0] - b[0])) {
    members.forEach((name, i) => {
      const node: Node = {
        name,
        layer,
        x: PAD + layer * (BOX_W + GAP_X),
        y: PAD + i * (BOX_H + GAP_Y),
      }
      nodes.push(node)
      index.set(name, node)
    })
  }

  const edges: Edge[] = []
  for (const [table, fks] of outgoing) {
    const from = index.get(table)
    if (!from) continue
    for (const fk of fks) {
      const to = index.get(fk.refTable)
      if (!to || to === from) continue
      edges.push({ from, to, label: fk.columns.join(", ") })
    }
  }

  const width = Math.max(
    320,
    PAD * 2 + (Math.max(0, ...nodes.map((n) => n.layer)) + 1) * (BOX_W + GAP_X) - GAP_X,
  )
  const height = Math.max(
    160,
    PAD * 2 + Math.max(1, ...[...byLayer.values()].map((m) => m.length)) * (BOX_H + GAP_Y) - GAP_Y,
  )
  return { nodes, edges, width, height, isolated: names.size - connected.size }
}
