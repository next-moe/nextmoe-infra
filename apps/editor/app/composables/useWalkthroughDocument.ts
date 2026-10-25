import type { InjectionKey } from 'vue'
import type {
  WtChart,
  WtIssue,
  WtLang,
  WtPin,
  WtTarget
} from '#shared/types/walkthrough'
import {
  cloneChart,
  normalizeChart,
  validateChart
} from '~/utils/walkthrough/doc'

const HISTORY_LIMIT = 300
const COALESCE_MS = 1200

export interface WalkthroughDocumentSource {
  chart: () => unknown
  setChart: (chart: WtChart) => void
  lang: () => WtLang
  setLang: (lang: WtLang) => void
  hidden: () => boolean
  setHidden: (hidden: boolean) => void
  pins: () => WtPin[]
  setPins: (pins: WtPin[]) => void
  workId: () => string | null
  setWorkId: ((id: string) => void) | null
  releaseId: () => string | null
  setReleaseId: (id: string | null) => void
  readonly: () => boolean
}

export const createWalkthroughDocument = (
  source: WalkthroughDocumentSource
) => {
  const chart = shallowRef<WtChart>(
    normalizeChart(cloneChart(source.chart() ?? {}))
  )
  const past = shallowRef<WtChart[]>([])
  const future = shallowRef<WtChart[]>([])
  let group: string | null = null
  let groupAt = 0

  const selection = ref<WtTarget | null>(null)
  const routeFilter = ref<string | null>(null)
  const expanded = ref<string | null>(null)
  const revealTick = ref(0)

  const lang = computed(() => source.lang())
  const hidden = computed(() => source.hidden())
  const pins = computed(() => source.pins())
  const readonly = computed(() => source.readonly())

  const pinByKey = computed(
    () => new Map(pins.value.map((pin) => [pin.chart_key, pin.character_id]))
  )

  const publish = (next: WtChart) => {
    chart.value = next
    source.setChart(next)
  }

  const edit = (mutate: (draft: WtChart) => void, coalesce?: string) => {
    if (readonly.value) return
    const next = cloneChart(chart.value)
    mutate(next)
    const now = Date.now()
    if (!coalesce || coalesce !== group || now - groupAt > COALESCE_MS) {
      past.value = [...past.value.slice(-HISTORY_LIMIT + 1), chart.value]
    }
    group = coalesce ?? null
    groupAt = now
    future.value = []
    publish(next)
  }

  const breakCoalescing = () => {
    group = null
  }

  const undo = () => {
    const previous = past.value.at(-1)
    if (!previous) return
    past.value = past.value.slice(0, -1)
    future.value = [...future.value, chart.value]
    group = null
    publish(previous)
  }

  const redo = () => {
    const next = future.value.at(-1)
    if (!next) return
    future.value = future.value.slice(0, -1)
    past.value = [...past.value, chart.value]
    group = null
    publish(next)
  }

  watch(
    () => toRaw(source.chart()),
    (incoming) => {
      if (incoming === toRaw(chart.value)) return
      past.value = [...past.value, chart.value]
      future.value = []
      group = null
      chart.value = normalizeChart(cloneChart(incoming))
    }
  )

  const setPin = (chartKey: string, characterId: number | null) => {
    const rest = pins.value.filter((pin) => pin.chart_key !== chartKey)
    const next =
      characterId === null
        ? rest
        : [...rest, { chart_key: chartKey, character_id: characterId }]
    source.setPins(next.sort((a, b) => a.chart_key.localeCompare(b.chart_key)))
  }

  const issues = computed<WtIssue[]>(() => validateChart(chart.value))
  const issueKey = (target: WtTarget | null) =>
    !target
      ? '*'
      : target.kind === 'segment' && target.step !== undefined
        ? `segment:${target.key}:${target.step}`
        : `${target.kind}:${target.key}`
  const issuesByTarget = computed(() => {
    const out = new Map<string, string[]>()
    for (const issue of issues.value) {
      const key = issueKey(issue.target)
      out.set(key, [...(out.get(key) ?? []), issue.message])
    }
    return out
  })
  const issuesOf = (target: WtTarget) =>
    issuesByTarget.value.get(issueKey(target)) ?? []

  const select = (target: WtTarget | null, reveal = false) => {
    selection.value = target
    if (reveal) revealTick.value++
  }

  return {
    chart,
    lang,
    hidden,
    pins,
    pinByKey,
    readonly,
    selection,
    routeFilter,
    expanded,
    revealTick,
    issues,
    issuesOf,
    canUndo: computed(() => past.value.length > 0),
    canRedo: computed(() => future.value.length > 0),
    edit,
    breakCoalescing,
    undo,
    redo,
    select,
    setPin,
    setLang: source.setLang,
    setHidden: source.setHidden,
    workId: computed(() => source.workId()),
    setWorkId: source.setWorkId,
    releaseId: computed(() => source.releaseId()),
    setReleaseId: source.setReleaseId
  }
}

export type WalkthroughDocument = ReturnType<typeof createWalkthroughDocument>

const DOCUMENT_KEY: InjectionKey<WalkthroughDocument> = Symbol(
  'walkthrough-document'
)

export const provideWalkthroughDocument = (doc: WalkthroughDocument) =>
  provide(DOCUMENT_KEY, doc)

export const useWalkthroughDocument = () => {
  const doc = inject(DOCUMENT_KEY)
  if (!doc) {
    throw new Error('useWalkthroughDocument needs provideWalkthroughDocument')
  }
  return doc
}
