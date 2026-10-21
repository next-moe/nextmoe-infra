import type { GuidelineDoc } from '#shared/types/guidelines'

export interface GuidelineHit {
  doc: GuidelineDoc
  anchor: string | null
  label: string
  snippet: string
}

const SNIPPET_RADIUS = 36

const snippetAround = (text: string, index: number, length: number) => {
  const start = Math.max(0, index - SNIPPET_RADIUS)
  const end = Math.min(text.length, index + length + SNIPPET_RADIUS)
  return `${start > 0 ? '…' : ''}${text.slice(start, end)}${end < text.length ? '…' : ''}`
}

export const searchGuidelines = (
  docs: GuidelineDoc[],
  query: string,
  limit = 20
): GuidelineHit[] => {
  const q = query.trim().toLowerCase()
  if (!q) {
    return []
  }
  const ranked: { hit: GuidelineHit; rank: number }[] = []
  for (const doc of docs) {
    if (doc.title.toLowerCase().includes(q)) {
      ranked.push({
        rank: 0,
        hit: { doc, anchor: null, label: doc.title, snippet: doc.description }
      })
      continue
    }
    const heading = doc.headings.find((h) => h.text.toLowerCase().includes(q))
    if (heading) {
      ranked.push({
        rank: 1,
        hit: {
          doc,
          anchor: heading.id,
          label: `${doc.title} · ${heading.text}`,
          snippet: doc.description
        }
      })
      continue
    }
    if (doc.description.toLowerCase().includes(q)) {
      ranked.push({
        rank: 2,
        hit: { doc, anchor: null, label: doc.title, snippet: doc.description }
      })
      continue
    }
    const at = doc.text.toLowerCase().indexOf(q)
    if (at >= 0) {
      ranked.push({
        rank: 3,
        hit: {
          doc,
          anchor: null,
          label: doc.title,
          snippet: snippetAround(doc.text, at, q.length)
        }
      })
    }
  }
  return ranked
    .sort((a, b) => a.rank - b.rank)
    .slice(0, limit)
    .map((entry) => entry.hit)
}

export const guidelineHref = (hit: Pick<GuidelineHit, 'doc' | 'anchor'>) =>
  `/guidelines/${hit.doc.slug}${hit.anchor ? `#${hit.anchor}` : ''}`
