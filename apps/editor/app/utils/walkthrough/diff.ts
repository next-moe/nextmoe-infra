import type {
  WtChart,
  WtNode,
  WtSegment,
  WtStep
} from '#shared/types/walkthrough'
import { WT_ENDING_LABEL, WT_STEP_META } from '~/constants/walkthrough'
import { nodeLabel, normalizeChart } from './doc'

export type WtChangeStatus = 'added' | 'removed' | 'changed'

export interface WtChange {
  group: string
  key: string
  status: WtChangeStatus
  label: string
  details: string[]
}

const quote = (s: string | null | undefined) => `「${s ?? ''}」`

const stepText = (st: WtStep, chart: WtChart) =>
  st.step_type === 'save'
    ? `存档${quote(chart.saves.find((s) => s.key === st.save)?.label ?? st.save)}`
    : `${WT_STEP_META[st.step_type].label}${quote(st.text)}${st.remark ? `（${st.remark}）` : ''}${st.achievement ? ' · 成就' : ''}`

const stepSignature = (st: WtStep) => JSON.stringify(st)

const lcs = (a: string[], b: string[]) => {
  const dp = Array.from({ length: a.length + 1 }, () =>
    new Array<number>(b.length + 1).fill(0)
  )
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      dp[i]![j] =
        a[i] === b[j]
          ? dp[i + 1]![j + 1]! + 1
          : Math.max(dp[i + 1]![j]!, dp[i]![j + 1]!)
    }
  }
  const ops: { op: 'keep' | 'del' | 'add'; i: number; j: number }[] = []
  let i = 0
  let j = 0
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      ops.push({ op: 'keep', i: i++, j: j++ })
    } else if (dp[i + 1]![j]! >= dp[i]![j + 1]!) {
      ops.push({ op: 'del', i: i++, j })
    } else {
      ops.push({ op: 'add', i, j: j++ })
    }
  }
  while (i < a.length) ops.push({ op: 'del', i: i++, j })
  while (j < b.length) ops.push({ op: 'add', i, j: j++ })
  return ops
}

const stepDetails = (
  before: WtSegment,
  after: WtSegment,
  a: WtChart,
  b: WtChart
) => {
  const ops = lcs(
    before.steps.map(stepSignature),
    after.steps.map(stepSignature)
  )
  const out: string[] = []
  for (let k = 0; k < ops.length; k++) {
    const op = ops[k]!
    const next = ops[k + 1]
    if (op.op === 'del' && next?.op === 'add') {
      const from = before.steps[op.i]!
      const to = after.steps[next.j]!
      const zhChanged =
        (from.text_zh !== to.text_zh || from.remark_zh !== to.remark_zh) &&
        from.text === to.text &&
        from.remark === to.remark
      out.push(
        zhChanged
          ? `第 ${next.j + 1} 步的译文：${quote(from.text_zh)}${from.remark_zh ? `（${from.remark_zh}）` : ''} → ${quote(to.text_zh)}${to.remark_zh ? `（${to.remark_zh}）` : ''}`
          : `第 ${next.j + 1} 步：${stepText(from, a)} → ${stepText(to, b)}`
      )
      k++
    } else if (op.op === 'del') {
      out.push(`删除第 ${op.i + 1} 步：${stepText(before.steps[op.i]!, a)}`)
    } else if (op.op === 'add') {
      out.push(`新增第 ${op.j + 1} 步：${stepText(after.steps[op.j]!, b)}`)
    }
  }
  return out
}

const nodeText = (chart: WtChart, node: WtNode) => quote(nodeLabel(chart, node))

const diffKeyed = <T extends { key: string }>(
  group: string,
  before: T[],
  after: T[],
  label: (item: T) => string,
  details: (from: T, to: T) => string[]
): WtChange[] => {
  const out: WtChange[] = []
  const old = new Map(before.map((item) => [item.key, item]))
  const now = new Map(after.map((item) => [item.key, item]))
  for (const item of after) {
    const prev = old.get(item.key)
    if (!prev) {
      out.push({
        group,
        key: item.key,
        status: 'added',
        label: label(item),
        details: []
      })
    } else if (JSON.stringify(prev) !== JSON.stringify(item)) {
      const d = details(prev, item)
      out.push({
        group,
        key: item.key,
        status: 'changed',
        label: label(item),
        details: d.length ? d : ['调整了细节']
      })
    }
  }
  for (const item of before) {
    if (!now.has(item.key)) {
      out.push({
        group,
        key: item.key,
        status: 'removed',
        label: label(item),
        details: []
      })
    }
  }
  return out
}

const changed = (what: string, from: unknown, to: unknown) =>
  JSON.stringify(from) === JSON.stringify(to) ? [] : [what]

export const diffCharts = (
  beforeRaw: unknown,
  afterRaw: unknown
): WtChange[] => {
  const a = normalizeChart(beforeRaw)
  const b = normalizeChart(afterRaw)
  const nameOf = (chart: WtChart, key: string) =>
    chart.characters.find((c) => c.key === key)?.name || key
  const names = (chart: WtChart, keys: string[]) =>
    keys.map((k) => nameOf(chart, k)).join('、') || '无'
  const endingNames = (chart: WtChart, keys: string[] | undefined) =>
    (keys ?? [])
      .map((k) => chart.endings.find((e) => e.key === k)?.name ?? k)
      .join('、') || '无'

  const segmentOrder =
    JSON.stringify(
      a.segments
        .map((s) => s.key)
        .filter((k) => b.segments.some((x) => x.key === k))
    ) !==
    JSON.stringify(
      b.segments
        .map((s) => s.key)
        .filter((k) => a.segments.some((x) => x.key === k))
    )

  const legendBefore = a.legend.map((l) => l.text)
  const legendAfter = b.legend.map((l) => l.text)
  const legendDetails = [
    ...legendBefore
      .filter((t) => !legendAfter.includes(t))
      .map((t) => `删除${quote(t)}`),
    ...legendAfter
      .filter((t) => !legendBefore.includes(t))
      .map((t) => `新增${quote(t)}`)
  ]

  return [
    ...(JSON.stringify(a.legend) !== JSON.stringify(b.legend)
      ? [
          {
            group: '符号说明',
            key: '__legend__',
            status: 'changed' as const,
            label: '符号说明',
            details: legendDetails.length ? legendDetails : ['调整了顺序或译文']
          }
        ]
      : []),
    ...diffKeyed(
      '角色',
      a.characters,
      b.characters,
      (c) => c.name || c.key,
      (x, y) => [
        ...(x.name !== y.name
          ? [`名字：${quote(x.name)} → ${quote(y.name)}`]
          : []),
        ...changed('别名', x.aliases, y.aliases)
      ]
    ),
    ...diffKeyed(
      '结局',
      a.endings,
      b.endings,
      (e) => e.name || e.key,
      (x, y) => [
        ...(x.name !== y.name
          ? [`名称：${quote(x.name)} → ${quote(y.name)}`]
          : []),
        ...(x.name_zh !== y.name_zh
          ? [`中文名：${quote(x.name_zh)} → ${quote(y.name_zh)}`]
          : []),
        ...(x.ending_type !== y.ending_type
          ? [
              `类型：${WT_ENDING_LABEL[x.ending_type]} → ${WT_ENDING_LABEL[y.ending_type]}`
            ]
          : []),
        ...(JSON.stringify(x.characters) !== JSON.stringify(y.characters)
          ? [`归属：${names(a, x.characters)} → ${names(b, y.characters)}`]
          : [])
      ]
    ),
    ...diffKeyed(
      '存档',
      a.saves,
      b.saves,
      (s) => s.label || s.key,
      (x, y) => [
        ...(x.label !== y.label
          ? [`名称：${quote(x.label)} → ${quote(y.label)}`]
          : []),
        ...changed('中文名称', x.label_zh, y.label_zh)
      ]
    ),
    ...diffKeyed(
      '入口',
      a.entries,
      b.entries,
      (e) => e.label || e.key,
      (x, y) => [
        ...(x.label !== y.label
          ? [`名称：${quote(x.label)} → ${quote(y.label)}`]
          : []),
        ...changed('中文名称', x.label_zh, y.label_zh),
        ...changed('解锁条件', x.unlocked_by, y.unlocked_by)
      ]
    ),
    ...diffKeyed(
      '分节',
      a.sections,
      b.sections,
      (s) => s.title || s.key,
      (x, y) => [
        ...(x.title !== y.title
          ? [`标题：${quote(x.title)} → ${quote(y.title)}`]
          : []),
        ...changed('中文标题', x.title_zh, y.title_zh),
        ...(JSON.stringify(x.characters) !== JSON.stringify(y.characters)
          ? [`归属：${names(a, x.characters)} → ${names(b, y.characters)}`]
          : [])
      ]
    ),
    ...diffKeyed(
      '路线段',
      a.segments,
      b.segments,
      (s) =>
        `${nodeLabel(b.segments.includes(s) ? b : a, s.from)} → ${nodeLabel(b.segments.includes(s) ? b : a, s.to)}`,
      (x, y) => [
        ...(JSON.stringify(x.from) !== JSON.stringify(y.from)
          ? [`起点：${nodeText(a, x.from)} → ${nodeText(b, y.from)}`]
          : []),
        ...(JSON.stringify(x.to) !== JSON.stringify(y.to)
          ? [`终点：${nodeText(a, x.to)} → ${nodeText(b, y.to)}`]
          : []),
        ...(x.condition !== y.condition
          ? [`前提：${quote(x.condition)} → ${quote(y.condition)}`]
          : []),
        ...changed('前提的译文', x.condition_zh, y.condition_zh),
        ...(JSON.stringify(x.requires_endings ?? []) !==
        JSON.stringify(y.requires_endings ?? [])
          ? [
              `前提结局：${endingNames(a, x.requires_endings)} → ${endingNames(b, y.requires_endings)}`
            ]
          : []),
        ...changed('所属分节', x.section, y.section),
        ...stepDetails(x, y, a, b)
      ]
    ),
    ...(segmentOrder
      ? [
          {
            group: '路线段',
            key: '__order__',
            status: 'changed' as const,
            label: '顺序',
            details: ['调整了路线段的顺序']
          }
        ]
      : [])
  ]
}
