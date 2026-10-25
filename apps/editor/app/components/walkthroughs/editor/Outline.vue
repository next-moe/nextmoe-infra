<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WtNode, WtSegment } from '#shared/types/walkthrough'
import { wtTone } from '~/constants/walkthrough'
import {
  blankStep,
  characterTones,
  keysOf,
  nextKey,
  segmentCharacterKey
} from '~/utils/walkthrough/doc'
import { focusStep } from '~/utils/walkthrough/focus'

const SHOW_ZH_KEY = 'nextmoe-editor-walkthrough-show-zh'
const COMMON = '__common__'

const doc = useWalkthroughDocument()
const chart = doc.chart

const showZh = ref(true)
onMounted(() => {
  try {
    showZh.value = localStorage.getItem(SHOW_ZH_KEY) !== '0'
  } catch {
    showZh.value = true
  }
})
watch(showZh, (next) => {
  try {
    localStorage.setItem(SHOW_ZH_KEY, next ? '1' : '0')
  } catch {
    return
  }
})
const translating = computed(() => doc.lang.value === 'ja' && showZh.value)

const tones = computed(() => characterTones(chart.value))
const routeOf = (segment: WtSegment) => segmentCharacterKey(chart.value, segment)

const filterChips = computed(() => {
  const used = new Set(chart.value.segments.map((s) => routeOf(s) ?? COMMON))
  return [
    ...chart.value.characters
      .filter((c) => used.has(c.key))
      .map((c) => ({ key: c.key, label: c.name || c.key, tone: tones.value.get(c.key) ?? null })),
    ...(used.has(COMMON) ? [{ key: COMMON, label: '共通 / 其他', tone: null }] : [])
  ]
})

const rows = computed(() => {
  const out: {
    segment: WtSegment
    index: number
    tone: number | null
    sectionStart: string | null
  }[] = []
  let previous: string | null | undefined
  chart.value.segments.forEach((segment, index) => {
    const routeKey = routeOf(segment)
    const route = routeKey ?? COMMON
    if (doc.routeFilter.value && doc.routeFilter.value !== route) return
    out.push({
      segment,
      index,
      tone: routeKey ? (tones.value.get(routeKey) ?? null) : null,
      sectionStart:
        segment.section !== previous && segment.section !== null ? segment.section : null
    })
    previous = segment.section
  })
  return out
})

const sectionOf = (key: string) => chart.value.sections.find((s) => s.key === key)

const defaultFrom = (): WtNode => {
  const lastSave = [...chart.value.segments]
    .reverse()
    .flatMap((s) => [...s.steps].reverse())
    .find((st) => st.step_type === 'save' && st.save)
  if (lastSave?.save) return { node_type: 'save', key: lastSave.save }
  const entry = chart.value.entries[0]
  return entry ? { node_type: 'entry', key: entry.key } : { node_type: 'entry', key: null }
}

const addSegment = (afterIndex: number | null = null) => {
  const key = nextKey('g', keysOf(chart.value.segments))
  const after = afterIndex === null ? null : chart.value.segments[afterIndex]
  doc.edit((d) => {
    if (!d.entries.length) {
      d.entries.push({ key: 'e1', label: 'START', unlocked_by: null })
    }
    const segment: WtSegment = {
      key,
      section: after?.section ?? null,
      from: after ? (after.to.node_type === 'ending' ? { ...after.to } : defaultFrom()) : defaultFrom(),
      condition: null,
      condition_zh: null,
      steps: [blankStep('choice')],
      to: { node_type: 'open', key: null }
    }
    if (segment.from.node_type === 'entry' && !segment.from.key) {
      segment.from = { node_type: 'entry', key: d.entries[0]!.key }
    }
    d.segments.splice(afterIndex === null ? d.segments.length : afterIndex + 1, 0, segment)
  })
  doc.select({ kind: 'segment', key })
  focusStep(key, 0)
}

const scroller = ref<HTMLElement | null>(null)
watch(
  () => doc.revealTick.value,
  async () => {
    const target = doc.selection.value
    if (!target) return
    if (target.kind === 'segment') {
      const route = (() => {
        const seg = chart.value.segments.find((s) => s.key === target.key)
        return seg ? (routeOf(seg) ?? COMMON) : null
      })()
      if (doc.routeFilter.value && route !== doc.routeFilter.value) {
        doc.routeFilter.value = null
      }
      await nextTick()
      if (target.step !== undefined && (await focusStep(target.key, target.step))) {
        return
      }
      scroller.value
        ?.querySelector(`[data-wt-segment="${target.key}"]`)
        ?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
  }
)
</script>

<template>
  <div class="bg-background flex h-full min-h-0 flex-col">
    <div
      class="border-default-200 bg-content1 flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-2"
    >
      <div class="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
        <button
          type="button"
          :class="
            cn(
              'rounded-full px-2.5 py-1 text-xs transition-colors',
              doc.routeFilter.value === null
                ? 'bg-foreground text-background'
                : 'bg-default-100 text-default-600 hover:bg-default-200'
            )
          "
          @click="doc.routeFilter.value = null"
        >
          全部路线
        </button>
        <button
          v-for="chip in filterChips"
          :key="chip.key"
          type="button"
          :class="
            cn(
              'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs transition-colors',
              doc.routeFilter.value === chip.key
                ? wtTone(chip.tone).chip + ' ring-1 ring-current'
                : 'bg-default-100 text-default-600 hover:bg-default-200'
            )
          "
          @click="doc.routeFilter.value = doc.routeFilter.value === chip.key ? null : chip.key"
        >
          <span :class="cn('size-2 rounded-full', wtTone(chip.tone).dot)" />
          {{ chip.label }}
        </button>
      </div>
      <KunSwitch
        v-if="doc.lang.value === 'ja'"
        v-model="showZh"
        size="sm"
        label="显示译文"
      />
      <KunButton
        size="sm"
        variant="flat"
        color="primary"
        :disabled="doc.readonly.value"
        @click="addSegment()"
      >
        <KunIcon name="lucide:plus" class="size-4" />
        路线段
      </KunButton>
    </div>

    <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto">
      <div class="mx-auto w-full max-w-4xl space-y-3 px-4 py-4 md:px-6">
        <template v-for="row in rows" :key="row.segment.key">
          <WalkthroughsEditorSectionHeader
            v-if="row.sectionStart && sectionOf(row.sectionStart)"
            :section="sectionOf(row.sectionStart)!"
          />
          <WalkthroughsEditorSegment
            :segment="row.segment"
            :index="row.index"
            :tone="row.tone"
            :translating="translating"
            @add-after="addSegment(row.index)"
          />
        </template>

        <WorkspaceEmpty
          v-if="!rows.length"
          icon="lucide:route"
          :title="chart.segments.length ? '这条路线没有路线段' : '还没有路线段'"
          description="路线段是从一个入口、存档或结局出发，经过若干选项，走到下一个存档或结局的一段路。"
        />

        <button
          v-if="!doc.readonly.value"
          type="button"
          class="border-default-200 text-default-500 hover:border-primary-300 hover:text-primary-600 flex w-full items-center justify-center gap-2 rounded-xl border border-dashed py-3 text-sm transition-colors"
          @click="addSegment()"
        >
          <KunIcon name="lucide:plus" class="size-4" />
          添加路线段
        </button>
      </div>
    </div>
  </div>
</template>
