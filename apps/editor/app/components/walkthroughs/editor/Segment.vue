<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { KunDropdownItem } from '@kungal/ui-vue'
import type { WtNode, WtSegment, WtStepType } from '#shared/types/walkthrough'
import { wtTone } from '~/constants/walkthrough'
import { blankStep, keysOf, nextKey } from '~/utils/walkthrough/doc'
import { focusStep } from '~/utils/walkthrough/focus'
import { useStepDrag } from '~/utils/walkthrough/drag'

const props = defineProps<{
  segment: WtSegment
  index: number
  tone: number | null
  translating: boolean
}>()

const emit = defineEmits<{ 'add-after': [] }>()

const doc = useWalkthroughDocument()
const drag = useStepDrag()

const selected = computed(
  () =>
    doc.selection.value?.kind === 'segment' &&
    doc.selection.value.key === props.segment.key
)
const issues = computed(() =>
  doc.issuesOf({ kind: 'segment', key: props.segment.key })
)
const conditionOpen = ref(Boolean(props.segment.condition))

const update = (mutate: (s: WtSegment) => void, coalesce?: string) =>
  doc.edit((d) => {
    const s = d.segments.find((x) => x.key === props.segment.key)
    if (s) mutate(s)
  }, coalesce)

const setFrom = (node: WtNode) => update((s) => (s.from = node))
const setTo = (node: WtNode) => update((s) => (s.to = node))

const addStep = (stepType: WtStepType) => {
  let index = props.segment.steps.length
  doc.edit((d) => {
    const s = d.segments.find((x) => x.key === props.segment.key)
    if (!s) return
    let save: string | null = null
    if (stepType === 'save') {
      save = nextKey('v', keysOf(d.saves))
      d.saves.push({ key: save, label: `存档 ${d.saves.length + 1}` })
    }
    s.steps.push(blankStep(stepType, save))
    index = s.steps.length - 1
  })
  if (stepType !== 'save') focusStep(props.segment.key, index)
}

const sectionOptions = computed(() => [
  { value: '', label: '不分节' },
  ...doc.chart.value.sections.map((s) => ({
    value: s.key,
    label: s.title || s.key
  }))
])

const setSection = async (value: string) => {
  if (value === '__new__') return
  update((s) => (s.section = value || null))
}

const newSection = () => {
  const key = nextKey('s', keysOf(doc.chart.value.sections))
  doc.edit((d) => {
    d.sections.push({ key, title: '新分节', title_zh: null, characters: [] })
    const s = d.segments.find((x) => x.key === props.segment.key)
    if (s) s.section = key
  })
}

const menu = computed<KunDropdownItem[]>(
  () =>
    [
      {
        key: 'up',
        label: '上移',
        icon: 'lucide:arrow-up',
        disabled: props.index === 0
      },
      {
        key: 'down',
        label: '下移',
        icon: 'lucide:arrow-down',
        disabled: props.index === doc.chart.value.segments.length - 1
      },
      { key: 'after', label: '在后面添加路线段', icon: 'lucide:list-plus' },
      { key: 'duplicate', label: '复制路线段', icon: 'lucide:copy' },
      { key: 'section', label: '新建分节并放入', icon: 'lucide:bookmark-plus' },
      { type: 'separator' },
      {
        key: 'delete',
        label: '删除路线段',
        icon: 'lucide:trash-2',
        color: 'danger'
      }
    ] as KunDropdownItem[]
)

const move = (delta: number) =>
  doc.edit((d) => {
    const i = d.segments.findIndex((x) => x.key === props.segment.key)
    const j = i + delta
    if (i < 0 || j < 0 || j >= d.segments.length) return
    const [s] = d.segments.splice(i, 1)
    d.segments.splice(j, 0, s!)
  })

const onMenu = async (item: KunDropdownItem) => {
  if (item.key === 'up') move(-1)
  else if (item.key === 'down') move(1)
  else if (item.key === 'after') emit('add-after')
  else if (item.key === 'section') newSection()
  else if (item.key === 'duplicate') {
    const key = nextKey('g', keysOf(doc.chart.value.segments))
    doc.edit((d) => {
      const i = d.segments.findIndex((x) => x.key === props.segment.key)
      const copy = structuredClone(toRaw(d.segments[i]!))
      copy.key = key
      d.segments.splice(i + 1, 0, copy)
    })
  } else if (item.key === 'delete') {
    doc.edit((d) => {
      d.segments = d.segments.filter((x) => x.key !== props.segment.key)
    })
    useKunMessage('已删除路线段，Ctrl/⌘ + Z 可以撤销', 'info')
  }
}

const segmentDrag = useStepDrag()
const onHeaderDragStart = (event: DragEvent) =>
  segmentDrag.startSegment(event, props.segment.key)
</script>

<template>
  <section
    :data-wt-segment="props.segment.key"
    :class="
      cn(
        'bg-content1 relative rounded-xl border transition-[box-shadow,border-color] [contain-intrinsic-size:auto_180px] [content-visibility:auto]',
        selected ? 'border-primary-300 shadow-kun-md' : 'border-default-200',
        issues.length && !selected && 'border-warning-300',
        drag.segmentOver.value === props.segment.key &&
          'ring-primary-300 ring-2'
      )
    "
    @click.self="doc.select({ kind: 'segment', key: props.segment.key })"
    @dragover="drag.overSegment($event, props.segment.key)"
    @drop="drag.dropOnSegment($event, props.segment.key)"
  >
    <span
      :class="
        cn(
          'absolute inset-y-3 left-0 w-1 rounded-r-full',
          wtTone(props.tone).bar
        )
      "
    />

    <header
      class="flex flex-wrap items-center gap-2 px-3 pt-2.5 pb-2 pl-4"
      @click="doc.select({ kind: 'segment', key: props.segment.key })"
    >
      <span
        v-if="!doc.readonly.value"
        draggable="true"
        class="text-default-300 hover:text-default-500 -ml-1 cursor-grab active:cursor-grabbing"
        aria-hidden="true"
        @dragstart="onHeaderDragStart"
        @dragend="segmentDrag.end"
      >
        <KunIcon name="lucide:grip-vertical" class="size-4" />
      </span>
      <WalkthroughsEditorNodePicker
        :model-value="props.segment.from"
        side="from"
        @update:model-value="setFrom"
      />
      <KunIcon
        name="lucide:arrow-right"
        class="text-default-300 size-4 shrink-0"
      />
      <WalkthroughsEditorNodePicker
        :model-value="props.segment.to"
        side="to"
        @update:model-value="setTo"
      />

      <span class="flex-1" />

      <button
        type="button"
        tabindex="-1"
        :class="
          cn(
            'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs transition-colors',
            props.segment.condition
              ? 'bg-info-50 text-info-700'
              : 'text-default-400 hover:bg-default-100 hover:text-default-600'
          )
        "
        @click.stop="conditionOpen = !conditionOpen"
      >
        <KunIcon name="lucide:key-round" class="size-3.5" />
        前提
      </button>

      <KunSelect
        :model-value="props.segment.section ?? ''"
        :options="sectionOptions"
        size="sm"
        :full-width="false"
        popup-width="auto"
        aria-label="所属分节"
        :disabled="doc.readonly.value"
        class-name="w-36"
        @update:model-value="setSection(String($event ?? ''))"
      />

      <KunDropdown :items="menu" position="bottom-end" @select="onMenu">
        <template #trigger>
          <KunButton
            variant="light"
            size="sm"
            is-icon-only
            aria-label="路线段操作"
            :disabled="doc.readonly.value"
          >
            <KunIcon name="lucide:ellipsis" class="size-4" />
          </KunButton>
        </template>
      </KunDropdown>
    </header>

    <div v-if="conditionOpen" class="space-y-1.5 px-4 pb-2">
      <KunInput
        :model-value="props.segment.condition ?? ''"
        size="sm"
        placeholder="走这段路的前提，例如「通关寧々线后」"
        :disabled="doc.readonly.value"
        @update:model-value="
          update(
            (s) => (s.condition = String($event) || null),
            `cond:${props.segment.key}`
          )
        "
        @blur="doc.breakCoalescing"
      />
      <KunInput
        v-if="props.translating"
        :model-value="props.segment.condition_zh ?? ''"
        size="sm"
        placeholder="前提的中文译文"
        :disabled="doc.readonly.value"
        @update:model-value="
          update(
            (s) => (s.condition_zh = String($event) || null),
            `cond-zh:${props.segment.key}`
          )
        "
        @blur="doc.breakCoalescing"
      />
    </div>

    <ol class="px-2 pb-1">
      <WalkthroughsEditorStep
        v-for="(step, i) in props.segment.steps"
        :key="i"
        :segment-key="props.segment.key"
        :index="i"
        :step="step"
        :translating="props.translating"
      />
    </ol>

    <ul
      v-if="issues.length"
      class="text-warning-700 space-y-0.5 px-4 pb-2 text-xs"
    >
      <li
        v-for="message in issues"
        :key="message"
        class="flex items-center gap-1"
      >
        <KunIcon name="lucide:circle-alert" class="size-3.5 shrink-0" />
        {{ message }}
      </li>
    </ul>

    <footer
      v-if="!doc.readonly.value"
      class="text-default-400 flex items-center gap-1 px-3 pb-2.5 pl-4 text-xs"
    >
      <button
        type="button"
        tabindex="-1"
        class="hover:bg-default-100 hover:text-default-700 inline-flex items-center gap-1 rounded-md px-2 py-1"
        @click="addStep('choice')"
      >
        <KunIcon name="lucide:plus" class="size-3.5" />
        选项
      </button>
      <button
        type="button"
        tabindex="-1"
        class="hover:bg-default-100 hover:text-default-700 inline-flex items-center gap-1 rounded-md px-2 py-1"
        @click="addStep('action')"
      >
        <KunIcon name="lucide:plus" class="size-3.5" />
        操作
      </button>
      <button
        type="button"
        tabindex="-1"
        class="hover:bg-default-100 hover:text-default-700 inline-flex items-center gap-1 rounded-md px-2 py-1"
        @click="addStep('save')"
      >
        <KunIcon name="lucide:save" class="size-3.5" />
        在这里存档
      </button>
      <span class="flex-1" />
      <span class="tabular-nums">{{ props.segment.steps.length }} 步</span>
    </footer>
  </section>
</template>
