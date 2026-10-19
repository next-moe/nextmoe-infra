export type DiffOp = 'same' | 'add' | 'del'

export interface DiffSegment {
  op: DiffOp
  text: string
}

const MAX_CELLS = 250_000

const TOKEN = /[A-Za-z0-9_]+|\s+|[\s\S]/gu

export const tokenize = (text: string) => text.match(TOKEN) ?? []

const splitLines = (text: string) => text.match(/[^\n]*\n|[^\n]+/g) ?? []

const push = (out: DiffSegment[], op: DiffOp, text: string) => {
  if (!text) {
    return
  }
  const last = out.at(-1)
  if (last && last.op === op) {
    last.text += text
  } else {
    out.push({ op, text })
  }
}

const lcsDiff = (a: string[], b: string[], out: DiffSegment[]) => {
  const n = a.length
  const m = b.length
  if (n * m > MAX_CELLS) {
    push(out, 'del', a.join(''))
    push(out, 'add', b.join(''))
    return
  }
  const width = m + 1
  const table = new Uint32Array((n + 1) * width)
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      table[i * width + j] =
        a[i] === b[j]
          ? table[(i + 1) * width + j + 1]! + 1
          : Math.max(table[(i + 1) * width + j]!, table[i * width + j + 1]!)
    }
  }
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      push(out, 'same', a[i]!)
      i++
      j++
    } else if (table[(i + 1) * width + j]! >= table[i * width + j + 1]!) {
      push(out, 'del', a[i]!)
      i++
    } else {
      push(out, 'add', b[j]!)
      j++
    }
  }
  while (i < n) {
    push(out, 'del', a[i++]!)
  }
  while (j < m) {
    push(out, 'add', b[j++]!)
  }
}

const diffTokens = (a: string[], b: string[]) => {
  const out: DiffSegment[] = []
  let start = 0
  while (start < a.length && start < b.length && a[start] === b[start]) {
    start++
  }
  let endA = a.length
  let endB = b.length
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--
    endB--
  }
  push(out, 'same', a.slice(0, start).join(''))
  lcsDiff(a.slice(start, endA), b.slice(start, endB), out)
  push(out, 'same', a.slice(endA).join(''))
  return out
}

export const diffText = (before: string, after: string) =>
  diffTokens(tokenize(before), tokenize(after))

export const diffLines = (before: string, after: string) =>
  diffTokens(splitLines(before), splitLines(after))

const PROSE_TOKEN_LIMIT = 6000

export const diffProse = (before: string, after: string) =>
  before.length + after.length > PROSE_TOKEN_LIMIT
    ? diffLines(before, after)
    : diffText(before, after)

export const hasChanges = (segments: DiffSegment[]) =>
  segments.some((segment) => segment.op !== 'same')
