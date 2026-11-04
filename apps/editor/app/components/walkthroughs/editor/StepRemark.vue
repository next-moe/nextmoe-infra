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

const inputId = computed(() => `${props.segmentKey}:${props.index}:remark`)

const updateStep = (mutate: (st: WtStep) => void, coalesce?: string) =>
  doc.edit((d) => {
    const st = d.segments.find((s) => s.key === props.segmentKey)?.steps[
      props.index
    ]
    if (st) mutate(st)
  }, coalesce)

const open = async () => {
  if (props.step.remark === null) updateStep((st) => (st.remark = ''))
  await nextTick()
  document
    .querySelector<HTMLElement>(`[data-wt-step-input="${inputId.value}"]`)
    ?.focus()
}

const closeIfBlank = () => {
  doc.breakCoalescing()
  if (props.step.remark !== null && !props.step.remark.trim()) {
    updateStep((st) => {
      st.remark = null
      st.remark_zh = null
    })
  }
}

const onFocus = () =>
  doc.select({ kind: 'segment', key: props.segmentKey, step: props.index })

defineExpose({ open })

const inputClass =
  'border-transparent bg-transparent shadow-none px-2 hover:border-default-200 focus:border-default-300 focus:bg-content1'
</script>

<template>
  <div v-if="props.step.remark !== null" class="flex items-center gap-1 pl-2">
    <span class="text-default-400 shrink-0 text-xs">备注</span>
    <KunInput
      :model-value="props.step.remark"
      size="sm"
      placeholder="选项后的结果或效果，如 推理正解、好感度+1"
      :disabled="doc.readonly.value"
      :data-wt-step-input="inputId"
      :class-name="cn(inputClass, 'text-[13px]')"
      @update:model-value="
        updateStep(
          (st) => (st.remark = String($event)),
          `remark:${props.segmentKey}:${props.index}`
        )
      "
      @focus="onFocus"
      @blur="closeIfBlank"
    />
    <KunInput
      v-if="props.translating"
      :model-value="props.step.remark_zh ?? ''"
      size="sm"
      placeholder="备注译文"
      :disabled="doc.readonly.value"
      :class-name="cn(inputClass, 'text-default-500 text-[13px]')"
      @update:model-value="
        updateStep(
          (st) => (st.remark_zh = String($event) || null),
          `remark-zh:${props.segmentKey}:${props.index}`
        )
      "
      @focus="onFocus"
      @blur="doc.breakCoalescing"
    />
  </div>
</template>
