import { describe, expect, it } from 'vitest'
import { normalizeChart, validateChart } from '../app/utils/walkthrough/doc'
import { endingLabel, normalizeEnding } from '../app/utils/walkthrough/endings'

describe('walkthrough endings', () => {
  it('splits a stored single-axis type into route_type and outcome', () => {
    const legacy = (ending_type: string, characters: string[]) =>
      normalizeEnding({ key: 'n1', name: 'END', ending_type, characters })
    expect(legacy('heroine', ['c1'])).toMatchObject({
      route_type: 'heroine',
      outcome: 'happy',
      after: false,
      other: false
    })
    expect(legacy('heroine', ['c1', 'c2'])).toMatchObject({
      route_type: 'harem',
      outcome: 'happy'
    })
    expect(legacy('bad', [])).toMatchObject({
      route_type: 'common',
      outcome: 'bad'
    })
    expect(legacy('after', ['c1'])).toMatchObject({
      route_type: 'heroine',
      outcome: null,
      after: true
    })
    expect(legacy('other', [])).toMatchObject({ route_type: null, other: true })
    expect(legacy('other', [])).not.toHaveProperty('ending_type')
  })

  it('keeps an ending that already has its axes', () => {
    expect(
      normalizeEnding({
        key: 'n1',
        name: 'END',
        route_type: 'common',
        outcome: 'happy',
        ending_type: 'bad',
        characters: []
      })
    ).toMatchObject({ route_type: 'common', outcome: 'happy' })
  })

  it('labels each combination', () => {
    const e = normalizeEnding({ key: 'n1', name: 'END', characters: [] })
    expect(endingLabel(e)).toBe('未分类')
    expect(endingLabel({ ...e, route_type: 'heroine', outcome: 'bad' })).toBe(
      '个人线 · BE'
    )
    expect(endingLabel({ ...e, route_type: 'harem', after: true })).toBe(
      '后宫 · 后日谈'
    )
    expect(endingLabel({ ...e, other: true, other_label: '隐藏结局' })).toBe(
      '其他 · 隐藏结局'
    )
  })

  it('flags a heroine route_type of two characters and an ending with no type', () => {
    const chart = normalizeChart({
      characters: [
        { key: 'c1', name: '寧々' },
        { key: 'c2', name: 'めぐる' }
      ],
      entries: [{ key: 'e1', label: 'START' }],
      endings: [
        {
          key: 'n1',
          name: '二人END',
          route_type: 'heroine',
          outcome: 'happy',
          characters: ['c1', 'c2']
        },
        { key: 'n2', name: 'END', characters: [] }
      ],
      segments: [
        {
          key: 'g1',
          from: { node_type: 'entry', key: 'e1' },
          steps: [{ step_type: 'choice', text: '帰る' }],
          to: { node_type: 'ending', key: 'n1' }
        }
      ]
    })
    expect(validateChart(chart).map((i) => i.message)).toEqual([
      '结局「二人END」是个人线，只能归属一个角色',
      '结局「END」还没有选择类型'
    ])
  })
})
