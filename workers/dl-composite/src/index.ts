import { bucket, type Bucket } from './bucket'
import type { Env } from './env'
import { parseManifest, type Manifest } from './manifest'
import { plan } from './plan'
import { ifRangeHolds, noneMatch, parseRange, UNSATISFIABLE } from './range'
import { pump } from './stream'

const PATH = /^\/c\/([a-z0-9_-]{1,32})\/([0-9a-f]{32})(?:\.[A-Za-z0-9]{1,16})?$/
const MANIFEST_TTL_MS = 5 * 60 * 1000
const MANIFEST_CACHE_ENTRIES = 64

const manifests = new Map<string, { at: number; manifest: Promise<Manifest | null> }>()

const loadManifest = (store: Bucket, key: string): Promise<Manifest | null> => {
  const now = Date.now()
  const hit = manifests.get(key)
  if (hit && now - hit.at < MANIFEST_TTL_MS) return hit.manifest
  const manifest = store.get(key).then(async (res) => {
    if (res.status === 404) return null
    if (!res.ok) throw new Error(`manifest ${key} answered ${res.status}`)
    return parseManifest(await res.arrayBuffer())
  })
  manifests.delete(key)
  manifests.set(key, { at: now, manifest })
  manifest.catch(() => manifests.delete(key))
  if (manifests.size > MANIFEST_CACHE_ENTRIES) manifests.delete(manifests.keys().next().value!)
  return manifest
}

const text = (status: number, body: string, headers: HeadersInit = {}): Response =>
  new Response(body, { status, headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store', ...headers } })

const serve = (req: Request, m: Manifest, store: Bucket, ctx: ExecutionContext): Response => {
  const headers = new Headers({
    'Content-Type': m.contentType,
    'Content-Disposition': m.contentDisposition,
    ETag: m.etag,
    'Last-Modified': m.lastModified,
    'Accept-Ranges': 'bytes',
    'Cache-Control': 'public, max-age=14400, no-transform',
  })
  if (noneMatch(req.headers.get('If-None-Match'), m.etag)) return new Response(null, { status: 304, headers })

  let start = 0
  let end = m.size
  let status = 200
  if (ifRangeHolds(req.headers.get('If-Range'), m.etag, m.lastModified)) {
    const range = parseRange(req.headers.get('Range'), m.size)
    if (range === UNSATISFIABLE) return text(416, 'range not satisfiable\n', { 'Content-Range': `bytes */${m.size}` })
    if (range) {
      ;({ start, end } = range)
      status = 206
      headers.set('Content-Range', `bytes ${start}-${end - 1}/${m.size}`)
    }
  }

  if (req.method === 'HEAD') {
    headers.set('Content-Length', String(end - start))
    return new Response(null, { status, headers })
  }
  const { readable, writable } = new FixedLengthStream(end - start)
  ctx.waitUntil(
    pump(plan(m, start, end), writable, store.range).catch((err) => {
      console.error('composite pump failed', req.url, `bytes ${start}-${end - 1}`, String(err))
    }),
  )
  return new Response(readable, { status, headers })
}

export default {
  async fetch(req, env, ctx): Promise<Response> {
    if (req.method !== 'GET' && req.method !== 'HEAD') return text(405, 'method not allowed\n', { Allow: 'GET, HEAD' })
    const path = new URL(req.url).pathname.match(PATH)
    if (!path) return text(404, 'not found\n')

    const store = bucket(env)
    let manifest: Manifest | null
    try {
      manifest = await loadManifest(store, `c/${path[1]}/${path[2]}`)
    } catch (err) {
      console.error('composite manifest failed', req.url, String(err))
      return text(502, 'upstream unavailable\n')
    }
    if (!manifest) return text(404, 'not found\n')
    return serve(req, manifest, store, ctx)
  },
} satisfies ExportedHandler<Env>
