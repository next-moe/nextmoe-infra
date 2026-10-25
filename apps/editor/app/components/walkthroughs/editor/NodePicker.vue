<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WtNode, WtNodeType } from '#shared/types/walkthrough'
import { WT_ENDING_LABEL, WT_NODE_LABEL, wtTone } from '~/constants/walkthrough'
import {
  characterTones,
  keysOf,
  nextKey,
  nodeLabel,
  sameNode
} from '~/utils/walkthrough/doc'

const props = defineProps<{
  modelValue: WtNode
  side: 'from' | 'to'
}>()

const emit = defineEmits<{ 'update:modelValue': [node: WtNode] }>()

const doc = useWalkthroughDocument()
const chart = doc.chart
const query = ref('')
const popover = ref<{ close: () => void } | null>(null)

const NODE_ICON: Record<WtNodeType, string> = {
  entry: 'lucide:flag',
  save: 'lucide:save',
  ending: 'lucide:trophy',
  collect: 'lucide:check-check',
  open: 'lucide:ellipsis'
}

const tones = computed(() => characterTones(chart.value))
const endingTone = (key: string | null) => {
  const ending = chart.value.endings.find((e) => e.key === key)
  const first = ending?.characters[0]
  return first ? (tones.value.get(first) ?? null) : null
}

const label = computed(() => nodeLabel(chart.value, props.modelValue))
const missing = computed(
  () =>
    ['entry', 'save', 'ending'].includes(props.modelValue.node_type) &&
    !(
      {
        entry: chart.value.entries,
        save: chart.value.saves,
        ending: chart.value.endings
      } as Record<string, { key: string }[]>
    )[props.modelValue.node_type]?.some((i) => i.key === props.modelValue.key)
)

const matches = (text: string) =>
  !query.value.trim() ||
  text.toLowerCase().includes(query.value.trim().toLowerCase())

const groups = computed(() => {
  const g: {
    type: WtNodeType
    title: string
    items: { node: WtNode; label: string; hint?: string }[]
  }[] = []
  if (props.side === 'from') {
    g.push({
      type: 'entry',
      title: '入口',
      items: chart.value.entries
        .filter((e) => matches(e.label))
        .map((e) => ({
          node: { node_type: 'entry', key: e.key },
          label: e.label || e.key
        }))
    })
    g.push({
      type: 'save',
      title: '存档',
      items: chart.value.saves
        .filter((s) => matches(s.label))
        .map((s) => ({
          node: { node_type: 'save', key: s.key },
          label: s.label || s.key
        }))
    })
  }
  g.push({
    type: 'ending',
    title: props.side === 'from' ? '结局之后（AFTER）' : '结局',
    items: chart.value.endings
      .filter((e) => matches(e.name))
      .map((e) => ({
        node: { node_type: 'ending', key: e.key },
        label: e.name || e.key,
        hint: WT_ENDING_LABEL[e.ending_type]
      }))
  })
  return g
})

const pick = (node: WtNode) => {
  emit('update:modelValue', node)
  popover.value?.close()
  query.value = ''
}

const create = (type: 'entry' | 'save' | 'ending') => {
  const name = query.value.trim()
  const prefix = { entry: 'e', save: 'v', ending: 'n' }[type]
  const pool = {
    entry: chart.value.entries,
    save: chart.value.saves,
    ending: chart.value.endings
  }[type]
  const key = nextKey(prefix, keysOf(pool))
  doc.edit((d) => {
    if (type === 'entry')
      d.entries.push({ key, label: name || 'START', unlocked_by: null })
    else if (type === 'save')
      d.saves.push({ key, label: name || `存档 ${d.saves.length + 1}` })
    else
      d.endings.push({
        key,
        name: name || `结局 ${d.endings.length + 1}`,
        name_zh: null,
        ending_type: 'other',
        characters: []
      })
  })
  pick({ node_type: type, key })
}

const tone = computed(() =>
  props.modelValue.node_type === 'ending'
    ? endingTone(props.modelValue.key)
    : null
)
</script>

<template>
  <KunPopover
    ref="popover"
    position="bottom-start"
    :aria-label="props.side === 'from' ? '选择起点' : '选择终点'"
    inner-class="w-80"
  >
    <template #trigger>
      <button
        type="button"
        tabindex="-1"
        :disabled="doc.readonly.value"
        :class="
          cn(
            'inline-flex max-w-64 items-center gap-1.5 rounded-lg border px-2 py-1 text-sm transition-colors',
            missing
              ? 'border-danger-300 bg-danger-50 text-danger-700'
              : props.modelValue.node_type === 'ending'
                ? cn('bg-content1 hover:bg-default-50', wtTone(tone).border)
                : 'border-default-200 bg-default-50 hover:bg-default-100'
          )
        "
      >
        <KunIcon
          :name="NODE_ICON[props.modelValue.node_type]"
          class="text-default-500 size-3.5 shrink-0"
        />
        <span class="truncate">{{ missing ? '请选择' : label }}</span>
        <KunIcon
          name="lucide:chevrons-up-down"
          class="text-default-400 size-3 shrink-0"
        />
      </button>
    </template>

    <div class="space-y-2 p-2">
      <KunInput
        v-model="query"
        size="sm"
        placeholder="搜索，或输入名称后新建"
        autofocus
      />
      <div class="max-h-80 space-y-2 overflow-y-auto">
        <div v-for="group in groups" :key="group.type">
          <p class="text-default-400 px-2 pt-1 pb-0.5 text-xs">
            {{ group.title }}
          </p>
          <button
            v-for="item in group.items"
            :key="item.node.key ?? group.type"
            type="button"
            :class="
              cn(
                'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm',
                sameNode(item.node, props.modelValue)
                  ? 'bg-primary-50 text-primary-700'
                  : 'hover:bg-default-100'
              )
            "
            @click="pick(item.node)"
          >
            <KunIcon
              :name="NODE_ICON[group.type]"
              class="text-default-400 size-3.5 shrink-0"
            />
            <span class="min-w-0 flex-1 truncate">{{ item.label }}</span>
            <span v-if="item.hint" class="text-default-400 shrink-0 text-xs">{{
              item.hint
            }}</span>
          </button>
          <button
            type="button"
            class="text-primary-600 hover:bg-primary-50 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm"
            @click="create(group.type as 'entry' | 'save' | 'ending')"
          >
            <KunIcon name="lucide:plus" class="size-3.5 shrink-0" />
            新建{{ WT_NODE_LABEL[group.type]
            }}{{ query.trim() ? `「${query.trim()}」` : '' }}
          </button>
        </div>

        <div v-if="props.side === 'to'">
          <p class="text-default-400 px-2 pt-1 pb-0.5 text-xs">其他</p>
          <button
            v-for="type in ['collect', 'open'] as const"
            :key="type"
            type="button"
            :class="
              cn(
                'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm',
                props.modelValue.node_type === type
                  ? 'bg-primary-50 text-primary-700'
                  : 'hover:bg-default-100'
              )
            "
            @click="pick({ node_type: type, key: null })"
          >
            <KunIcon
              :name="NODE_ICON[type]"
              class="text-default-400 size-3.5 shrink-0"
            />
            {{ WT_NODE_LABEL[type] }}
          </button>
        </div>
      </div>
    </div>
  </KunPopover>
</template>
