// The object apps/api writes at c/<site>/<id> when a composite is created
// (service/composite_service.go encodeManifest): one line of JSON, "\n", then
// the bytes of every inline source in source order.

export interface Source {
  key?: string
  inline?: Uint8Array
}

export interface Manifest {
  size: number
  contentType: string
  contentDisposition: string
  etag: string
  lastModified: string
  sources: Source[]
  segSource: Uint32Array
  segOffset: Float64Array
  segLength: Float64Array
  segStart: Float64Array
}

interface Header {
  v: number
  size: number
  content_type: string
  content_disposition: string
  etag: string
  last_modified: string
  sources: Array<{ key?: string; inline?: number }>
  segments: Array<[number, number, number]>
}

export const parseManifest = (buf: ArrayBuffer): Manifest => {
  const bytes = new Uint8Array(buf)
  const nl = bytes.indexOf(0x0a)
  if (nl < 0) throw new Error('manifest: no header line')
  const h = JSON.parse(new TextDecoder().decode(bytes.subarray(0, nl))) as Header
  if (h.v !== 1) throw new Error(`manifest: unsupported version ${h.v}`)

  let pos = nl + 1
  const sources = h.sources.map((s): Source => {
    if (s.key) return { key: s.key }
    const inline = bytes.subarray(pos, pos + (s.inline ?? 0))
    pos += inline.length
    return { inline }
  })
  if (pos !== bytes.length) throw new Error('manifest: inline bytes do not match the header')

  const n = h.segments.length
  const m: Manifest = {
    size: h.size,
    contentType: h.content_type,
    contentDisposition: h.content_disposition,
    etag: h.etag,
    lastModified: h.last_modified,
    sources,
    segSource: new Uint32Array(n),
    segOffset: new Float64Array(n),
    segLength: new Float64Array(n),
    segStart: new Float64Array(n),
  }
  let at = 0
  h.segments.forEach(([source, offset, length], i) => {
    m.segSource[i] = source
    m.segOffset[i] = offset
    m.segLength[i] = length
    m.segStart[i] = at
    at += length
  })
  if (at !== h.size) throw new Error(`manifest: segments cover ${at} bytes, size is ${h.size}`)
  return m
}
