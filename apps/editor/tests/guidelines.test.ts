import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import type { GuidelineDoc } from '../shared/types/guidelines'
import { GUIDELINE_DOCS } from '../app/generated/guidelines'
import { GUIDELINE_PITFALLS } from '../app/constants/guidelines'
import { FAMILY_GUIDES, WORK_SECTIONS } from '../app/constants/workspace'
import { guidelineHref, searchGuidelines } from '../app/utils/guidelineSearch'
// @ts-expect-error -- the build script is plain ESM without types
import { renderGuidelinesModule } from '../scripts/build-guidelines.mjs'

const GENERATED = fileURLToPath(
  new URL('../app/generated/guidelines.ts', import.meta.url)
)

const resolves = (href: string) => {
  const [path, anchor] = href.split('#')
  const slug = (path ?? '').replace(/^\/guidelines\/?/, '')
  const doc = GUIDELINE_DOCS.find((entry) => entry.slug === slug)
  if (!doc) {
    return false
  }
  return !anchor || doc.html.includes(`id="${anchor}"`)
}

describe('guidelines content', () => {
  it('ships a generated module that matches the markdown sources', () => {
    expect(readFileSync(GENERATED, 'utf8')).toBe(renderGuidelinesModule())
  })

  it('renders every emphasis mark, including next to CJK punctuation', () => {
    for (const doc of GUIDELINE_DOCS) {
      expect(doc.html, doc.slug).not.toMatch(/\*\*|__/)
    }
  })

  it('links every overview pitfall to an existing document and heading', () => {
    for (const pitfall of GUIDELINE_PITFALLS) {
      expect(resolves(pitfall.to), pitfall.to).toBe(true)
    }
  })

  it('points every workspace section and entity family at a document', () => {
    const slugs = [
      ...WORK_SECTIONS.map((section) => section.guide),
      ...Object.values(FAMILY_GUIDES)
    ]
    for (const slug of slugs) {
      expect(slug && resolves(`/guidelines/${slug}`), slug).toBe(true)
    }
  })
})

describe('searchGuidelines', () => {
  const doc = (
    slug: string,
    overrides: Partial<GuidelineDoc>
  ): GuidelineDoc => ({
    slug,
    title: slug,
    description: '',
    category: 'rules',
    order: 0,
    icon: 'lucide:book-open',
    related: [],
    headings: [],
    text: '',
    html: '',
    ...overrides
  })
  const docs = [
    doc('body', {
      title: '正文',
      text: '这里提到署名名与人物的区别，以及更多内容。'
    }),
    doc('heading', {
      title: '小节',
      headings: [{ id: 'credit', text: '署名名是什么', depth: 2 }]
    }),
    doc('title', { title: '署名名与人物', description: '概念说明' })
  ]

  it('ranks title matches above headings above body text', () => {
    expect(searchGuidelines(docs, '署名名').map((hit) => hit.doc.slug)).toEqual(
      ['title', 'heading', 'body']
    )
  })

  it('deep-links heading hits and quotes body hits', () => {
    const [, heading, body] = searchGuidelines(docs, '署名名')
    expect(guidelineHref(heading!)).toBe('/guidelines/heading#credit')
    expect(body!.snippet).toContain('署名名')
  })

  it('returns nothing for a blank query', () => {
    expect(searchGuidelines(docs, '   ')).toEqual([])
  })
})
