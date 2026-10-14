export interface ByteRange {
  start: number
  end: number
}

export const UNSATISFIABLE = 'unsatisfiable'

const SPEC = /^bytes=(\d*)-(\d*)$/

// Anything but one well-formed byte range is ignored and answered in full,
// which RFC 9110 §14.2 permits; downloaders only ever send one.
export const parseRange = (header: string | null, size: number): ByteRange | typeof UNSATISFIABLE | null => {
  const m = header?.trim().match(SPEC)
  if (!m || (m[1] === '' && m[2] === '')) return null
  if (m[1] === '') {
    const suffix = Number(m[2])
    if (suffix === 0) return UNSATISFIABLE
    return { start: Math.max(0, size - suffix), end: size }
  }
  const start = Number(m[1])
  const last = m[2] === '' ? Infinity : Number(m[2])
  if (last < start) return null
  if (start >= size) return UNSATISFIABLE
  return { start, end: Math.min(last, size - 1) + 1 }
}

export const ifRangeHolds = (header: string | null, etag: string, lastModified: string): boolean => {
  if (header === null) return true
  const v = header.trim()
  if (v.startsWith('"') || v.startsWith('W/')) return v === etag
  const date = Date.parse(v)
  return !Number.isNaN(date) && date === Date.parse(lastModified)
}

export const noneMatch = (header: string | null, etag: string): boolean => {
  if (header === null) return false
  return header.split(',').some((tag) => {
    const t = tag.trim()
    return t === '*' || t.replace(/^W\//, '') === etag
  })
}
