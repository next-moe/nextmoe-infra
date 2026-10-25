<script setup lang="ts">
import type { WtChart } from '#shared/types/walkthrough'
import {
  keysOf,
  nextKey,
  referencesOf,
  removeEntry,
  removeSave,
  removeSection
} from '~/utils/walkthrough/doc'

type Kind = 'save' | 'entry' | 'section'

const props = defineProps<{ kind: Kind }>()

const doc = useWalkthroughDocument()
const chart = doc.chart

const META: Record<
  Kind,
  { title: string; icon: string; prefix: string; empty: string; hint: string }
> = {
  save: {
    title: '存档',
    icon: 'lucide:save',
    prefix: 'v',
    empty: '新存档',
    hint: '在路线段里加一个「存档」步骤来建立存档点，之后的路线段可以从它出发。'
  },
  entry: {
    title: '入口',
    icon: 'lucide:flag',
    prefix: 'e',
    empty: 'START',
    hint: '游戏开始的地方，或通关某个结局后解锁的新起点。'
  },
  section: {
    title: '分节',
    icon: 'lucide:bookmark',
    prefix: 's',
    empty: '新分节',
    hint: '把路线段按章节或路线分组，归属到角色后会按角色着色。'
  }
}

const meta = computed(() => META[props.kind])

interface Row {
  key: string
  label: string
  zh: string | null
}

const rows = computed<Row[]>(() => {
  if (props.kind === 'save')
    return chart.value.saves.map((s) => ({
      key: s.key,
      label: s.label,
      zh: null
    }))
  if (props.kind === 'entry')
    return chart.value.entries.map((e) => ({
      key: e.key,
      label: e.label,
      zh: null
    }))
  return chart.value.sections.map((s) => ({
    key: s.key,
    label: s.title,
    zh: s.title_zh
  }))
})

const setLabel = (key: string, value: string) =>
  doc.edit((d) => {
    if (props.kind === 'save') {
      const s = d.saves.find((x) => x.key === key)
      if (s) s.label = value
    } else if (props.kind === 'entry') {
      const e = d.entries.find((x) => x.key === key)
      if (e) e.label = value
    } else {
      const s = d.sections.find((x) => x.key === key)
      if (s) s.title = value
    }
  }, `${props.kind}:${key}`)

const setZh = (key: string, value: string) =>
  doc.edit((d) => {
    const s = d.sections.find((x) => x.key === key)
    if (s) s.title_zh = value || null
  }, `${props.kind}-zh:${key}`)

const entryOf = (key: string) => chart.value.entries.find((e) => e.key === key)
const sectionOf = (key: string) =>
  chart.value.sections.find((s) => s.key === key)

const endingOptions = computed(() => [
  { value: '', label: '一开始就能进入' },
  ...chart.value.endings.map((e) => ({
    value: e.key,
    label: `通关「${e.name || e.key}」后解锁`
  }))
])
const characterOptions = computed(() =>
  chart.value.characters.map((c) => ({ value: c.key, label: c.name || c.key }))
)

const setUnlock = (key: string, value: string) =>
  doc.edit((d) => {
    const e = d.entries.find((x) => x.key === key)
    if (e) e.unlocked_by = value || null
  })

const setSectionCharacters = (key: string, value: string[]) =>
  doc.edit((d) => {
    const s = d.sections.find((x) => x.key === key)
    if (s) s.characters = value
  })

const add = async () => {
  const pool = rows.value.map((r) => ({ key: r.key }))
  const key = nextKey(meta.value.prefix, keysOf(pool))
  doc.edit((d) => {
    if (props.kind === 'save')
      d.saves.push({ key, label: `${meta.value.empty} ${d.saves.length + 1}` })
    else if (props.kind === 'entry')
      d.entries.push({
        key,
        label: d.entries.length ? 'RESTART' : 'START',
        unlocked_by: null
      })
    else
      d.sections.push({
        key,
        title: meta.value.empty,
        title_zh: null,
        characters: []
      })
  })
  doc.expanded.value = `${props.kind}:${key}`
  await nextTick()
  const el = document.querySelector<HTMLInputElement>(
    `[data-wt-label="${props.kind}:${key}"]`
  )
  el?.focus()
  el?.select()
}

const removers: Record<Kind, (d: WtChart, key: string) => void> = {
  save: removeSave,
  entry: removeEntry,
  section: removeSection
}

const remove = async (row: Row) => {
  const refs = referencesOf(chart.value, props.kind, row.key)
  const parts =
    props.kind === 'section'
      ? [refs.segmentsFrom && `${refs.segmentsFrom} 条路线段会变成不分节`]
      : [
          refs.segmentsFrom &&
            `从这里出发的 ${refs.segmentsFrom} 条路线段会被删除`,
          refs.steps && `${refs.steps} 个存档步骤会被删除`
        ]
  const message = parts.filter(Boolean).join('，')
  if (message) {
    const ok = await useKunAlert({
      title: `删除${meta.value.title}「${row.label || row.key}」`,
      message: `${message}。这一步可以撤销。`,
      type: 'warning',
      confirmText: '删除',
      cancelText: '取消'
    })
    if (!ok) return
  }
  doc.edit((d) => removers[props.kind](d, row.key))
}

const toggle = (key: string) => {
  const id = `${props.kind}:${key}`
  doc.expanded.value = doc.expanded.value === id ? null : id
}

const subtitle = (row: Row) => {
  if (props.kind === 'entry') {
    const unlock = entryOf(row.key)?.unlocked_by
    const ending = unlock
      ? chart.value.endings.find((e) => e.key === unlock)
      : null
    return ending ? `通关「${ending.name}」后解锁` : ''
  }
  const refs = referencesOf(chart.value, props.kind, row.key)
  return props.kind === 'save'
    ? `${refs.segmentsFrom} 条路线从这里出发`
    : `${refs.segmentsFrom} 条路线段`
}
</script>

<template>
  <section class="space-y-1">
    <div class="flex items-center justify-between px-2">
      <h3 class="text-default-500 text-xs font-medium">
        {{ meta.title }} · {{ rows.length }}
      </h3>
      <KunButton
        v-if="!doc.readonly.value"
        size="sm"
        variant="light"
        is-icon-only
        :aria-label="`添加${meta.title}`"
        @click="add"
      >
        <KunIcon name="lucide:plus" class="size-4" />
      </KunButton>
    </div>
    <p v-if="!rows.length" class="text-default-400 px-2 text-xs">
      {{ meta.hint }}
    </p>

    <WalkthroughsEditorRailItem
      v-for="row in rows"
      :key="row.key"
      :title="row.label || `未命名${meta.title}`"
      :subtitle="subtitle(row)"
      :icon="meta.icon"
      :issues="doc.issuesOf({ kind: props.kind, key: row.key })"
      :expanded="doc.expanded.value === `${props.kind}:${row.key}`"
      @toggle="toggle(row.key)"
      @remove="remove(row)"
    >
      <KunInput
        :model-value="row.label"
        size="sm"
        label="名称"
        :data-wt-label="`${props.kind}:${row.key}`"
        :disabled="doc.readonly.value"
        @update:model-value="setLabel(row.key, String($event))"
        @blur="doc.breakCoalescing"
      />
      <KunInput
        v-if="props.kind === 'section' && doc.lang.value === 'ja'"
        :model-value="row.zh ?? ''"
        size="sm"
        label="中文标题"
        placeholder="可选"
        :disabled="doc.readonly.value"
        @update:model-value="setZh(row.key, String($event))"
        @blur="doc.breakCoalescing"
      />
      <KunSelect
        v-if="props.kind === 'entry'"
        :model-value="entryOf(row.key)?.unlocked_by ?? ''"
        :options="endingOptions"
        size="sm"
        label="解锁条件"
        :disabled="doc.readonly.value"
        @update:model-value="setUnlock(row.key, String($event ?? ''))"
      />
      <KunSelect
        v-if="props.kind === 'section'"
        :model-value="sectionOf(row.key)?.characters ?? []"
        :options="characterOptions"
        size="sm"
        label="归属角色"
        multiple
        :disabled="doc.readonly.value || !characterOptions.length"
        @update:model-value="
          setSectionCharacters(row.key, ($event as string[]) ?? [])
        "
      />
    </WalkthroughsEditorRailItem>
  </section>
</template>
