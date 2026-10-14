import type { Manifest } from './manifest'

export type Item = { mem: Uint8Array } | { at: number; length: number }

export type Step = { mem: Uint8Array } | { key: string; offset: number; length: number; items: Item[] }

export interface Coalesce {
  small: number
  gap: number
}

export const COALESCE: Coalesce = { small: 1 << 20, gap: 64 << 10 }

interface Piece {
  source: number
  offset: number
  length: number
}

const piecesIn = (m: Manifest, start: number, end: number): Piece[] => {
  let lo = 0
  let hi = m.segStart.length - 1
  while (lo < hi) {
    const mid = (lo + hi + 1) >>> 1
    if (m.segStart[mid]! <= start) lo = mid
    else hi = mid - 1
  }
  const out: Piece[] = []
  for (let i = lo; i < m.segStart.length && m.segStart[i]! < end; i++) {
    const segStart = m.segStart[i]!
    const from = Math.max(start, segStart)
    const to = Math.min(end, segStart + m.segLength[i]!)
    out.push({ source: m.segSource[i]!, offset: m.segOffset[i]! + (from - segStart), length: to - from })
  }
  return out
}

// A piece of `small` bytes or more gets a fetch of its own so it is piped
// natively; slicing a shared fetch runs in JavaScript and spends Worker CPU.
export const plan = (m: Manifest, start: number, end: number, c: Coalesce = COALESCE): Step[] => {
  const pieces = piecesIn(m, start, end)
  const memOf = (p: Piece): Uint8Array | undefined => m.sources[p.source]!.inline?.subarray(p.offset, p.offset + p.length)
  const steps: Step[] = []
  let i = 0
  while (i < pieces.length) {
    const p = pieces[i++]!
    const mem = memOf(p)
    if (mem) {
      steps.push({ mem })
      continue
    }
    const step = { key: m.sources[p.source]!.key!, offset: p.offset, length: p.length, items: [{ at: p.offset, length: p.length }] as Item[] }
    steps.push(step)
    if (p.length >= c.small) continue

    let reach = p.offset + p.length
    const pending: Item[] = []
    let j = i
    for (; j < pieces.length; j++) {
      const q = pieces[j]!
      const qmem = memOf(q)
      if (qmem) {
        pending.push({ mem: qmem })
        continue
      }
      if (q.source !== p.source || q.length >= c.small || q.offset < reach || q.offset - reach > c.gap) break
      step.items.push(...pending.splice(0), { at: q.offset, length: q.length })
      reach = q.offset + q.length
    }
    step.length = reach - step.offset
    i = j - pending.length
  }
  return steps
}
