import { describe, expect, it } from 'vitest'
import { plan } from '../src/plan'
import { pump } from '../src/stream'
import { build, bytesOf, drain, fakeBucket, rng, same } from './helpers'

const splice = async (fx: ReturnType<typeof build>, start: number, end: number, chunk: () => number, small?: number) => {
  const bucket = fakeBucket(fx.objects, chunk)
  const { readable, writable } = new TransformStream<Uint8Array, Uint8Array>()
  const steps = plan(fx.manifest, start, end, small ? { small, gap: 64 << 10 } : undefined)
  const [out] = await Promise.all([drain(readable), pump(steps, writable, bucket.range)])
  return { out, calls: bucket.calls }
}

// The shape forge emits: the base minus an overridden entry, then each patch
// file as a generated local header (glue) followed by its data (layer), then
// the rewritten central directory (glue).
const zipLike = (seed: number) => {
  const next = rng(seed)
  const base = bytesOf(300_000, next)
  const layer = bytesOf(120_000, next)
  const glue = bytesOf(9_000, next)
  const segments: Array<[number, number, number]> = [[0, 0, 100_000], [0, 140_000, 150_000]]
  let g = 0
  let l = 40
  for (let i = 0; i < 8; i++) {
    segments.push([2, g, 70])
    g += 70
    const len = 2_000 + Math.floor(next() * 9_000)
    segments.push([1, l, len])
    l += len + 60
  }
  segments.push([2, g, 9_000 - g])
  return build([{ key: 'letmoe/base.zip', bytes: base }, { key: 'letmoe/layer.zip', bytes: layer }, { inline: glue }], segments)
}

describe('splice', () => {
  it('serves every byte range of a composite exactly', async () => {
    const fx = zipLike(1)
    const next = rng(2)
    const size = fx.expected.length
    const ranges: Array<[number, number]> = [[0, size], [0, 1], [size - 1, size], [99_990, 100_010], [289_000, 300_000]]
    for (let i = 0; i < 60; i++) {
      const a = Math.floor(next() * size)
      const b = a + 1 + Math.floor(next() * (size - a))
      ranges.push([a, b])
    }
    for (const [a, b] of ranges) {
      const { out } = await splice(fx, a, b, next)
      expect(out.length).toBe(b - a)
      expect(same(out, fx.expected.subarray(a, b))).toBe(true)
    }
  })

  it('fetches the patch files of one layer in a single request', async () => {
    const fx = zipLike(3)
    const { calls } = await splice(fx, 0, fx.expected.length, rng(4))
    expect(calls.filter((c) => c.key === 'letmoe/layer.zip')).toHaveLength(1)
    expect(calls.filter((c) => c.key === 'letmoe/base.zip')).toHaveLength(1)
  })

  it('gives a piece at or over the threshold a fetch of exactly its own bytes', async () => {
    const fx = zipLike(5)
    const { out, calls } = await splice(fx, 0, fx.expected.length, rng(6), 2_048)
    expect(same(out, fx.expected)).toBe(true)
    expect(calls.filter((c) => c.key === 'letmoe/layer.zip')).toHaveLength(8)
    expect(calls.find((c) => c.key === 'letmoe/base.zip' && c.offset === 140_000)?.length).toBe(150_000)
  })

  it('does not stretch a fetch across a gap wider than the limit', async () => {
    const next = rng(7)
    const src = bytesOf(400_000, next)
    const fx = build([{ key: 'k', bytes: src }], [[0, 0, 100], [0, 200_000, 100]])
    const { out, calls } = await splice(fx, 0, 200, next)
    expect(same(out, fx.expected)).toBe(true)
    expect(calls).toHaveLength(2)
  })

  it('errors the output when a source is shorter than the plan', async () => {
    const fx = zipLike(9)
    fx.objects.set('letmoe/layer.zip', fx.objects.get('letmoe/layer.zip')!.subarray(0, 1_000))
    const bucket = fakeBucket(fx.objects, rng(10))
    const { readable, writable } = new TransformStream<Uint8Array, Uint8Array>()
    const result = Promise.all([drain(readable), pump(plan(fx.manifest, 0, fx.expected.length), writable, bucket.range)])
    await expect(result).rejects.toThrow(/short/)
  })
})
