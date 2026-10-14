import { parseManifest, type Manifest } from '../src/manifest'
import type { RangeFetcher } from '../src/stream'

export const rng = (seed: number) => () => {
  seed = (seed * 1664525 + 1013904223) >>> 0
  return seed / 2 ** 32
}

export const bytesOf = (n: number, next: () => number): Uint8Array => {
  const out = new Uint8Array(n)
  for (let i = 0; i < n; i++) out[i] = Math.floor(next() * 256)
  return out
}

export interface Fixture {
  manifest: Manifest
  objects: Map<string, Uint8Array>
  expected: Uint8Array
}

export const build = (
  sources: Array<{ key: string; bytes: Uint8Array } | { inline: Uint8Array }>,
  segments: Array<[number, number, number]>,
): Fixture => {
  const objects = new Map<string, Uint8Array>()
  const tails: Uint8Array[] = []
  const header = {
    v: 1,
    size: segments.reduce((a, s) => a + s[2], 0),
    content_type: 'application/zip',
    content_disposition: "attachment; filename*=UTF-8''x.zip",
    etag: '"e"',
    last_modified: 'Wed, 14 Oct 2026 12:00:00 GMT',
    sources: sources.map((s) => {
      if ('key' in s) {
        objects.set(s.key, s.bytes)
        return { key: s.key }
      }
      tails.push(s.inline)
      return { inline: s.inline.length }
    }),
    segments,
  }
  const head = new TextEncoder().encode(JSON.stringify(header) + '\n')
  const buf = new Uint8Array(head.length + tails.reduce((a, t) => a + t.length, 0))
  buf.set(head)
  let at = head.length
  for (const t of tails) {
    buf.set(t, at)
    at += t.length
  }
  const expected = new Uint8Array(header.size)
  let out = 0
  for (const [src, off, len] of segments) {
    const s = sources[src]!
    const from = 'key' in s ? s.bytes : s.inline
    expected.set(from.subarray(off, off + len), out)
    out += len
  }
  return { manifest: parseManifest(buf.buffer), objects, expected }
}

export const fakeBucket = (objects: Map<string, Uint8Array>, chunk: () => number) => {
  const calls: Array<{ key: string; offset: number; length: number }> = []
  const range: RangeFetcher = async (key, offset, length) => {
    calls.push({ key, offset, length })
    const bytes = objects.get(key)!.subarray(offset, offset + length)
    let pos = 0
    return new ReadableStream<Uint8Array>({
      pull(ctrl) {
        if (pos >= bytes.length) return ctrl.close()
        const n = Math.max(1, Math.floor(chunk() * 4096))
        ctrl.enqueue(bytes.slice(pos, pos + n))
        pos += n
      },
    })
  }
  return { range, calls }
}

export const drain = async (readable: ReadableStream<Uint8Array>): Promise<Uint8Array> => {
  const parts: Uint8Array[] = []
  const reader = readable.getReader()
  for (;;) {
    const r = await reader.read()
    if (r.done) break
    parts.push(r.value)
  }
  const out = new Uint8Array(parts.reduce((a, p) => a + p.length, 0))
  let at = 0
  for (const p of parts) {
    out.set(p, at)
    at += p.length
  }
  return out
}

export const same = (a: Uint8Array, b: Uint8Array): boolean => a.length === b.length && a.every((v, i) => v === b[i])
