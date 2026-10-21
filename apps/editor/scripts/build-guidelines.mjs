#!/usr/bin/env node
// Compiles content/guidelines/*.md into app/generated/guidelines.ts, so
// markdown-it never enters the client bundle. Re-run with
// `pnpm --filter editor guidelines`; `--check` fails when the file is stale.
import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import matter from 'gray-matter'
import MarkdownIt from 'markdown-it'
import cjkFriendly from 'markdown-it-cjk-friendly'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const CONTENT_DIR = join(ROOT, 'content/guidelines')
const OUTPUT = join(ROOT, 'app/generated/guidelines.ts')

const CATEGORIES = ['start', 'rules', 'work', 'people', 'vocab', 'appendix']

const ALERTS = {
  NOTE: 'info',
  TIP: 'success',
  IMPORTANT: 'primary',
  WARNING: 'warning',
  CAUTION: 'danger'
}
const ALERT_MARK = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*/
const EXPLICIT_ID = /\s*\{#([A-Za-z0-9_-]+)\}\s*$/

const slugify = (text) =>
  String(text)
    .trim()
    .toLowerCase()
    .replace(/[^\w一-龥]+/g, '-')
    .replace(/^-+|-+$/g, '')

const md = MarkdownIt({
  html: false,
  linkify: false,
  typographer: false
}).use(cjkFriendly)

md.core.ruler.push('heading_ids', (state) => {
  const used = new Set()
  for (let i = 0; i < state.tokens.length; i++) {
    const open = state.tokens[i]
    if (open.type !== 'heading_open') continue
    const inline = state.tokens[i + 1]
    if (inline?.type !== 'inline') continue
    const explicit = inline.content.match(EXPLICIT_ID)
    let id
    if (explicit) {
      id = explicit[1]
      inline.content = inline.content.replace(EXPLICIT_ID, '')
      const last = inline.children?.[inline.children.length - 1]
      if (last?.type === 'text') {
        last.content = last.content.replace(EXPLICIT_ID, '')
      }
    } else {
      id = slugify(inline.content)
    }
    let unique = id
    for (let n = 2; used.has(unique); n++) unique = `${id}-${n}`
    used.add(unique)
    open.attrSet('id', unique)
  }
})

md.core.ruler.push('alerts', (state) => {
  const tokens = state.tokens
  for (let i = 0; i < tokens.length; i++) {
    if (tokens[i].type !== 'blockquote_open') continue
    const inline = tokens[i + 2]
    if (inline?.type !== 'inline') continue
    const mark = inline.content.match(ALERT_MARK)
    if (!mark) continue
    tokens[i].attrJoin('class', `guide-alert guide-alert-${ALERTS[mark[1]]}`)
    inline.content = inline.content.replace(ALERT_MARK, '')
    const kids = inline.children ?? []
    if (kids[0]?.type === 'text') {
      kids[0].content = kids[0].content.replace(ALERT_MARK, '')
    }
    while (
      kids.length &&
      (kids[0].type === 'softbreak' ||
        (kids[0].type === 'text' && !kids[0].content))
    ) {
      kids.shift()
    }
  }
})

md.renderer.rules.table_open = () => '<div class="guide-table"><table>'
md.renderer.rules.table_close = () => '</table></div>'

const headingsOf = (tokens) => {
  const out = []
  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i]
    if (token.type !== 'heading_open') continue
    const depth = Number(token.tag.slice(1))
    const text = tokens[i + 1]?.content ?? ''
    if (depth >= 2 && depth <= 3 && text) {
      out.push({ id: token.attrGet('id'), text, depth })
    }
  }
  return out
}

const linksOf = (tokens) => {
  const out = []
  const walk = (list) => {
    for (const token of list) {
      if (token.type === 'link_open') out.push(token.attrGet('href'))
      if (token.children) walk(token.children)
    }
  }
  walk(tokens)
  return out
}

const plainText = (tokens) => {
  const parts = []
  const walk = (list) => {
    for (const token of list) {
      if (token.type === 'text' || token.type === 'code_inline') {
        parts.push(token.content)
      }
      if (token.children) walk(token.children)
    }
  }
  walk(tokens)
  return parts.join(' ').replace(/\s+/g, ' ').trim()
}

const required = (data, field, slug) => {
  const value = data[field]
  if (typeof value === 'string' && value.trim()) return value.trim()
  throw new Error(
    `content/guidelines/${slug}.md: missing frontmatter "${field}"`
  )
}

export const buildGuidelines = () => {
  const files = readdirSync(CONTENT_DIR)
    .filter((name) => name.endsWith('.md'))
    .sort()
  const entries = files.map((name) => {
    const slug = name.slice(0, -'.md'.length)
    const { content, data } = matter(
      readFileSync(join(CONTENT_DIR, name), 'utf8')
    )
    const category = required(data, 'category', slug)
    if (!CATEGORIES.includes(category)) {
      throw new Error(`${slug}.md: unknown category "${category}"`)
    }
    const tokens = md.parse(content, {})
    return {
      doc: {
        slug,
        title: required(data, 'title', slug),
        description: required(data, 'description', slug),
        category,
        order: Number(data.order ?? 0),
        icon: required(data, 'icon', slug),
        related: Array.isArray(data.related) ? data.related.map(String) : [],
        headings: headingsOf(tokens),
        text: plainText(tokens),
        html: md.renderer.render(tokens, md.options, {})
      },
      links: linksOf(tokens),
      anchors: tokens
        .filter((token) => token.type === 'heading_open')
        .map((token) => token.attrGet('id'))
    }
  })

  const bySlug = new Map(entries.map((entry) => [entry.doc.slug, entry]))
  const problems = []
  for (const { doc, links } of entries) {
    for (const slug of doc.related) {
      if (!bySlug.has(slug)) {
        problems.push(`${doc.slug}.md: related "${slug}" does not exist`)
      }
    }
    for (const href of links) {
      if (!href?.startsWith('/guidelines')) continue
      const [path, anchor] = href.split('#')
      const target = path.replace(/^\/guidelines\/?/, '')
      if (!target) continue
      const linked = bySlug.get(target)
      if (!linked) {
        problems.push(`${doc.slug}.md: link ${href} points to no document`)
      } else if (anchor && !linked.anchors.includes(anchor)) {
        problems.push(`${doc.slug}.md: link ${href} has no such heading`)
      }
    }
  }
  if (problems.length) {
    throw new Error(
      `guidelines have broken references:\n${problems.join('\n')}`
    )
  }

  return entries
    .map((entry) => entry.doc)
    .sort(
      (a, b) =>
        CATEGORIES.indexOf(a.category) - CATEGORIES.indexOf(b.category) ||
        a.order - b.order ||
        a.slug.localeCompare(b.slug)
    )
}

export const renderGuidelinesModule = () =>
  [
    '// Generated by scripts/build-guidelines.mjs — run `pnpm --filter editor guidelines`. DO NOT EDIT.',
    "import type { GuidelineDoc } from '#shared/types/guidelines'",
    '',
    `export const GUIDELINE_DOCS: GuidelineDoc[] = ${JSON.stringify(buildGuidelines(), null, 2)}`,
    ''
  ].join('\n')

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const next = renderGuidelinesModule()
  if (process.argv.includes('--check')) {
    const current = readFileSync(OUTPUT, 'utf8')
    if (current !== next) {
      console.error(
        'app/generated/guidelines.ts is stale — run pnpm guidelines'
      )
      process.exit(1)
    }
  } else {
    writeFileSync(OUTPUT, next)
    console.log(`Wrote ${OUTPUT}`)
  }
}
