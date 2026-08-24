import dagre from "@dagrejs/dagre"
import type { Edge, Node } from "@xyflow/react"
import type { DbSchemaGraph } from "@/lib/types"
import { HEADER_HEIGHT, NODE_WIDTH, ROW_HEIGHT } from "./table-node"

/**
 * Where the tables go.
 *
 * Layered left to right by dagre rather than placed by a force simulation: a
 * physics layout looks livelier and puts the same schema somewhere different
 * every time it is opened, which is the opposite of what a reference diagram is
 * for. Dagre is deterministic, so a schema has *a* shape that somebody can
 * learn and come back to.
 *
 * Heights are computed rather than measured because the node renders at a size
 * this file decides — a header plus a row per column. Handing dagre a wrong
 * height is what produces a layout with boxes overlapping the edges beneath
 * them, and it is invisible until a table has thirty columns.
 */
export function layoutGraph(
  graph: DbSchemaGraph,
  opts: { keysOnly: boolean; direction: "LR" | "TB" },
): { nodes: Node[]; edges: Edge[] } {
  const g = new dagre.graphlib.Graph()
  g.setDefaultEdgeLabel(() => ({}))
  g.setGraph({
    rankdir: opts.direction,
    // Generous, because these nodes are wide and the edges need somewhere to
    // run that is not through another table.
    nodesep: 48,
    ranksep: 140,
    marginx: 32,
    marginy: 32,
  })

  const visibleCount = (t: DbSchemaGraph["tables"][number]) => {
    if (!opts.keysOnly) return t.columns.length
    const keys = t.columns.filter((c) => c.primaryKey || c.foreignKey || c.unique).length
    // The "n more columns" row takes space too.
    return keys + (keys < t.columns.length ? 1 : 0)
  }

  for (const t of graph.tables) {
    g.setNode(t.name, {
      width: NODE_WIDTH,
      height: HEADER_HEIGHT + Math.max(1, visibleCount(t)) * ROW_HEIGHT,
    })
  }
  // Only edges between tables that are both present, or dagre invents a node
  // for the missing end and lays out a box that is never drawn.
  const present = new Set(graph.tables.map((t) => t.name))
  for (const e of graph.edges) {
    if (present.has(e.fromTable) && present.has(e.toTable) && e.fromTable !== e.toTable) {
      g.setEdge(e.fromTable, e.toTable)
    }
  }
  dagre.layout(g)

  const positions = new Map<string, { x: number; y: number }>()
  const nodes: Node[] = graph.tables.map((t) => {
    const laid = g.node(t.name)
    // dagre reports the centre; React Flow positions by the top-left corner.
    const x = laid.x - laid.width / 2
    const y = laid.y - laid.height / 2
    positions.set(t.name, { x, y })
    return {
      id: t.name,
      type: "table",
      position: { x, y },
      data: { table: t },
      // Measured by us, so React Flow does not have to guess before first paint.
      width: laid.width,
      height: laid.height,
    }
  })

  const edges: Edge[] = graph.edges
    .filter((e) => present.has(e.fromTable) && present.has(e.toTable))
    .map((e, i) => {
      // The side an edge leaves by is chosen from where the layout actually
      // put the two tables. Anchoring every edge to the right of its source
      // sends half of them backwards around the outside of the diagram.
      const from = positions.get(e.fromTable)!
      const to = positions.get(e.toTable)!
      const forward = to.x >= from.x
      return {
        id: `${e.name}-${i}`,
        source: e.fromTable,
        target: e.toTable,
        sourceHandle: `${e.fromTable}.${e.fromColumn}.${forward ? "right" : "left"}.s`,
        targetHandle: `${e.toTable}.${e.toColumn}.${forward ? "left" : "right"}.t`,
        type: "relation",
        data: { relation: e },
      }
    })

  return { nodes, edges }
}
