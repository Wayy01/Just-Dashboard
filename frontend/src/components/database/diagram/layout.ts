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

  const present = new Set(graph.tables.map((t) => t.name))
  const heightOf = (t: DbSchemaGraph["tables"][number]) =>
    HEADER_HEIGHT + Math.max(1, visibleCount(t)) * ROW_HEIGHT

  // Tables with no relationships at all are laid out separately.
  //
  // Dagre has nothing to rank them by, so it puts every one of them in the
  // same rank — a single column as tall as the schema is wide. A database with
  // six unrelated lookup tables became a strip a thousand pixels tall that fit
  // to view at ten per cent zoom, which is a diagram of nothing. They are
  // packed into a grid below the connected graph instead, where they are still
  // findable and cost almost no space.
  const connectedNames = new Set<string>()
  for (const e of graph.edges) {
    if (present.has(e.fromTable) && present.has(e.toTable)) {
      connectedNames.add(e.fromTable)
      connectedNames.add(e.toTable)
    }
  }
  const connected = graph.tables.filter((t) => connectedNames.has(t.name))
  const isolated = graph.tables.filter((t) => !connectedNames.has(t.name))

  for (const t of connected) {
    g.setNode(t.name, { width: NODE_WIDTH, height: heightOf(t) })
  }
  // Only edges between tables that are both present, or dagre invents a node
  // for the missing end and lays out a box that is never drawn.
  for (const e of graph.edges) {
    if (present.has(e.fromTable) && present.has(e.toTable) && e.fromTable !== e.toTable) {
      g.setEdge(e.fromTable, e.toTable)
    }
  }
  dagre.layout(g)

  const positions = new Map<string, { x: number; y: number }>()
  const nodes: Node[] = []
  let maxX = 0
  let maxY = 0

  for (const t of connected) {
    const laid = g.node(t.name)
    // dagre reports the centre; React Flow positions by the top-left corner.
    const x = laid.x - laid.width / 2
    const y = laid.y - laid.height / 2
    positions.set(t.name, { x, y })
    maxX = Math.max(maxX, x + laid.width)
    maxY = Math.max(maxY, y + laid.height)
    nodes.push({
      id: t.name,
      type: "table",
      position: { x, y },
      data: { table: t },
      // Measured by us, so React Flow does not have to guess before first paint.
      width: laid.width,
      height: laid.height,
    })
  }

  // The grid is as wide as the connected graph, so the two halves read as one
  // picture rather than as a diagram with a tail.
  const perRow = Math.max(
    1,
    Math.min(isolated.length, Math.round(Math.sqrt(isolated.length * 1.6))),
  )
  const gridTop = connected.length > 0 ? maxY + 72 : 32
  let rowTop = gridTop
  let rowHeight = 0
  isolated.forEach((t, i) => {
    const col = i % perRow
    if (col === 0 && i > 0) {
      rowTop += rowHeight + 32
      rowHeight = 0
    }
    const h = heightOf(t)
    rowHeight = Math.max(rowHeight, h)
    const x = 32 + col * (NODE_WIDTH + 40)
    positions.set(t.name, { x, y: rowTop })
    nodes.push({
      id: t.name,
      type: "table",
      position: { x, y: rowTop },
      data: { table: t },
      width: NODE_WIDTH,
      height: h,
    })
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
