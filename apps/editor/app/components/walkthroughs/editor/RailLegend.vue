<script setup lang="ts">
const doc = useWalkthroughDocument()
const legend = computed(() => doc.chart.value.legend)

const setText = (i: number, value: string) =>
  doc.edit((d) => {
    const l = d.legend[i]
    if (l) l.text = value
  }, `legend:${i}`)

const setZh = (i: number, value: string) =>
  doc.edit((d) => {
    const l = d.legend[i]
    if (l) l.text_zh = value || null
  }, `legend-zh:${i}`)

const add = async () => {
  const i = legend.value.length
  doc.edit((d) => d.legend.push({ text: '', text_zh: null }))
  doc.expanded.value = `legend:${i}`
  await nextTick()
  document.querySelector<HTMLElement>(`[data-wt-legend="${i}"]`)?.focus()
}

const remove = (i: number) =>
  doc.edit((d) => {
    d.legend.splice(i, 1)
  })

const toggle = (i: number) => {
  const id = `legend:${i}`
  doc.expanded.value = doc.expanded.value === id ? null : id
}
</script>

<template>
  <section class="space-y-1">
    <div class="flex items-center justify-between px-2">
      <h3 class="text-default-500 text-xs font-medium">
        符号说明 · {{ legend.length }}
      </h3>
      <KunButton
        v-if="!doc.readonly.value"
        size="sm"
        variant="light"
        is-icon-only
        aria-label="添加符号说明"
        @click="add"
      >
        <KunIcon name="lucide:plus" class="size-4" />
      </KunButton>
    </div>
    <p v-if="!legend.length" class="text-default-400 px-2 text-xs">
      选项里的 ★、⇒ 等符号在每个攻略里含义不同，在这里写明，例如「★的选项影响推理评价」。只写符号的含义。
    </p>

    <WalkthroughsEditorRailItem
      v-for="(line, i) in legend"
      :key="i"
      :title="line.text || '未填写的说明'"
      :subtitle="line.text_zh ?? ''"
      icon="lucide:info"
      :issues="doc.issuesOf({ kind: 'legend', key: String(i) })"
      :expanded="doc.expanded.value === `legend:${i}`"
      @toggle="toggle(i)"
      @remove="remove(i)"
    >
      <KunInput
        :model-value="line.text"
        size="sm"
        label="说明"
        :data-wt-legend="i"
        :disabled="doc.readonly.value"
        @update:model-value="setText(i, String($event))"
        @blur="doc.breakCoalescing"
      />
      <KunInput
        v-if="doc.lang.value === 'ja'"
        :model-value="line.text_zh ?? ''"
        size="sm"
        label="中文译文"
        placeholder="可选"
        :disabled="doc.readonly.value"
        @update:model-value="setZh(i, String($event))"
        @blur="doc.breakCoalescing"
      />
    </WalkthroughsEditorRailItem>
  </section>
</template>
