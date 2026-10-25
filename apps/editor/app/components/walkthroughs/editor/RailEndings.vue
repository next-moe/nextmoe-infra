<script setup lang="ts">
import type { WtEnding, WtEndingType } from '#shared/types/walkthrough'
import { WT_ENDING_LABEL, WT_ENDING_TYPES, wtTone } from '~/constants/walkthrough'
import { characterTones, keysOf, nextKey, referencesOf, removeEnding } from '~/utils/walkthrough/doc'

const doc = useWalkthroughDocument()
const chart = doc.chart
const tones = computed(() => characterTones(chart.value))

const characterOptions = computed(() =>
  chart.value.characters.map((c) => ({ value: c.key, label: c.name || c.key }))
)

const update = (key: string, patch: Partial<WtEnding>, coalesce?: string) =>
  doc.edit((d) => {
    const e = d.endings.find((x) => x.key === key)
    if (e) Object.assign(e, patch)
  }, coalesce)

const add = async () => {
  const key = nextKey('n', keysOf(chart.value.endings))
  doc.edit((d) =>
    d.endings.push({ key, name: '', name_zh: null, ending_type: 'heroine', characters: [] })
  )
  doc.expanded.value = `ending:${key}`
  await nextTick()
  document.querySelector<HTMLInputElement>(`[data-wt-ending-name="${key}"]`)?.focus()
}

const remove = async (e: WtEnding) => {
  const refs = referencesOf(chart.value, 'ending', e.key)
  if (refs.segmentsTo + refs.segmentsFrom + refs.entries) {
    const parts = [
      refs.segmentsTo && `${refs.segmentsTo} 条路线段的终点会变成「未完待续」`,
      refs.segmentsFrom && `从它出发的 ${refs.segmentsFrom} 条路线段会被删除`,
      refs.entries && `${refs.entries} 个入口不再需要它解锁`
    ].filter(Boolean)
    const ok = await useKunAlert({
      title: `删除结局「${e.name || e.key}」`,
      message: `${parts.join('，')}。这一步可以撤销。`,
      type: 'warning',
      confirmText: '删除',
      cancelText: '取消'
    })
    if (!ok) return
  }
  doc.edit((d) => removeEnding(d, e.key))
}

const toggle = (key: string) => {
  const id = `ending:${key}`
  doc.expanded.value = doc.expanded.value === id ? null : id
}

const subtitle = (e: WtEnding) =>
  [WT_ENDING_LABEL[e.ending_type], e.characters.map((k) => chart.value.characters.find((c) => c.key === k)?.name || k).join('、')]
    .filter(Boolean)
    .join(' · ')
</script>

<template>
  <section class="space-y-1">
    <div class="flex items-center justify-between px-2">
      <h3 class="text-default-500 text-xs font-medium">结局 · {{ chart.endings.length }}</h3>
      <KunButton v-if="!doc.readonly.value" size="sm" variant="light" is-icon-only aria-label="添加结局" @click="add">
        <KunIcon name="lucide:plus" class="size-4" />
      </KunButton>
    </div>

    <WalkthroughsEditorRailItem
      v-for="e in chart.endings"
      :key="e.key"
      :title="e.name || '未命名结局'"
      :subtitle="subtitle(e)"
      :dot="wtTone(e.characters[0] ? (tones.get(e.characters[0]) ?? null) : null).dot"
      :issues="doc.issuesOf({ kind: 'ending', key: e.key })"
      :expanded="doc.expanded.value === `ending:${e.key}`"
      @toggle="toggle(e.key)"
      @remove="remove(e)"
    >
      <KunInput
        :model-value="e.name"
        size="sm"
        label="结局名"
        :data-wt-ending-name="e.key"
        :disabled="doc.readonly.value"
        @update:model-value="update(e.key, { name: String($event) }, `ending:${e.key}`)"
        @blur="doc.breakCoalescing"
      />
      <KunInput
        v-if="doc.lang.value === 'ja'"
        :model-value="e.name_zh ?? ''"
        size="sm"
        label="中文名"
        placeholder="可选"
        :disabled="doc.readonly.value"
        @update:model-value="update(e.key, { name_zh: String($event) || null }, `ending-zh:${e.key}`)"
        @blur="doc.breakCoalescing"
      />
      <KunSelect
        :model-value="e.ending_type"
        :options="WT_ENDING_TYPES"
        size="sm"
        label="类型"
        :disabled="doc.readonly.value"
        @update:model-value="update(e.key, { ending_type: $event as WtEndingType })"
      />
      <KunSelect
        :model-value="e.characters"
        :options="characterOptions"
        size="sm"
        label="归属角色"
        placeholder="后宫结局可以选多个"
        multiple
        :disabled="doc.readonly.value || !characterOptions.length"
        @update:model-value="update(e.key, { characters: ($event as string[]) ?? [] })"
      />
    </WalkthroughsEditorRailItem>
  </section>
</template>
