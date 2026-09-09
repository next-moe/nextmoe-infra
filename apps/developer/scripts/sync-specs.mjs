#!/usr/bin/env node
/**
 * Builds the typed documentation model the self-built API reference (/docs/**)
 * renders. Reads the Tier-A specs from the repo's docs/, parses them,
 * dereferences $ref pointers inline into render-friendly schema trees, derives
 * parameter tables / request bodies / responses, generates a ready-to-run curl
 * example per operation, and writes `app/generated/docs-model.ts`.
 *
 * It then hands the same model to scripts/gen-llms.mjs, which writes the
 * LLM-facing half into public/: llms.txt, llms-full.txt, and one Markdown twin
 * per docs route. One entry point on purpose — CI runs this file and nothing
 * else, so a spec change cannot refresh the reference pages while leaving the
 * Markdown twins describing the old contract.
 *
 * The HTML /docs pages render a Chinese overlay of this English model
 * (i18n/docs-zh.json, asserted below). The Markdown twins stay English so
 * agents keep the contract language they were trained on. The RFC 9457 problem
 * registry and the /v2/vocabularies registry get the same treatment through
 * i18n/problems-zh.json and i18n/vocabularies-zh.json, except that their
 * English source is Go-generated rather than derived here.
 *
 * The generated files are committed (same pattern as app/assets/kun-icons.ts):
 * derived build artifacts, never hand-edited. Re-run after the specs change:
 *   pnpm --filter developer sync:specs
 *
 * The galgame face was dropped at wave 146 (2026-07-30): its /v1/galgame
 * projection was delisted and its spec deleted. Wave R3 (2026-08-27) did the
 * same to the whole v1 surface — the catalog public projection, playtime, the
 * user-edit subset, news and store — leaving /v2 as the only first-party face.
 * The vendored downstream specs in docs/downstream/ carry the federated moyu
 * and sticker faces alongside it.
 *
 * Operation GROUPING is derived here, not in the specs: the OpenAPI tags put
 * every operation of a face in one bucket, which is no navigation at all for an
 * 90-operation face. The spec YAML is a frozen contract and must not be edited
 * to carry portal IA.
 */
import {
  copyFileSync,
  readFileSync,
  writeFileSync,
  mkdirSync,
  rmSync
} from 'node:fs'
import { basename, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse as parseYaml } from 'yaml'
import {
  API_HOST,
  EXPECTED_OPERATION_COUNTS,
  FACES,
  NO_AUTH,
  USER_TOKEN_AUTH,
  V2_SPEC
} from './faces.mjs'
import { writeLlmArtifacts } from './gen-llms.mjs'
import { assertDocZhCatalog, loadDocZhCatalog } from './docs-i18n.mjs'
import { buildProblemsZh, loadProblems } from './problems-i18n.mjs'
import { buildVocabulariesZh, loadVocabularies } from './vocabularies-i18n.mjs'
import { buildGuides } from './build-guides.mjs'
import { buildSearchIndex } from './search-index.mjs'

const __dirname = dirname(fileURLToPath(import.meta.url))
const OUTPUT = join(__dirname, '..', 'app/generated/docs-model.ts')
const PROBLEMS_ZH_OUT = join(__dirname, '..', 'app/generated/problems-zh.ts')
const VOCAB_ZH_OUT = join(__dirname, '..', 'app/generated/vocabularies-zh.ts')
const SEARCH_OUT = join(__dirname, '..', 'app/generated/search-index.ts')
const GUIDES_OUT = join(__dirname, '..', 'app/generated/guides')
const GUIDES_NAV_OUT = join(__dirname, '..', 'app/generated/guides-nav.ts')
const PUBLIC_DIR = join(__dirname, '..', 'public')
const SPECS_DIR = join(PUBLIC_DIR, 'specs')

const refName = (ref) => ref.split('/').pop()

const splitType = (rawType) => {
  const types = Array.isArray(rawType) ? rawType : rawType ? [rawType] : []
  return {
    primary: types.find((t) => t !== 'null'),
    nullable: types.includes('null')
  }
}

// The v2 spec has no allOf, so for a long time the builder had no branch for
// one. The sticker face is written entirely in it — every list response is
// `allOf: [List, {items}]` — and an unmerged allOf falls through to a bare
// `object` with no children, which rendered seven of its nine operations with an
// empty response schema and hid Pack's fields from the overlay entirely.
const flattenAllOf = (schema, { schemas, seen }) => {
  const merged = { ...schema }
  delete merged.allOf
  const properties = { ...schema.properties }
  const required = new Set(schema.required)
  for (const member of schema.allOf) {
    let sub = member
    while (sub?.$ref) {
      const rn = refName(sub.$ref)
      if (seen.has(rn)) {
        sub = undefined
        break
      }
      seen.add(rn)
      sub = schemas[rn]
      if (!sub) throw new Error(`unresolved $ref: ${member.$ref}`)
    }
    if (!sub) continue
    if (sub.allOf) sub = flattenAllOf(sub, { schemas, seen })
    Object.assign(properties, sub.properties)
    for (const key of sub.required || []) required.add(key)
    merged.type ??= sub.type
    merged.description ??= sub.description
  }
  if (Object.keys(properties).length) merged.properties = properties
  if (required.size) merged.required = [...required]
  merged.type ??= 'object'
  return merged
}

const buildNode = (schema, { name, required, schemas, seen }) => {
  if (schema.allOf) {
    const next = new Set(seen)
    return buildNode(flattenAllOf(schema, { schemas, seen: next }), {
      name,
      required,
      schemas,
      seen: next
    })
  }
  if (schema.$ref) {
    const rn = refName(schema.$ref)
    if (seen.has(rn)) {
      return {
        ...(name !== undefined && { name }),
        type: rn,
        ...(required && { required })
      }
    }
    const resolved = schemas[rn]
    if (!resolved) throw new Error(`unresolved $ref: ${schema.$ref}`)
    const next = new Set(seen)
    next.add(rn)
    return buildNode(resolved, { name, required, schemas, seen: next })
  }

  const { primary, nullable } = splitType(schema.type)
  const node = {}
  if (name !== undefined) node.name = name
  if (required) node.required = true
  if (nullable) node.nullable = true
  if (schema.description) node.doc = schema.description
  if (schema.format) node.format = schema.format
  if (schema.enum) node.enum = stringEnum(schema.enum)

  if (primary === 'object' && schema.properties) {
    node.type = 'object'
    const req = new Set(schema.required || [])
    node.children = Object.keys(schema.properties)
      .filter((k) => k !== '$schema')
      .map((k) =>
        buildNode(schema.properties[k], {
          name: k,
          required: req.has(k),
          schemas,
          seen
        })
      )
  } else if (
    primary === 'object' &&
    schema.additionalProperties &&
    typeof schema.additionalProperties === 'object'
  ) {
    node.type = 'map'
    node.itemsOf = buildNode(schema.additionalProperties, { schemas, seen })
  } else if (primary === 'array') {
    node.type = 'array'
    node.itemsOf = schema.items
      ? buildNode(schema.items, { schemas, seen })
      : { type: 'any' }
  } else {
    node.type = primary || 'object'
  }
  return node
}

const sampleValue = (schema, { schemas, seen }) => {
  if (schema.$ref) {
    const rn = refName(schema.$ref)
    if (seen.has(rn)) return null
    const next = new Set(seen)
    next.add(rn)
    return sampleValue(schemas[rn], { schemas, seen: next })
  }
  if (schema.enum) return schema.enum[0]
  const { primary } = splitType(schema.type)
  switch (primary) {
    case 'object': {
      if (schema.properties) {
        const req = new Set(schema.required || [])
        const obj = {}
        for (const k of Object.keys(schema.properties)) {
          if (k === '$schema' || !req.has(k)) continue
          obj[k] = sampleValue(schema.properties[k], { schemas, seen })
        }
        return obj
      }
      if (
        schema.additionalProperties &&
        typeof schema.additionalProperties === 'object'
      ) {
        return {
          key: sampleValue(schema.additionalProperties, { schemas, seen })
        }
      }
      return {}
    }
    case 'array':
      return schema.items ? [sampleValue(schema.items, { schemas, seen })] : []
    case 'integer':
    case 'number':
      return 0
    case 'boolean':
      return true
    default:
      return 'string'
  }
}

const samplePathValue = (param) => {
  if (param.enum?.length) return String(param.enum[0])
  return param.type === 'integer' || param.type === 'number' ? '1' : 'value'
}

const buildCurl = (method, path, params, bodyExample, authHeader) => {
  let url = API_HOST + path
  for (const p of params) {
    if (p.in === 'path') url = url.replace(`{${p.name}}`, samplePathValue(p))
  }
  const verb = method.toUpperCase()
  const lines = [verb === 'GET' ? `curl "${url}"` : `curl -X ${verb} "${url}"`]
  if (authHeader) lines.push(`  -H "${authHeader}"`)
  if (bodyExample !== undefined) {
    lines.push('  -H "Content-Type: application/json"')
    lines.push(`  -d '${JSON.stringify(bodyExample)}'`)
  }
  return lines.join(' \\\n')
}

// A JSON Schema enum may list null next to its string members; the model says
// nullable separately and DocsSchemaNode.enum is string[], so the null member is
// dropped rather than typed around. moyu's content_limit is the only one today.
const stringEnum = (values) => values.filter((v) => v !== null)

// Same story as derefResponse: v2 inlines every parameter, both downstream specs
// share theirs through components/parameters, and an unresolved one produced a
// param row with no name and no `in` at all.
const derefParam = (param, paramDefs) => {
  const seen = new Set()
  let cur = param
  while (cur?.$ref) {
    const rn = refName(cur.$ref)
    if (seen.has(rn)) break
    seen.add(rn)
    const next = paramDefs[rn]
    if (!next) throw new Error(`unresolved $ref: ${cur.$ref}`)
    cur = next
  }
  return cur
}

const buildParams = (rawParams = [], paramDefs = {}) => {
  const mapped = rawParams.map((raw) => {
    const p = derefParam(raw, paramDefs)
    const s = p.schema || {}
    const { primary } = splitType(s.type)
    const param = {
      name: p.name,
      in: p.in,
      required: !!p.required,
      type: primary || 'string'
    }
    if (s.format) param.format = s.format
    if (p.description || s.description)
      param.doc = p.description || s.description
    if (s.enum) param.enum = stringEnum(s.enum)
    return param
  })
  return mapped.sort((a, b) => {
    if (a.in === b.in) return 0
    return a.in === 'path' ? -1 : 1
  })
}

const jsonContent = (content) =>
  content?.['application/json'] || content?.['application/problem+json']

// v2 spells every response inline, so a $ref pointing at components/responses
// used to read as a response with neither description nor schema and rendered as
// a bare status line. Both downstream specs write their shared 304/400/404/503
// that way, and sticker writes its 200 that way too.
const derefResponse = (res, responses) => {
  const seen = new Set()
  let cur = res
  while (cur?.$ref) {
    const rn = refName(cur.$ref)
    if (seen.has(rn)) break
    seen.add(rn)
    const next = responses[rn]
    if (!next) throw new Error(`unresolved $ref: ${cur.$ref}`)
    cur = next
  }
  return cur
}

const authForPath = (faceDef, path) => {
  if (faceDef.key !== 'v2') return faceDef.auth
  if (path.startsWith('/v2/me/') || path.startsWith('/v2/moderation/'))
    return USER_TOKEN_AUTH
  if (
    path.startsWith('/v2/problems') ||
    path.startsWith('/v2/vocabularies') ||
    path.startsWith('/v2/news') ||
    path === '/v2/catalog/stats' ||
    path.startsWith('/v2/catalog/schemas/')
  ) {
    return NO_AUTH
  }
  return faceDef.auth
}

const buildOperation = (
  method,
  path,
  op,
  { schemas, responseDefs, paramDefs, scope, auth, faceAuth }
) => {
  const params = buildParams(op.parameters, paramDefs)

  let requestBody
  let bodyExample
  const bodySchema = jsonContent(op.requestBody?.content)?.schema
  if (bodySchema) {
    requestBody = buildNode(bodySchema, { schemas, seen: new Set() })
    bodyExample = sampleValue(bodySchema, { schemas, seen: new Set() })
  }

  const responses = Object.entries(op.responses || {}).map(([status, raw]) => {
    const res = derefResponse(raw, responseDefs)
    const schema = jsonContent(res.content)?.schema
    return {
      status,
      description: res.description || '',
      ...(schema && { schema: buildNode(schema, { schemas, seen: new Set() }) })
    }
  })

  return {
    id: op.operationId,
    method,
    path,
    summary: op.summary || '',
    ...(op.description && { description: op.description }),
    scope,
    // Whatever departs from the face default must land in the model — the
    // renderers fall back to face.auth for an op without its own. Until
    // 2026-08-26 only the per-operation override table was emitted, so every
    // credential-free /v2 operation's page and Markdown twin printed the face's
    // "Bearer nmk_live_…" line, and /v2/me printed a machine key for a face
    // that takes a user token.
    ...(auth !== faceAuth && { auth }),
    params,
    ...(requestBody && { requestBody }),
    responses,
    curl: buildCurl(method, path, params, bodyExample, auth.curl)
  }
}

const METHODS = ['get', 'post', 'put', 'patch', 'delete']

const opKey = (method, path) => `${method.toUpperCase()} ${path}`

const autoGroupDefs = (faceDef, spec) => {
  const defs = faceDef.autoGroups.map((g) => ({
    key: g.key,
    label: g.label,
    ops: []
  }))
  for (const [path, item] of Object.entries(spec.paths || {})) {
    if (!path.startsWith(faceDef.prefix)) continue
    for (const method of METHODS) {
      if (!item[method]) continue
      const group = defs.find((g) => {
        const src = faceDef.autoGroups.find((a) => a.key === g.key)
        return src.match.test(path)
      })
      if (!group) {
        throw new Error(
          `docs-model grouping guard: ${opKey(method, path)} matches no autoGroup of face ${faceDef.key}`
        )
      }
      group.ops.push(opKey(method, path))
    }
  }
  return defs
}

const buildFace = (faceDef, specs) => {
  const spec = specs.get(faceDef.file)
  const schemas = spec.components?.schemas || {}
  const responseDefs = spec.components?.responses || {}
  const paramDefs = spec.components?.parameters || {}

  const groupDefs = autoGroupDefs(faceDef, spec)
  const buckets = new Map(groupDefs.map((g) => [g.key, []]))
  const placed = new Set()

  for (const [path, item] of Object.entries(spec.paths || {})) {
    if (!path.startsWith(faceDef.prefix)) continue
    for (const method of METHODS) {
      const op = item[method]
      if (!op) continue
      const key = opKey(method, path)
      const group = groupDefs.find((g) => g.ops.includes(key))
      if (!group) {
        throw new Error(
          `docs-model grouping guard: ${key} (${op.operationId}) belongs to no group of face ${faceDef.key} — add an autoGroup`
        )
      }
      placed.add(key)
      const auth = authForPath(faceDef, path)
      const scopeFn =
        faceDef.scope.length >= 2
          ? faceDef.scope(method, path)
          : faceDef.scope(method)
      buckets.get(group.key).push(
        buildOperation(method, path, op, {
          schemas,
          responseDefs,
          paramDefs,
          scope: scopeFn,
          auth,
          faceAuth: faceDef.auth
        })
      )
    }
  }

  for (const group of groupDefs) {
    for (const key of group.ops) {
      if (!placed.has(key)) {
        throw new Error(
          `docs-model grouping guard: face ${faceDef.key} group ${group.key} lists ${key}, which the spec no longer has`
        )
      }
    }
  }

  return {
    key: faceDef.key,
    label: faceDef.label,
    name: faceDef.name,
    baseUrl: API_HOST,
    prefix: faceDef.prefix,
    ...(faceDef.specUrl && { specUrl: faceDef.specUrl }),
    auth: faceDef.auth,
    ...(faceDef.notes?.length && { notes: faceDef.notes }),
    groups: groupDefs.map((g) => ({
      key: g.key,
      label: g.label,
      operations: buckets
        .get(g.key)
        .sort(
          (a, b) =>
            g.ops.indexOf(opKey(a.method, a.path)) -
            g.ops.indexOf(opKey(b.method, b.path))
        )
    }))
  }
}

const specs = new Map()
for (const faceDef of FACES) {
  if (!specs.has(faceDef.file)) {
    specs.set(faceDef.file, parseYaml(readFileSync(faceDef.file, 'utf8')))
  }
}

const model = { faces: FACES.map((f) => buildFace(f, specs)) }

const faceOpCount = (f) => f.groups.reduce((m, g) => m + g.operations.length, 0)

for (const face of model.faces) {
  const want = EXPECTED_OPERATION_COUNTS[face.key]
  const got = faceOpCount(face)
  if (want !== got) {
    throw new Error(
      `docs-model coverage guard: face ${face.key} expected ${want} operations, built ${got}`
    )
  }
}

// A path the prefixes do not claim would vanish from the reference with every
// other guard still green, which is how five playtime operations shipped
// documented as catalog ones.
for (const file of new Set(FACES.map((f) => f.file))) {
  const claimed = new Set(
    model.faces
      .filter((f) => FACES.find((d) => d.key === f.key)?.file === file)
      .flatMap((f) => f.groups.flatMap((g) => g.operations.map((o) => o.path)))
  )
  for (const path of Object.keys(specs.get(file).paths || {})) {
    if (!claimed.has(path)) {
      throw new Error(
        `docs-model coverage guard: spec path ${path} belongs to no face — add one to FACES`
      )
    }
  }
}

const opCount = model.faces.reduce((n, f) => n + faceOpCount(f), 0)

const docZh = loadDocZhCatalog()
assertDocZhCatalog(model, docZh)
const tZh = (text) => (text ? (docZh[text] ?? text) : '')

const out = `/**
 * Auto-generated by scripts/sync-specs.mjs — do not edit by hand.
 * Run \`pnpm --filter developer sync:specs\` after the public specs change.
 *
 * The Tier-A v2 spec plus the vendored downstream specs, projected into the
 * render-friendly DocsModel the /docs/** reference pages consume, one entry
 * per public face.
 */
import type { DocsModel } from '~~/shared/types/docs'

export const docsModel: DocsModel = ${JSON.stringify(model, null, 2)}
`

mkdirSync(dirname(OUTPUT), { recursive: true })
writeFileSync(OUTPUT, out)
const breakdown = model.faces
  .map((f) => `${f.key} ${faceOpCount(f)}`)
  .join(', ')
console.log(
  `Wrote docs model → app/generated/docs-model.ts (${model.faces.length} faces, ${opCount} operations: ${breakdown})`
)

// The problem registry itself is generated by cmd/gen-v2-portal and stays
// English — it is the wire contract. Only its Chinese twin is written here, so
// a new problem code with no translation fails this job the same way a stale
// docs model does.
const problems = loadProblems()
const problemsZh = buildProblemsZh(problems)
writeFileSync(
  PROBLEMS_ZH_OUT,
  `/**
 * Auto-generated by scripts/sync-specs.mjs — do not edit by hand.
 * Source: app/generated/problems.ts + i18n/problems-zh.json.
 *
 * Reader-facing only. The English title/description on the registry stay the
 * contract and the /problems pages keep rendering them alongside these.
 */
export const problemDomainsZh: Record<string, string> = ${JSON.stringify(
    problemsZh.domains,
    null,
    2
  )}

export const problemsZh: Record<string, { title: string; description: string }> = ${JSON.stringify(
    problemsZh.entries,
    null,
    2
  )}
`
)
console.log(
  `Wrote problem overlay → app/generated/problems-zh.ts (${Object.keys(problemsZh.entries).length} codes, ${Object.keys(problemsZh.domains).length} domains)`
)

// Same deal for the vocabulary registry: cmd/gen-v2-portal writes the English
// app/generated/vocabularies.ts, and only its Chinese twin is written here.
const vocabularies = loadVocabularies()
const vocabulariesZh = buildVocabulariesZh(vocabularies)
writeFileSync(
  VOCAB_ZH_OUT,
  `/**
 * Auto-generated by scripts/sync-specs.mjs — do not edit by hand.
 * Source: app/generated/vocabularies.ts + i18n/vocabularies-zh.json.
 *
 * Reader-facing only. The value tokens and the English display_name /
 * description stay the contract — they are what GET /v2/vocabularies returns —
 * and the pages render these alongside them.
 */
export interface VocabularyZhMeta {
  label: string
  summary: string
}

export interface VocabularyZhValue {
  displayName: string
  description: string
}

export const vocabularyMetaZh: Record<string, VocabularyZhMeta> = ${JSON.stringify(
    vocabulariesZh.meta,
    null,
    2
  )}

export const vocabularyValuesZh: Record<
  string,
  Record<string, VocabularyZhValue>
> = ${JSON.stringify(vocabulariesZh.values, null, 2)}
`
)
console.log(
  `Wrote vocabulary overlay → app/generated/vocabularies-zh.ts (${vocabularies.length} vocabularies, ${vocabularies.reduce((n, v) => n + v.values.length, 0)} tokens)`
)

// The hand-written guides compile from apps/developer/docs/*.md through the
// same entry point on purpose: the HTML the pages render and the Markdown twin
// an agent reads come out of one run, so they cannot describe different things.
const guides = buildGuides()
rmSync(GUIDES_OUT, { recursive: true, force: true })
mkdirSync(GUIDES_OUT, { recursive: true })
for (const page of guides.pages) {
  // `markdown` stays out of the client artifact: the page renders `html`, and
  // the raw source is served as the route's Markdown twin from public/.
  const { slug, route, title, eyebrow, description, toc, html } = page
  writeFileSync(
    join(GUIDES_OUT, `${slug}.json`),
    JSON.stringify({ slug, route, title, eyebrow, description, toc, html })
  )
}
writeFileSync(
  GUIDES_NAV_OUT,
  `/**
 * Auto-generated by scripts/sync-specs.mjs — do not edit by hand.
 * Source: apps/developer/docs/*.md, ordered by scripts/guides.mjs.
 */
import type { GuideMeta, GuideNavSection } from '~~/shared/types/guides'

export const guideNav: GuideNavSection[] = ${JSON.stringify(guides.nav, null, 2)}

export const guideMeta: Record<string, GuideMeta> = ${JSON.stringify(
    Object.fromEntries(
      guides.pages.map((p) => [
        p.route,
        {
          slug: p.slug,
          title: p.title,
          eyebrow: p.eyebrow,
          description: p.description
        }
      ])
    ),
    null,
    2
  )}
`
)
console.log(
  `Wrote guides → app/generated/guides/ (${guides.pages.length} pages from docs/*.md)`
)

// The ⌘K palette's haystack. Last, because it is the only artifact that needs
// every other one: the reference model, the compiled guides, and both Go-
// generated registries with their Chinese overlays.
const searchIndex = buildSearchIndex({
  model,
  guides: guides.pages,
  problems,
  problemsZh,
  vocabularies,
  vocabulariesZh,
  t: tZh
})
writeFileSync(
  SEARCH_OUT,
  `/**
 * Auto-generated by scripts/sync-specs.mjs — do not edit by hand.
 *
 * The whole searchable surface of the portal, one entry per route. Only
 * components/layout/Search.vue may import it, and only through a dynamic
 * import: it is a standalone chunk fetched the first time the palette opens.
 */
import type { SearchEntry } from '~~/shared/types/search'

export const searchIndex: SearchEntry[] = ${JSON.stringify(searchIndex, null, 2)}
`
)
console.log(
  `Wrote search index → app/generated/search-index.ts (${searchIndex.length} pages)`
)

// v2 publishes its own OpenAPI from the running routes, so its specUrl is live
// and nothing is copied. A downstream face is served by another repo and has no
// public spec endpoint of its own — the portal serves the vendored mirror.
const vendoredSpecs = FACES.filter((f) => f.file !== V2_SPEC)
mkdirSync(SPECS_DIR, { recursive: true })
for (const faceDef of vendoredSpecs) {
  copyFileSync(faceDef.file, join(SPECS_DIR, basename(faceDef.file)))
}
console.log(
  `Wrote face specs → public/specs/ (${vendoredSpecs.length} vendored: ${vendoredSpecs.map((f) => basename(f.file)).join(', ')})`
)

const llmFiles = writeLlmArtifacts(
  model,
  guides.pages,
  problems,
  vocabularies,
  PUBLIC_DIR
)
console.log(
  `Wrote LLM artifacts → public/ (llms.txt, llms-full.txt, ${llmFiles - 2} route Markdown twins)`
)
