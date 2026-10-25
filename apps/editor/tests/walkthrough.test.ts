import { describe, expect, it } from 'vitest'
import type { WtChart } from '../shared/types/walkthrough'
import {
  blankStep,
  emptyChart,
  nextKey,
  normalizeChart,
  referencesOf,
  removeEnding,
  removeSave,
  stripTranslations,
  validateChart
} from '../app/utils/walkthrough/doc'
import { layoutChart } from '../app/utils/walkthrough/graph'
import { diffCharts } from '../app/utils/walkthrough/diff'

const chart = (): WtChart =>
  normalizeChart({
    characters: [{ key: 'c1', name: '寧々' }],
    entries: [{ key: 'e1', label: 'START' }],
    saves: [{ key: 'v1', label: 'セーブ1' }],
    endings: [
      { key: 'n1', name: '寧々END', ending_type: 'heroine', characters: ['c1'] }
    ],
    segments: [
      {
        key: 'g1',
        from: { node_type: 'entry', key: 'e1' },
        steps: [
          { step_type: 'choice', text: '帰る' },
          { step_type: 'save', save: 'v1' }
        ],
        to: { node_type: 'open', key: null }
      },
      {
        key: 'g2',
        from: { node_type: 'save', key: 'v1' },
        steps: [{ step_type: 'choice', text: '手伝う', text_zh: '帮忙' }],
        to: { node_type: 'ending', key: 'n1' }
      }
    ]
  })

describe('walkthrough document', () => {
  it('fills every array a stored chart may omit', () => {
    const c = chart()
    expect(c.sections).toEqual([])
    expect(c.segments[0]!.steps[1]).toMatchObject({
      alt: [],
      alt_zh: [],
      text: null,
      achievement: false
    })
  })

  it('validates a sound chart and the starter chart once its step is filled', () => {
    expect(validateChart(chart())).toEqual([])
    const starter = emptyChart()
    expect(validateChart(starter).map((i) => i.message)).toEqual([
      '选项还没有填写'
    ])
    starter.segments[0]!.steps[0]!.text = '始める'
    expect(validateChart(starter)).toEqual([])
  })

  it('points every issue at the thing to fix', () => {
    const c = chart()
    c.segments[1]!.to = { node_type: 'ending', key: 'n9' }
    c.segments[0]!.steps[0]!.text = ' '
    c.characters.push({ key: 'c1', name: '重複', aliases: [] })
    const issues = validateChart(c)
    expect(issues).toContainEqual({
      target: { kind: 'segment', key: 'g2', step: undefined },
      message: '终点结局不存在'
    })
    expect(issues).toContainEqual({
      target: { kind: 'segment', key: 'g1', step: 0 },
      message: '选项还没有填写'
    })
    expect(issues.some((i) => i.message.includes('重复'))).toBe(true)
  })

  it('cascades a removed save and ending into the segments that use them', () => {
    const c = chart()
    expect(referencesOf(c, 'save', 'v1')).toMatchObject({
      segmentsFrom: 1,
      steps: 1
    })
    removeSave(c, 'v1')
    expect(c.segments.map((s) => s.key)).toEqual(['g1'])
    expect(c.segments[0]!.steps).toHaveLength(1)

    const d = chart()
    removeEnding(d, 'n1')
    expect(d.segments[1]!.to).toEqual({ node_type: 'open', key: null })
    expect(validateChart(d)).toEqual([])
  })

  it('hands out the first free key', () => {
    expect(nextKey('g', ['g1', 'g3'])).toBe('g2')
    expect(nextKey('g', [])).toBe('g1')
    expect(blankStep('save', 'v1')).toMatchObject({ text: null, save: 'v1' })
  })

  it('strips every translation when the chart becomes Chinese', () => {
    const c = chart()
    stripTranslations(c)
    expect(c.segments[1]!.steps[0]!.text_zh).toBeNull()
  })
})

describe('walkthrough graph', () => {
  it('cuts a segment at its save step and keeps step ranges for selection', () => {
    const layout = layoutChart(chart())
    const g1 = layout.edges.filter((e) => e.segmentKey === 'g1')
    expect(g1.map((e) => [e.from, e.to, e.stepStart, e.stepEnd])).toEqual([
      ['entry:e1', 'save:v1', 0, 1],
      ['save:v1', 'open:g1', 2, 2]
    ])
    expect(layout.nodes.map((n) => n.id).sort()).toEqual([
      'ending:n1',
      'entry:e1',
      'open:g1',
      'save:v1'
    ])
    expect(layout.width).toBeGreaterThan(0)
  })
})

describe('walkthrough diff', () => {
  it('reports an inserted step without marking the rest changed', () => {
    const before = chart()
    const after = chart()
    after.segments[0]!.steps.splice(0, 0, {
      ...blankStep('choice'),
      text: '話す'
    })
    const [change] = diffCharts(before, after)
    expect(change).toMatchObject({
      group: '路线段',
      key: 'g1',
      status: 'changed'
    })
    expect(change!.details).toEqual(['新增第 1 步：选项「話す」'])
  })

  it('names a translation-only edit as such', () => {
    const after = chart()
    after.segments[1]!.steps[0]!.text_zh = '帮把手'
    expect(diffCharts(chart(), after)[0]!.details).toEqual([
      '第 1 步的译文：「帮忙」 → 「帮把手」'
    ])
  })

  it('reports nothing for equal charts', () => {
    expect(diffCharts(chart(), JSON.parse(JSON.stringify(chart())))).toEqual([])
  })
})
