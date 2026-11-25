<script setup lang="ts">
import type { WtEnding, WtOutcome, WtRoute } from '#shared/types/walkthrough'
import { WT_OUTCOMES, WT_ROUTES, wtTone } from '~/constants/walkthrough'
import {
  characterTones,
  keysOf,
  nextKey,
  referencesOf,
  removeEnding
} from '~/utils/walkthrough/doc'
import { endingLabel } from '~/utils/walkthrough/endings'

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
    d.endings.push({
      key,
      name: '',
      name_zh: null,
      route_type: 'heroine',
      outcome: 'happy',
      after: false,
      other: false,
      other_label: null,
      characters: []
    })
  )
  doc.expanded.value = `ending:${key}`
  await nextTick()
  document
    .querySelector<HTMLInputElement>(`[data-wt-ending-name="${key}"]`)
    ?.focus()
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

const toggleOther = (e: WtEnding) =>
  update(
    e.key,
    e.other
      ? { other: false, other_label: null }
      : { other: true, route_type: null, outcome: null }
  )

const subtitle = (e: WtEnding) =>
  [
    endingLabel(e),
    e.characters
      .map((k) => chart.value.characters.find((c) => c.key === k)?.name || k)
      .join('、')
  ]
    .filter(Boolean)
    .join(' · ')
</script>

<template>
  <section class="space-y-1">
    <div class="flex items-center justify-between px-2">
      <h3 class="text-default-500 text-xs font-medium">
        结局 · {{ chart.endings.length }}
      </h3>
      <KunButton
        v-if="!doc.readonly.value"
        size="sm"
        variant="light"
        is-icon-only
        aria-label="添加结局"
        @click="add"
      >
        <KunIcon name="lucide:plus" class="size-4" />
      </KunButton>
    </div>

    <WalkthroughsEditorRailItem
      v-for="e in chart.endings"
      :key="e.key"
      :title="e.name || '未命名结局'"
      :subtitle="subtitle(e)"
      :dot="
        wtTone(e.characters[0] ? (tones.get(e.characters[0]) ?? null) : null)
          .dot
      "
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
        @update:model-value="
          update(e.key, { name: String($event) }, `ending:${e.key}`)
        "
        @blur="doc.breakCoalescing"
      />
      <KunInput
        v-if="doc.lang.value === 'ja'"
        :model-value="e.name_zh ?? ''"
        size="sm"
        label="中文名"
        placeholder="可选"
        :disabled="doc.readonly.value"
        @update:model-value="
          update(
            e.key,
            { name_zh: String($event) || null },
            `ending-zh:${e.key}`
          )
        "
        @blur="doc.breakCoalescing"
      />
      <div class="grid grid-cols-2 gap-2">
        <KunSelect
          :model-value="e.route_type"
          :options="WT_ROUTES"
          size="sm"
          label="线路类型"
          placeholder="未选"
          clearable
          :disabled="doc.readonly.value || e.other"
          @update:model-value="
            update(e.key, { route_type: ($event as WtRoute | null) || null })
          "
        />
        <KunSelect
          :model-value="e.outcome"
          :options="WT_OUTCOMES"
          size="sm"
          label="结局类型"
          placeholder="未选"
          clearable
          :disabled="doc.readonly.value || e.other"
          @update:model-value="
            update(e.key, { outcome: ($event as WtOutcome | null) || null })
          "
        />
      </div>
      <div class="flex gap-2">
        <KunButton
          size="sm"
          :variant="e.after ? 'flat' : 'bordered'"
          :color="e.after ? 'primary' : 'default'"
          :disabled="doc.readonly.value"
          @click="update(e.key, { after: !e.after })"
        >
          后日谈
        </KunButton>
        <KunButton
          size="sm"
          :variant="e.other ? 'flat' : 'bordered'"
          :color="e.other ? 'primary' : 'default'"
          :disabled="doc.readonly.value"
          @click="toggleOther(e)"
        >
          其他
        </KunButton>
      </div>
      <KunInput
        v-if="e.other"
        :model-value="e.other_label ?? ''"
        size="sm"
        label="备注"
        placeholder="可选，例如「隐藏结局」"
        :disabled="doc.readonly.value"
        @update:model-value="
          update(
            e.key,
            { other_label: String($event).trim() ? String($event) : null },
            `ending-other:${e.key}`
          )
        "
        @blur="doc.breakCoalescing"
      />
      <KunSelect
        :model-value="e.characters"
        :options="characterOptions"
        size="sm"
        label="归属角色"
        :placeholder="
          e.route_type === 'heroine'
            ? '个人线只能选一个角色'
            : '后宫结局可以选多个'
        "
        multiple
        :disabled="doc.readonly.value || !characterOptions.length"
        @update:model-value="
          update(e.key, { characters: ($event as string[]) ?? [] })
        "
      />
    </WalkthroughsEditorRailItem>
  </section>
</template>
