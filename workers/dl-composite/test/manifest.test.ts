import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { parseManifest } from '../src/manifest'

describe('parseManifest', () => {
  it('reads the manifest apps/api writes (shared fixture)', () => {
    const file = readFileSync(new URL('./fixtures/manifest-v1.bin', import.meta.url))
    const m = parseManifest(file.buffer.slice(file.byteOffset, file.byteOffset + file.byteLength))
    expect(m.size).toBe(40)
    expect(m.contentType).toBe('application/zip')
    expect(m.etag).toBe(`"${'ab'.repeat(32)}"`)
    expect(m.lastModified).toBe('Wed, 14 Oct 2026 12:00:00 GMT')
    expect(m.contentDisposition.startsWith("attachment; filename*=UTF-8''")).toBe(true)
    expect(m.sources.map((s) => s.key ?? new TextDecoder().decode(s.inline))).toEqual([
      'letmoe/base.zip',
      'letmoe/layer.zip',
      'GLUE-0123456789',
    ])
    expect([...m.segStart]).toEqual([0, 10, 15, 25, 35])
    expect([...m.segSource]).toEqual([0, 2, 1, 2, 0])
  })

  it('refuses a manifest whose segments do not add up to its size', () => {
    const bad = new TextEncoder().encode(
      JSON.stringify({ v: 1, size: 5, sources: [{ key: 'k' }], segments: [[0, 0, 4]] }) + '\n',
    )
    expect(() => parseManifest(bad.buffer)).toThrow(/cover 4 bytes/)
  })
})
