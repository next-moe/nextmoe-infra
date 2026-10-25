<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WtStep } from '#shared/types/walkthrough'

const props = defineProps<{
  segmentKey: string
  index: number
  step: WtStep
  translating: boolean
}>()

const doc = useWalkthroughDocument()

const update = (mutate: (st: WtStep) => void, coalesce?: string) =>
  doc.edit((d) => {
    const st = d.segments.find((s) => s.key === props.segmentKey)?.steps[props.index]
    if (st) mutate(st)
  }, coalesce)

const add = () =>
  update((st) => {
    st.alt.push('')
    st.alt_zh.push(null)
  })

const removeAt = (i: number) =>
  update((st) => {
    st.alt.splice(i, 1)
    st.alt_zh.splice(i, 1)
  })

const setAlt = (i: number, value: string) =>
  update((st) => (st.alt[i] = value), `alt:${props.segmentKey}:${props.index}:${i}`)

const setAltZh = (i: number, value: string) =>
  update((st) => (st.alt_zh[i] = value || null), `alt-zh:${props.segmentKey}:${props.index}:${i}`)

const popover = ref<{ open: () => void } | null>(null)
defineExpose({ open: () => popover.value?.open() })
</script>

<template>
  <KunPopover ref="popover" position="bottom-end" aria-label="其他写法" inner-class="w-96">
    <template #trigger>
      <button
        type="button"
        tabindex="-1"
        :title="props.step.alt.length ? `${props.step.alt.length} 种其他写法` : '添加其他写法'"
        :class="
          cn(
            'flex h-7 min-w-7 items-center justify-center gap-0.5 rounded-md px-1 text-xs transition-colors',
            props.step.alt.length
              ? 'text-default-600 hover:bg-default-100'
              : 'text-default-300 opacity-0 group-hover/step:opacity-100 hover:text-default-500'
          )
        "
      >
        <KunIcon name="lucide:git-branch-plus" class="size-4" />
        <span v-if="props.step.alt.length" class="tabular-nums">{{ props.step.alt.length }}</span>
      </button>
    </template>

    <div class="space-y-3 p-3">
      <p class="text-default-500 text-xs">
        同一个选项在不同版本里的其他写法，例如移植版改过的文字。
        <template v-if="!props.step.alt.length">还没有其他写法。</template>
      </p>
      <div v-for="(alt, i) in props.step.alt" :key="i" class="flex items-start gap-2">
        <div class="min-w-0 flex-1 space-y-1">
          <KunInput
            :model-value="alt"
            size="sm"
            placeholder="其他写法"
            :disabled="doc.readonly.value"
            @update:model-value="setAlt(i, String($event))"
            @blur="doc.breakCoalescing"
          />
          <KunInput
            v-if="props.translating"
            :model-value="props.step.alt_zh[i] ?? ''"
            size="sm"
            placeholder="中文译文"
            :disabled="doc.readonly.value"
            @update:model-value="setAltZh(i, String($event))"
            @blur="doc.breakCoalescing"
          />
        </div>
        <KunButton
          variant="light"
          size="sm"
          is-icon-only
          aria-label="删除这个写法"
          :disabled="doc.readonly.value"
          @click="removeAt(i)"
        >
          <KunIcon name="lucide:trash-2" class="size-4" />
        </KunButton>
      </div>
      <KunButton size="sm" variant="light" :disabled="doc.readonly.value" @click="add">
        <KunIcon name="lucide:plus" class="size-4" />
        再加一种写法
      </KunButton>
    </div>
  </KunPopover>
</template>
