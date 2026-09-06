// Self-built Markdown layer for the portal's hand-written guides — no
// @nuxt/content, no VitePress. Same shape as the kungal docs portal: markdown
// is compiled by a standalone Node script BEFORE nuxt builds, so markdown-it
// and Shiki never enter the Vite graph or the client bundle. Pages ship
// pre-highlighted HTML.
//
// Called from sync-specs.mjs, which stays the single entry point: one command
// rebuilds the reference model, the guides, and every LLM-facing artifact, so
// a guide edit cannot refresh the HTML while leaving the Markdown twin stale.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import matter from 'gray-matter'
import MarkdownIt from 'markdown-it'
import { createHighlighter } from 'shiki'
import { fromHighlighter } from '@shikijs/markdown-it/core'

import {
  GUIDES_DIR,
  GUIDE_SECTIONS,
  GUIDE_SLUGS,
  guideRoute
} from './guides.mjs'

// Keeps CJK: the anchor of a Chinese heading should be that heading, not an
// empty string. Headings that need a stable cross-page anchor declare it as
// `## 标题 {#explicit-id}` instead.
const slugify = (s) =>
  String(s)
    .trim()
    .toLowerCase()
    .replace(/[^\w一-龥]+/g, '-')
    .replace(/^-+|-+$/g, '')

const EXPLICIT_ID = /\s*\{#([A-Za-z0-9_-]+)\}\s*$/

const ALERTS = {
  NOTE: 'info',
  TIP: 'success',
  IMPORTANT: 'primary',
  WARNING: 'warning',
  CAUTION: 'danger'
}
const ALERT_MARK = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*/

const highlighter = await createHighlighter({
  themes: ['github-light', 'github-dark'],
  langs: [
    'bash',
    'http',
    'json',
    'javascript',
    'typescript',
    'sql',
    'yaml',
    'go',
    'rust',
    'text'
  ]
})

const md = MarkdownIt({ html: false, linkify: false })
md.use(
  fromHighlighter(highlighter, {
    themes: { light: 'github-light', dark: 'github-dark' },
    fallbackLanguage: 'text'
  })
)

// Heading ids, with `{#id}` winning over the slug. Written by hand rather than
// pulled from markdown-it-anchor because the explicit form is the whole point:
// /docs/design#client-contract must keep working when its Chinese heading is
// reworded.
md.core.ruler.push('heading_ids', (state) => {
  const used = new Set()
  for (let i = 0; i < state.tokens.length; i++) {
    const open = state.tokens[i]
    if (open.type !== 'heading_open') continue
    const inline = state.tokens[i + 1]
    if (!inline || inline.type !== 'inline') continue
    const explicit = inline.content.match(EXPLICIT_ID)
    let id
    if (explicit) {
      id = explicit[1]
      inline.content = inline.content.replace(EXPLICIT_ID, '')
      const last = inline.children?.[inline.children.length - 1]
      if (last?.type === 'text')
        last.content = last.content.replace(EXPLICIT_ID, '')
    } else {
      id = slugify(inline.content)
    }
    let unique = id
    for (let n = 2; used.has(unique); n++) unique = `${id}-${n}`
    used.add(unique)
    open.attrSet('id', unique)
  }
})

// GitHub-style alerts (`> [!WARNING]`) become tone-coloured callouts. The
// marker is stripped from the rendered text but stays in the Markdown twin,
// where it is the standard syntax an agent already understands.
md.core.ruler.push('alerts', (state) => {
  const tokens = state.tokens
  for (let i = 0; i < tokens.length; i++) {
    if (tokens[i].type !== 'blockquote_open') continue
    const inline = tokens[i + 2]
    if (inline?.type !== 'inline') continue
    const mark = inline.content.match(ALERT_MARK)
    if (!mark) continue
    tokens[i].attrJoin('class', `kun-alert kun-alert-${ALERTS[mark[1]]}`)
    inline.content = inline.content.replace(ALERT_MARK, '')
    const kids = inline.children ?? []
    if (kids[0]?.type === 'text')
      kids[0].content = kids[0].content.replace(ALERT_MARK, '')
    while (
      kids.length &&
      (kids[0].type === 'softbreak' ||
        (kids[0].type === 'text' && !kids[0].content))
    ) {
      kids.shift()
    }
  }
})

// Wide tables scroll inside the prose column instead of overflowing the page.
md.renderer.rules.table_open = () => '<div class="kun-table-wrap"><table>'
md.renderer.rules.table_close = () => '</table></div>'

const extractToc = (tokens) => {
  const toc = []
  for (let i = 0; i < tokens.length; i++) {
    const t = tokens[i]
    if (t.type !== 'heading_open') continue
    if (t.tag !== 'h2' && t.tag !== 'h3') continue
    const id = t.attrGet('id')
    const text = tokens[i + 1]?.content ?? ''
    if (id && text) toc.push({ id, text, depth: t.tag === 'h2' ? 2 : 3 })
  }
  return toc
}

const requireField = (value, field, slug) => {
  if (typeof value === 'string' && value.trim()) return value.trim()
  throw new Error(`docs/${slug}.md is missing frontmatter \`${field}\``)
}

export const buildGuides = () => {
  const pages = GUIDE_SLUGS.map((slug) => {
    const raw = readFileSync(join(GUIDES_DIR, `${slug}.md`), 'utf8')
    const { content, data } = matter(raw)
    const tokens = md.parse(content, {})
    const toc = extractToc(tokens)
    return {
      slug,
      route: guideRoute(slug),
      title: requireField(data.title, 'title', slug),
      eyebrow: requireField(data.eyebrow, 'eyebrow', slug),
      description: requireField(data.description, 'description', slug),
      toc,
      html: md.renderer.render(tokens, md.options, {}),
      // The twin is the source with the anchor markers taken out: they are
      // authoring syntax, not prose, and the twin is what agents read.
      markdown: content.replace(/[ \t]*\{#[A-Za-z0-9_-]+\}[ \t]*$/gm, '').trim()
    }
  })

  const bySlug = new Map(pages.map((p) => [p.slug, p]))
  const nav = GUIDE_SECTIONS.map((section) => ({
    key: section.key,
    label: section.label,
    links: section.slugs.map((slug) => ({
      to: bySlug.get(slug).route,
      label: bySlug.get(slug).title
    }))
  }))

  return { pages, nav }
}
