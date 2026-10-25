<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { KunDropdownItem } from '@kungal/ui-vue'
import type { WtStep, WtStepType } from '#shared/types/walkthrough'
import { WT_STEP_META } from '~/constants/walkthrough'
import { blankStep, keysOf, nextKey } from '~/utils/walkthrough/doc'
import {
  composing,
  focusSibling,
  focusStep,
  isMod
} from '~/utils/walkthrough/focus'
import { useStepDrag } from '~/utils/walkthrough/drag'

const props = defineProps<{
  segmentKey: string
  index: number
  step: WtStep
  translating: boolean
}>()

const doc = useWalkthroughDocument()
const drag = useStepDrag()
const alts = ref<{ open: () => void } | null>(null)

const meta = computed(() => WT_STEP_META[props.step.step_type])
const issues = computed(() =>
  doc.issuesOf({ kind: 'segment', key: props.segmentKey, step: props.index })
)
const active = computed(() => {
  const s = doc.selection.value
  return (
    s?.kind === 'segment' &&
    s.key === props.segmentKey &&
    s.step === props.index
  )
})
const dropLine = computed(() => {
  const over = drag.stepOver.value
  return over && over.segment === props.segmentKey && over.index === props.index
    ? over.after
      ? 'after'
      : 'before'
    : null
})

const updateStep = (mutate: (st: WtStep) => void, coalesce?: string) =>
  doc.edit((d) => {
    const st = d.segments.find((s) => s.key === props.segmentKey)?.steps[
      props.index
    ]
    if (st) mutate(st)
  }, coalesce)

const withSegment = (mutate: (steps: WtStep[]) => void) =>
  doc.edit((d) => {
    const s = d.segments.find((x) => x.key === props.segmentKey)
    if (s) mutate(s.steps)
  })

const insertAfter = (stepType: WtStepType = 'choice') => {
  withSegment((steps) => steps.splice(props.index + 1, 0, blankStep(stepType)))
  focusStep(props.segmentKey, props.index + 1, 'start')
}

const remove = (focus: 'previous' | 'next' = 'previous') => {
  const count =
    doc.chart.value.segments.find((s) => s.key === props.segmentKey)?.steps
      .length ?? 0
  withSegment((steps) => steps.splice(props.index, 1))
  if (focus === 'previous' && props.index > 0) {
    focusStep(props.segmentKey, props.index - 1, 'end')
  } else if (count > 1) {
    focusStep(props.segmentKey, Math.min(props.index, count - 2), 'start')
  }
}

const move = (delta: number) => {
  const steps =
    doc.chart.value.segments.find((s) => s.key === props.segmentKey)?.steps ??
    []
  const to = props.index + delta
  if (to < 0 || to >= steps.length) return
  withSegment((list) => {
    const [st] = list.splice(props.index, 1)
    list.splice(to, 0, st!)
  })
  focusStep(props.segmentKey, to)
}

const duplicate = () => {
  withSegment((steps) =>
    steps.splice(props.index + 1, 0, structuredClone(toRaw(props.step)))
  )
  focusStep(props.segmentKey, props.index + 1)
}

const toggleAchievement = () =>
  updateStep((st) => (st.achievement = !st.achievement))

const onKeydown = (event: KeyboardEvent, field: 'text' | 'zh') => {
  if (composing(event)) return
  const target = event.currentTarget as HTMLElement
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault()
    insertAfter()
  } else if (
    event.key === 'Backspace' &&
    !isMod(event) &&
    field === 'text' &&
    !props.step.text &&
    !props.step.text_zh &&
    !props.step.alt.length
  ) {
    event.preventDefault()
    remove('previous')
  } else if (isMod(event) && event.key === 'Backspace') {
    event.preventDefault()
    remove('previous')
  } else if (
    event.altKey &&
    (event.key === 'ArrowUp' || event.key === 'ArrowDown')
  ) {
    event.preventDefault()
    move(event.key === 'ArrowUp' ? -1 : 1)
  } else if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
    event.preventDefault()
    focusSibling(target, event.key === 'ArrowUp' ? -1 : 1)
  } else if (isMod(event) && event.code === 'KeyD') {
    event.preventDefault()
    duplicate()
  } else if (event.altKey && event.code === 'KeyA') {
    event.preventDefault()
    toggleAchievement()
  } else if (event.key === 'Escape') {
    target.blur()
  }
}

const onFocus = () =>
  doc.select({ kind: 'segment', key: props.segmentKey, step: props.index })

const typeItems = computed<KunDropdownItem[]>(() =>
  (Object.keys(WT_STEP_META) as WtStepType[]).map((type) => ({
    key: type,
    label: WT_STEP_META[type].label,
    icon: WT_STEP_META[type].icon,
    disabled: type === props.step.step_type
  }))
)

const setType = (item: KunDropdownItem) => {
  const type = item.key as WtStepType
  doc.edit((d) => {
    const st = d.segments.find((s) => s.key === props.segmentKey)?.steps[
      props.index
    ]
    if (!st) return
    if (type === 'save') {
      let save = d.saves[0]?.key ?? null
      if (!save) {
        save = nextKey('v', keysOf(d.saves))
        d.saves.push({ key: save, label: '存档 1' })
      }
      Object.assign(st, blankStep('save', save))
    } else {
      if (st.step_type === 'save') Object.assign(st, blankStep(type))
      st.step_type = type
    }
  })
}

const saveOptions = computed(() => [
  ...doc.chart.value.saves.map((s) => ({
    value: s.key,
    label: s.label || s.key
  })),
  { value: '__new__', label: '＋ 新存档…' }
])

const setSave = (value: string) => {
  if (value === '__new__') {
    const key = nextKey('v', keysOf(doc.chart.value.saves))
    doc.edit((d) => {
      d.saves.push({ key, label: `存档 ${d.saves.length + 1}` })
      const st = d.segments.find((s) => s.key === props.segmentKey)?.steps[
        props.index
      ]
      if (st) st.save = key
    })
    doc.select({ kind: 'save', key }, true)
    return
  }
  updateStep((st) => (st.save = value))
}

const inputClass =
  'border-transparent bg-transparent shadow-none px-2 hover:border-default-200 focus:border-default-300 focus:bg-content1'
</script>

<template>
  <li
    :data-wt-step="`${props.segmentKey}:${props.index}`"
    :class="
      cn(
        'group/step relative flex items-start gap-1 rounded-lg py-0.5 pr-1 transition-colors',
        active ? 'bg-primary-50' : 'hover:bg-default-50',
        drag.dragging.value?.kind === 'step' &&
          drag.dragging.value.segment === props.segmentKey &&
          drag.dragging.value.index === props.index &&
          'opacity-40'
      )
    "
    @dragover="drag.overStep($event, props.segmentKey, props.index)"
    @drop="drag.dropOnStep($event, props.segmentKey, props.index)"
  >
    <span
      v-if="dropLine"
      :class="
        cn(
          'bg-primary-500 pointer-events-none absolute inset-x-2 z-10 h-0.5 rounded-full',
          dropLine === 'before' ? '-top-px' : '-bottom-px'
        )
      "
    />

    <span
      v-if="!doc.readonly.value"
      draggable="true"
      class="text-default-300 hover:text-default-500 mt-2 w-3 shrink-0 cursor-grab opacity-0 group-hover/step:opacity-100 active:cursor-grabbing"
      aria-hidden="true"
      @dragstart="drag.startStep($event, props.segmentKey, props.index)"
      @dragend="drag.end"
    >
      <KunIcon name="lucide:grip-vertical" class="size-3.5" />
    </span>

    <span
      class="text-default-400 mt-2 w-5 shrink-0 text-right text-xs tabular-nums"
    >
      {{ props.index + 1 }}
    </span>

    <KunDropdown :items="typeItems" position="bottom-start" @select="setType">
      <template #trigger>
        <button
          type="button"
          tabindex="-1"
          :disabled="doc.readonly.value"
          :class="
            cn(
              'mt-1 flex size-7 shrink-0 items-center justify-center rounded-md transition-colors',
              props.step.step_type === 'save'
                ? 'bg-success-100 text-success-700'
                : props.step.step_type === 'action'
                  ? 'bg-default-100 text-default-600'
                  : 'text-primary-600 hover:bg-primary-100'
            )
          "
          :title="`${meta.label}（点击切换类型）`"
        >
          <KunIcon :name="meta.icon" class="size-4" />
        </button>
      </template>
    </KunDropdown>

    <div
      v-if="props.step.step_type === 'save'"
      class="flex min-w-0 flex-1 items-center gap-2 py-1"
    >
      <span class="text-default-500 text-sm">在这里建立存档</span>
      <KunSelect
        :model-value="props.step.save ?? ''"
        :options="saveOptions"
        size="sm"
        :full-width="false"
        popup-width="auto"
        aria-label="存档"
        :disabled="doc.readonly.value"
        :error="issues[0] ?? ''"
        class-name="w-48"
        @update:model-value="setSave(String($event ?? ''))"
      />
    </div>

    <div v-else class="min-w-0 flex-1">
      <KunInput
        :model-value="props.step.text ?? ''"
        size="sm"
        :placeholder="meta.placeholder"
        :disabled="doc.readonly.value"
        :is-invalid="issues.length > 0"
        :data-wt-step-input="`${props.segmentKey}:${props.index}`"
        :class-name="cn(inputClass, 'text-[15px]')"
        @update:model-value="
          updateStep(
            (st) => (st.text = String($event)),
            `text:${props.segmentKey}:${props.index}`
          )
        "
        @keydown="onKeydown($event, 'text')"
        @focus="onFocus"
        @blur="doc.breakCoalescing"
      />
      <KunInput
        v-if="props.translating"
        :model-value="props.step.text_zh ?? ''"
        size="sm"
        placeholder="中文译文"
        :disabled="doc.readonly.value"
        :data-wt-step-input="`${props.segmentKey}:${props.index}:zh`"
        :class-name="cn(inputClass, 'text-default-500 text-[13px]')"
        @update:model-value="
          updateStep(
            (st) => (st.text_zh = String($event) || null),
            `zh:${props.segmentKey}:${props.index}`
          )
        "
        @keydown="onKeydown($event, 'zh')"
        @focus="onFocus"
        @blur="doc.breakCoalescing"
      />
      <div v-if="props.step.alt.length" class="flex flex-wrap gap-1 px-2 pb-1">
        <button
          v-for="(alt, i) in props.step.alt"
          :key="i"
          type="button"
          tabindex="-1"
          class="bg-default-100 text-default-600 hover:bg-default-200 rounded px-1.5 py-0.5 text-xs"
          @click="alts?.open()"
        >
          或「{{ alt || '…' }}」
        </button>
      </div>
    </div>

    <div
      v-if="props.step.step_type !== 'save'"
      class="mt-1 flex shrink-0 items-center"
    >
      <KunTooltip text="☆ 标记（Alt + A）" position="top">
        <button
          type="button"
          tabindex="-1"
          :disabled="doc.readonly.value"
          :aria-pressed="props.step.achievement"
          :class="
            cn(
              'flex size-7 items-center justify-center rounded-md transition-colors',
              props.step.achievement
                ? 'text-warning-500'
                : 'text-default-300 hover:text-default-500 opacity-0 group-hover/step:opacity-100'
            )
          "
          @click="toggleAchievement"
        >
          <KunIcon
            name="lucide:star"
            :class="cn('size-4', props.step.achievement && 'fill-current')"
          />
        </button>
      </KunTooltip>
      <WalkthroughsEditorStepAlts
        ref="alts"
        :segment-key="props.segmentKey"
        :index="props.index"
        :step="props.step"
        :translating="props.translating"
      />
    </div>

    <div
      v-if="!doc.readonly.value"
      class="mt-1 flex shrink-0 items-center opacity-0 group-hover/step:opacity-100 focus-within:opacity-100"
    >
      <button
        type="button"
        tabindex="-1"
        class="text-default-400 hover:bg-danger-50 hover:text-danger-600 flex size-7 items-center justify-center rounded-md"
        title="删除这一步（Mod + ⌫）"
        @click="remove('next')"
      >
        <KunIcon name="lucide:x" class="size-4" />
      </button>
    </div>
  </li>
</template>
