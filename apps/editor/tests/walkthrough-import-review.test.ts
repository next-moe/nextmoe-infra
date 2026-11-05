import { describe, expect, it } from 'vitest'
import type { WtChart } from '../shared/types/walkthrough'
import { normalizeChart } from '../app/utils/walkthrough/doc'
import {
  importReviewGaps,
  importReviewProgress
} from '../app/utils/walkthrough/importReview'

const chartWith = (texts: string[], endings = 1): WtChart =>
  normalizeChart({
    endings: Array.from({ length: endings }, (_, i) => ({
      key: `e${i + 1}`,
      name: `END${i + 1}`
    })),
    entries: [{ key: 'start', label: 'START' }],
    segments: Array.from({ length: endings }, (_, i) => ({
      key: `s${i + 1}`,
      from: { node_type: 'entry', key: 'start' },
      to: { node_type: 'ending', key: `e${i + 1}` },
      steps: i === 0 ? texts.map((text) => ({ text })) : []
    }))
  })

const review = (missed: string[], endMarkers = 1) => ({
  missed,
  coverage: 0.8,
  end_markers: endMarkers,
  endings_reached: 0
})

describe('importReviewProgress', () => {
  it('counts a choice the editor wrote with an action word or a different note as added', () => {
    const progress = importReviewProgress(
      review([
        '○楽園幻想',
        '○龍の如く',
        '○頂点への道(リサH)',
        '○手持ち無沙汰(クローディアH)'
      ]),
      chartWith([
        '○楽園幻想をクリック(ティアH)',
        '龍の如く（ヴィノレッタの日常）',
        '頂点への道をクリック(リサH)',
        '手持ち無沙汰(クローディアH)'
      ])
    )
    expect(progress.items.map((item) => item.added)).toEqual([
      true,
      true,
      true,
      true
    ])
    expect(progress.remaining).toBe(0)
    expect(progress.complete).toBe(true)
  })

  it('keeps a choice that is not in the chart', () => {
    const progress = importReviewProgress(
      review(['○聖女と娼館']),
      chartWith(['召使いのいない夜'])
    )
    expect(progress.remaining).toBe(1)
    expect(progress.items[0]?.copy).toBe('聖女と娼館')
    expect(progress.complete).toBe(false)
  })

  it('is not complete while endings are missing', () => {
    const progress = importReviewProgress(review([], 3), chartWith([], 2))
    expect(progress.endingsReached).toBe(2)
    expect(progress.complete).toBe(false)
    expect(importReviewGaps(progress)).toEqual(['结局只连上了 2 / 3 个'])
  })

  it('is not complete when the import list may have been cut short', () => {
    const missed = Array.from({ length: 8 }, (_, i) => `選択肢${i}番目`)
    const progress = importReviewProgress(review(missed), chartWith(missed))
    expect(progress.remaining).toBe(0)
    expect(progress.truncated).toBe(true)
    expect(progress.complete).toBe(false)
  })
})
