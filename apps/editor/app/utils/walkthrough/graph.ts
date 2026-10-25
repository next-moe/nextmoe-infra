import dagre from '@dagrejs/dagre'
import type { WtChart, WtNode, WtNodeType } from '#shared/types/walkthrough'
import { WT_NODE_SIZE } from '~/constants/walkthrough'
import { characterTones, nodeLabel, segmentCharacterKey } from './doc'

export interface WtGraphNode {
  id: string
  kind: WtNodeType
  key: string | null
  segmentKey: string | null
  label: string
  tone: number | null
  width: number
  height: number
  x: number
  y: number
}

export interface WtGraphEdge {
  id: string
  from: string
  to: string
  segmentKey: string
  stepStart: number
  stepEnd: number
  firstText: string | null
  stepCount: number
  tone: number | null
  path: string
  head: { x: number; y: number; angle: number }
  labelX: number
  labelY: number
}

export interface WtGraphLayout {
  nodes: WtGraphNode[]
  edges: WtGraphEdge[]
  width: number
  height: number
}

export const graphNodeId = (node: WtNode, segmentKey: string) =>
  node.node_type === 'collect' || node.node_type === 'open'
    ? `${node.node_type}:${segmentKey}`
    : `${node.node_type}:${node.key}`

type Point = { x: number; y: number }

const smoothPath = (points: Point[]) => {
  const [first, ...rest] = points
  if (!first) return ''
  let d = `M ${first.x} ${first.y}`
  rest.forEach((p, i) => {
    const prev = i === 0 ? first : rest[i - 1]!
    const midY = (prev.y + p.y) / 2
    d += ` C ${prev.x} ${midY}, ${p.x} ${midY}, ${p.x} ${p.y}`
  })
  return d
}

const MARGIN = 24

export const layoutChart = (chart: WtChart): WtGraphLayout => {
  const tones = characterTones(chart)
  const nodes = new Map<string, Omit<WtGraphNode, 'x' | 'y'>>()
  const edges: Omit<WtGraphEdge, 'path' | 'head' | 'labelX' | 'labelY'>[] = []

  const addNode = (node: WtNode, segmentKey: string, tone: number | null) => {
    const id = graphNodeId(node, segmentKey)
    const existing = nodes.get(id)
    if (existing) {
      existing.tone ??= tone
      return id
    }
    nodes.set(id, {
      id,
      kind: node.node_type,
      key: node.key,
      segmentKey:
        node.node_type === 'collect' || node.node_type === 'open'
          ? segmentKey
          : null,
      label: nodeLabel(chart, node),
      tone,
      ...WT_NODE_SIZE[node.node_type]
    })
    return id
  }

  for (const segment of chart.segments) {
    const routeKey = segmentCharacterKey(chart, segment)
    const tone = routeKey ? (tones.get(routeKey) ?? null) : null
    let current = addNode(segment.from, segment.key, null)
    let start = 0
    let piece = 0
    const close = (to: string, end: number) => {
      const run = segment.steps.slice(start, end)
      const first = run.find((st) => st.step_type !== 'save')
      edges.push({
        id: `${segment.key}:${piece++}`,
        from: current,
        to,
        segmentKey: segment.key,
        stepStart: start,
        stepEnd: end,
        firstText: first?.text_zh || first?.text || null,
        stepCount: run.filter((st) => st.step_type !== 'save').length,
        tone
      })
      current = to
      start = end + 1
    }
    segment.steps.forEach((step, i) => {
      if (step.step_type === 'save' && step.save) {
        close(
          addNode({ node_type: 'save', key: step.save }, segment.key, tone),
          i
        )
      }
    })
    const ending =
      segment.to.node_type === 'ending'
        ? chart.endings.find((e) => e.key === segment.to.key)
        : undefined
    const endTone = ending?.characters.length
      ? (tones.get(ending.characters[0]!) ?? tone)
      : tone
    close(addNode(segment.to, segment.key, endTone), segment.steps.length)
  }
  for (const entry of chart.entries) {
    const entryId = `entry:${entry.key}`
    if (!nodes.has(entryId)) {
      addNode({ node_type: 'entry', key: entry.key }, '', null)
    }
    if (!entry.unlocked_by) continue
    const from = `ending:${entry.unlocked_by}`
    if (nodes.has(from)) {
      edges.push({
        id: `unlock:${entry.key}`,
        from,
        to: entryId,
        segmentKey: '',
        stepStart: 0,
        stepEnd: 0,
        firstText: null,
        stepCount: 0,
        tone: null
      })
    }
  }

  const g = new dagre.graphlib.Graph({ multigraph: true })
  g.setGraph({
    rankdir: 'TB',
    nodesep: 28,
    ranksep: 64,
    edgesep: 10,
    marginx: MARGIN,
    marginy: MARGIN
  })
  g.setDefaultEdgeLabel(() => ({}))
  for (const n of nodes.values()) {
    g.setNode(n.id, { width: n.width, height: n.height })
  }
  for (const e of edges) {
    g.setEdge(e.from, e.to, { minlen: 1, weight: e.stepCount ? 2 : 1 }, e.id)
  }
  dagre.layout(g)

  const laidNodes = [...nodes.values()].map((n) => {
    const p = g.node(n.id)
    return { ...n, x: p?.x ?? 0, y: p?.y ?? 0 }
  })
  const laidEdges = edges.map((e) => {
    const points = (g.edge({ v: e.from, w: e.to, name: e.id })?.points ??
      []) as Point[]
    const last = points.at(-1) ?? { x: 0, y: 0 }
    const before = points.at(-2) ?? last
    const angle =
      (Math.atan2(last.y - before.y, last.x - before.x) * 180) / Math.PI
    const mid = points[Math.floor(points.length / 2)] ?? last
    return {
      ...e,
      path: smoothPath(points),
      head: { x: last.x, y: last.y, angle },
      labelX: mid.x + 8,
      labelY: mid.y + 3
    }
  })
  const graph = g.graph()
  return {
    nodes: laidNodes,
    edges: laidEdges,
    width: Math.ceil(graph.width ?? 0),
    height: Math.ceil(graph.height ?? 0)
  }
}
