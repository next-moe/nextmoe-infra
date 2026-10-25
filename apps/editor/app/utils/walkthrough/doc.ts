import type {
  WtChart,
  WtIssue,
  WtNode,
  WtSegment,
  WtStep,
  WtStepType
} from '#shared/types/walkthrough'
import {
  WT_ENDING_TYPES,
  WT_KEY_PATTERN,
  WT_LIMITS
} from '~/constants/walkthrough'

const list = <T>(value: T[] | null | undefined): T[] =>
  Array.isArray(value) ? value : []

export const normalizeChart = (raw: unknown): WtChart => {
  const c = (raw && typeof raw === 'object' ? raw : {}) as Partial<WtChart>
  return {
    characters: list(c.characters).map((ch) => ({
      ...ch,
      aliases: list(ch.aliases)
    })),
    sections: list(c.sections).map((s) => ({
      ...s,
      title_zh: s.title_zh ?? null,
      characters: list(s.characters)
    })),
    entries: list(c.entries).map((e) => ({
      ...e,
      unlocked_by: e.unlocked_by ?? null
    })),
    saves: list(c.saves),
    endings: list(c.endings).map((e) => ({
      ...e,
      name_zh: e.name_zh ?? null,
      characters: list(e.characters)
    })),
    segments: list(c.segments).map((s) => ({
      ...s,
      section: s.section ?? null,
      condition: s.condition ?? null,
      condition_zh: s.condition_zh ?? null,
      steps: list(s.steps).map((st) => ({
        ...st,
        text: st.text ?? null,
        text_zh: st.text_zh ?? null,
        alt: list(st.alt),
        alt_zh: list(st.alt_zh),
        achievement: Boolean(st.achievement),
        save: st.save ?? null
      }))
    }))
  }
}

export const nextKey = (prefix: string, taken: Iterable<string>) => {
  const used = new Set(taken)
  let n = used.size + 1
  for (let i = 1; i <= used.size + 1; i++) {
    if (!used.has(`${prefix}${i}`)) {
      n = i
      break
    }
  }
  return `${prefix}${n}`
}

export const keysOf = (items: { key: string }[]) => items.map((i) => i.key)

export const blankStep = (
  stepType: WtStepType,
  save: string | null = null
): WtStep => ({
  step_type: stepType,
  text: stepType === 'save' ? null : '',
  text_zh: null,
  alt: [],
  alt_zh: [],
  achievement: false,
  save: stepType === 'save' ? save : null
})

export const emptyChart = (): WtChart => ({
  characters: [],
  sections: [],
  entries: [{ key: 'e1', label: 'START', unlocked_by: null }],
  saves: [],
  endings: [],
  segments: [
    {
      key: 'g1',
      section: null,
      from: { node_type: 'entry', key: 'e1' },
      condition: null,
      condition_zh: null,
      steps: [blankStep('choice')],
      to: { node_type: 'open', key: null }
    }
  ]
})

export const sameNode = (a: WtNode, b: WtNode) =>
  a.node_type === b.node_type && (a.key ?? null) === (b.key ?? null)

export const segmentCharacterKey = (chart: WtChart, segment: WtSegment) => {
  if (segment.to.node_type === 'ending') {
    const ending = chart.endings.find((e) => e.key === segment.to.key)
    if (ending?.characters.length === 1) {
      return ending.characters[0]!
    }
  }
  const section = chart.sections.find((s) => s.key === segment.section)
  return section?.characters.length === 1 ? section.characters[0]! : null
}

export const characterTones = (chart: WtChart) =>
  new Map(chart.characters.map((c, i) => [c.key, i]))

export const nodeLabel = (chart: WtChart, node: WtNode): string => {
  switch (node.node_type) {
    case 'entry':
      return chart.entries.find((e) => e.key === node.key)?.label || '入口'
    case 'save':
      return chart.saves.find((s) => s.key === node.key)?.label || '存档'
    case 'ending':
      return chart.endings.find((e) => e.key === node.key)?.name || '结局'
    case 'collect':
      return '回收后结束'
    default:
      return '未完待续'
  }
}

export interface WtReferences {
  segmentsFrom: number
  segmentsTo: number
  steps: number
  entries: number
  sections: number
  endings: number
}

export const referencesOf = (
  chart: WtChart,
  kind: 'character' | 'section' | 'entry' | 'save' | 'ending',
  key: string
): WtReferences => {
  const refs: WtReferences = {
    segmentsFrom: 0,
    segmentsTo: 0,
    steps: 0,
    entries: 0,
    sections: 0,
    endings: 0
  }
  for (const s of chart.segments) {
    if (kind === 'section' && s.section === key) refs.segmentsFrom++
    if (s.from.node_type === kind && s.from.key === key) refs.segmentsFrom++
    if (s.to.node_type === kind && s.to.key === key) refs.segmentsTo++
    if (kind === 'save') {
      refs.steps += s.steps.filter((st) => st.save === key).length
    }
  }
  if (kind === 'ending') {
    refs.entries = chart.entries.filter((e) => e.unlocked_by === key).length
  }
  if (kind === 'character') {
    refs.sections = chart.sections.filter((s) =>
      s.characters.includes(key)
    ).length
    refs.endings = chart.endings.filter((e) =>
      e.characters.includes(key)
    ).length
  }
  return refs
}

export const removeCharacter = (d: WtChart, key: string) => {
  d.characters = d.characters.filter((c) => c.key !== key)
  for (const s of d.sections)
    s.characters = s.characters.filter((k) => k !== key)
  for (const e of d.endings)
    e.characters = e.characters.filter((k) => k !== key)
}

export const removeSection = (d: WtChart, key: string) => {
  d.sections = d.sections.filter((s) => s.key !== key)
  for (const s of d.segments) if (s.section === key) s.section = null
}

export const removeEntry = (d: WtChart, key: string) => {
  d.entries = d.entries.filter((e) => e.key !== key)
  d.segments = d.segments.filter(
    (s) => !(s.from.node_type === 'entry' && s.from.key === key)
  )
}

export const removeSave = (d: WtChart, key: string) => {
  d.saves = d.saves.filter((s) => s.key !== key)
  d.segments = d.segments.filter(
    (s) => !(s.from.node_type === 'save' && s.from.key === key)
  )
  for (const s of d.segments) s.steps = s.steps.filter((st) => st.save !== key)
}

export const removeEnding = (d: WtChart, key: string) => {
  d.endings = d.endings.filter((e) => e.key !== key)
  d.segments = d.segments.filter(
    (s) => !(s.from.node_type === 'ending' && s.from.key === key)
  )
  for (const s of d.segments) {
    if (s.to.node_type === 'ending' && s.to.key === key) {
      s.to = { node_type: 'open', key: null }
    }
  }
  for (const e of d.entries) if (e.unlocked_by === key) e.unlocked_by = null
}

const runes = (s: string) => [...s].length

export const validateChart = (chart: WtChart): WtIssue[] => {
  const issues: WtIssue[] = []
  const push = (target: WtIssue['target'], message: string) =>
    issues.push({ target, message })

  const groups = [
    ['character', '角色', chart.characters],
    ['section', '分节', chart.sections],
    ['entry', '入口', chart.entries],
    ['save', '存档', chart.saves],
    ['ending', '结局', chart.endings],
    ['segment', '路线段', chart.segments]
  ] as const
  for (const [kind, label, items] of groups) {
    const seen = new Set<string>()
    for (const item of items) {
      if (!WT_KEY_PATTERN.test(item.key)) {
        push(
          { kind, key: item.key },
          `${label}的编号「${item.key}」只能由字母、数字、下划线组成`
        )
      } else if (seen.has(item.key)) {
        push({ kind, key: item.key }, `${label}编号「${item.key}」重复了`)
      }
      seen.add(item.key)
    }
  }

  const has = (items: { key: string }[], key: string | null) =>
    key !== null && items.some((i) => i.key === key)
  const tooLong = (s: string | null, max: number) =>
    s !== null && runes(s) > max
  const blank = (s: string | null) => !s || !s.trim()

  for (const c of chart.characters) {
    const t = { kind: 'character' as const, key: c.key }
    if (blank(c.name)) push(t, '角色还没有名字')
    if (tooLong(c.name, WT_LIMITS.nameRunes))
      push(t, `角色名最多 ${WT_LIMITS.nameRunes} 字`)
    if (c.aliases.length > WT_LIMITS.aliases)
      push(t, `别名最多 ${WT_LIMITS.aliases} 个`)
    if (c.aliases.some(blank)) push(t, `角色「${c.name}」有空白的别名`)
  }
  for (const s of chart.sections) {
    const t = { kind: 'section' as const, key: s.key }
    if (blank(s.title)) push(t, '分节还没有标题')
    for (const k of s.characters) {
      if (!has(chart.characters, k))
        push(t, `分节「${s.title}」指向了不存在的角色`)
    }
  }
  for (const e of chart.entries) {
    const t = { kind: 'entry' as const, key: e.key }
    if (blank(e.label)) push(t, '入口还没有名称')
    if (e.unlocked_by !== null && !has(chart.endings, e.unlocked_by)) {
      push(t, `入口「${e.label}」的解锁结局不存在`)
    }
  }
  for (const s of chart.saves) {
    if (blank(s.label)) push({ kind: 'save', key: s.key }, '存档还没有名称')
  }
  const types = new Set(WT_ENDING_TYPES.map((e) => e.value))
  for (const e of chart.endings) {
    const t = { kind: 'ending' as const, key: e.key }
    if (blank(e.name)) push(t, '结局还没有名称')
    if (!types.has(e.ending_type)) push(t, `结局「${e.name}」的类型无效`)
    for (const k of e.characters) {
      if (!has(chart.characters, k))
        push(t, `结局「${e.name}」指向了不存在的角色`)
    }
  }

  if (!chart.segments.length) {
    push(null, '至少需要一条路线段')
  }
  let steps = 0
  for (const s of chart.segments) {
    const t = (step?: number) => ({
      kind: 'segment' as const,
      key: s.key,
      step
    })
    if (s.section !== null && !has(chart.sections, s.section))
      push(t(), '路线段所属的分节不存在')
    const from = s.from
    if (from.node_type === 'entry' && !has(chart.entries, from.key))
      push(t(), '起点入口不存在')
    else if (from.node_type === 'save' && !has(chart.saves, from.key))
      push(t(), '起点存档不存在')
    else if (from.node_type === 'ending' && !has(chart.endings, from.key))
      push(t(), '起点结局不存在')
    else if (!['entry', 'save', 'ending'].includes(from.node_type))
      push(t(), '起点只能是入口、存档或结局')
    const to = s.to
    if (to.node_type === 'ending' && !has(chart.endings, to.key))
      push(t(), '终点结局不存在')
    else if (!['ending', 'collect', 'open'].includes(to.node_type))
      push(t(), '终点只能是结局、回收或未完待续')
    if (tooLong(s.condition, WT_LIMITS.conditionRunes))
      push(t(), '前提条件太长了')
    steps += s.steps.length
    s.steps.forEach((st, i) => {
      if (st.step_type === 'save') {
        if (!has(chart.saves, st.save)) push(t(i), '存档步骤没有选择存档')
        return
      }
      if (blank(st.text))
        push(
          t(i),
          st.step_type === 'choice' ? '选项还没有填写' : '操作还没有填写'
        )
      if (tooLong(st.text, WT_LIMITS.textRunes))
        push(t(i), `步骤文本最多 ${WT_LIMITS.textRunes} 字`)
      if (st.alt.length > WT_LIMITS.alts)
        push(t(i), `其他写法最多 ${WT_LIMITS.alts} 个`)
      if (st.alt.some(blank)) push(t(i), '有空白的其他写法')
      if (st.alt_zh.length !== st.alt.length)
        push(t(i), '其他写法与译文数量不一致')
    })
  }
  if (steps > WT_LIMITS.steps) push(null, `步骤总数最多 ${WT_LIMITS.steps} 个`)
  if (chart.characters.length > WT_LIMITS.characters)
    push(null, `角色最多 ${WT_LIMITS.characters} 个`)
  if (chart.segments.length > WT_LIMITS.segments)
    push(null, `路线段最多 ${WT_LIMITS.segments} 条`)
  return issues
}

export const stripTranslations = (d: WtChart) => {
  for (const s of d.sections) s.title_zh = null
  for (const e of d.endings) e.name_zh = null
  for (const s of d.segments) {
    s.condition_zh = null
    for (const st of s.steps) {
      st.text_zh = null
      st.alt_zh = st.alt.map(() => null)
    }
  }
}
