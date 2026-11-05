import type { CatalogWalkthroughImportReview } from '#shared/types/catalog'
import type { WtChart } from '#shared/types/walkthrough'

// extract.coverage keeps at most this many missed lines, so a full list may hide more.
const MISSED_CAP = 8

const LEADING_MARKERS = /^[○●◆◇■□・]+/
const PARENTHESES = /\([^()]*\)/g
const OR_WORD = /\bor\b|選択肢/gi
const WORD_CHAR = /[\p{L}\p{N}]/u

const squash = (text: string) =>
  text.normalize('NFKC').replace(/\s+/g, '').replace(LEADING_MARKERS, '')

const wordChars = (text: string) =>
  [...text].filter((ch) => WORD_CHAR.test(ch)).length

export interface ImportReviewItem {
  text: string
  copy: string
  added: boolean
}

export interface ImportReviewProgress {
  items: ImportReviewItem[]
  remaining: number
  truncated: boolean
  endingsReached: number
  endMarkers: number
  complete: boolean
}

const knownTexts = (chart: WtChart) => {
  const known: string[] = []
  const add = (text: string | null | undefined) => {
    const squashed = text ? squash(text) : ''
    if (squashed) known.push(squashed)
  }
  for (const segment of chart.segments ?? []) {
    for (const step of segment.steps ?? []) {
      add(step.text)
      if (step.text && step.remark) add(step.text + step.remark)
      for (const alt of step.alt ?? []) add(alt)
    }
    add(segment.condition)
  }
  for (const section of chart.sections ?? []) add(section.title)
  for (const character of chart.characters ?? []) add(character.name)
  for (const ending of chart.endings ?? []) add(ending.name)
  for (const entry of chart.entries ?? []) add(entry.label)
  for (const legend of chart.legend ?? []) add(legend.text)
  return known.sort((a, b) => b.length - a.length)
}

const covered = (line: string, known: string[]) => {
  const full = squash(line)
  const core = full.replace(PARENTHESES, '')
  const usableCore = wordChars(core) >= 2
  if (known.some((k) => k.includes(full) || (usableCore && k.includes(core)))) {
    return true
  }
  let rest = full
  for (const k of known) rest = rest.replaceAll(k, '')
  return wordChars(rest.replace(OR_WORD, '')) < 2
}

export const importReviewProgress = (
  review: CatalogWalkthroughImportReview,
  chart: WtChart
): ImportReviewProgress => {
  const known = knownTexts(chart)
  const items = review.missed.map((text) => ({
    text,
    copy: text.replace(LEADING_MARKERS, '').trim(),
    added: covered(text, known)
  }))
  const remaining = items.filter((item) => !item.added).length
  const truncated = review.missed.length >= MISSED_CAP
  const endingsReached = (chart.segments ?? []).filter(
    (segment) => segment.to?.node_type === 'ending'
  ).length
  return {
    items,
    remaining,
    truncated,
    endingsReached,
    endMarkers: review.end_markers,
    complete:
      remaining === 0 && !truncated && endingsReached >= review.end_markers
  }
}

export const importReviewGaps = (progress: ImportReviewProgress) => {
  const gaps: string[] = []
  if (progress.remaining) {
    gaps.push(`还有 ${progress.remaining} 个选项没有补上`)
  }
  if (progress.endingsReached < progress.endMarkers) {
    gaps.push(
      `结局只连上了 ${progress.endingsReached} / ${progress.endMarkers} 个`
    )
  }
  if (progress.truncated && !progress.remaining) {
    gaps.push(`导入时只列出了前 ${MISSED_CAP} 处遗漏，原攻略可能还有更多`)
  }
  return gaps
}
