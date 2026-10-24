<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { EditRefFamily } from '#shared/types/edit'
import { ENTITY_META } from '~/constants/editEntities'
import { SITE_LABELS } from '~/constants/catalog'

const props = defineProps<{
  side: 'source' | 'target'
  entityId: string | null
  editable: boolean
}>()
const emit = defineEmits<{ pick: [id: string | null] }>()

const workbench = useMergeWorkbench()
const { preview, candidates, family, request } = workbench
const refs = useRefNames()

const meta = computed(() => ENTITY_META[family.value])
const other = computed(() =>
  props.side === 'source' ? workbench.targetId.value : workbench.sourceId.value
)
const info = computed(() => (preview.value ? preview.value[props.side] : null))
const recordName = computed(() =>
  request.value
    ? props.side === 'source'
      ? request.value.source_name
      : request.value.target_name
    : ''
)
const name = computed(
  () =>
    recordName.value ||
    info.value?.name ||
    (props.entityId
      ? refs.nameOf(family.value as EditRefFamily, props.entityId)
      : '') ||
    (props.entityId ? `#${props.entityId}` : '')
)
watch(
  () => props.entityId,
  (id) => {
    if (id) {
      refs.resolve(family.value as EditRefFamily, [id])
    }
  },
  { immediate: true }
)

const picked = ref<string | null>(null)
watch(picked, (id) => {
  if (id) {
    emit('pick', id)
    picked.value = null
  }
})

const TONE = {
  source: {
    dot: 'bg-warning-500',
    label: '被并入',
    hint: '合并后编号跳转到保留的一方'
  },
  target: {
    dot: 'bg-success-500',
    label: '保留',
    hint: '编号与资料保留，另一方并入这里'
  }
}
const tone = computed(() => TONE[props.side])

watch(
  candidates,
  (list) => {
    if (list.length) {
      refs.resolve(
        family.value as EditRefFamily,
        list.map((candidate) => candidate.id)
      )
    }
  },
  { immediate: true }
)

const REASONS: Record<string, string> = {
  queue: '查重候选',
  same_name: '同名',
  similar_name: '名称相近'
}
</script>

<template>
  <div
    :class="
      cn(
        'border-default-200 bg-content1 flex min-w-0 flex-col gap-3 rounded-xl border p-4',
        info && !info.live && 'border-danger-200 bg-danger-50'
      )
    "
  >
    <div class="flex items-center justify-between gap-2">
      <p class="flex items-center gap-2 text-sm">
        <span :class="cn('size-2 rounded-full', tone.dot)" />
        <span class="font-medium">{{ tone.label }}</span>
        <span class="text-default-400 hidden text-xs lg:inline">
          {{ tone.hint }}
        </span>
      </p>
      <KunButton
        v-if="props.editable && props.entityId"
        variant="light"
        size="xs"
        @click="emit('pick', null)"
      >
        更换
      </KunButton>
    </div>

    <template v-if="props.entityId">
      <div class="min-w-0">
        <a
          :href="`${meta.path}/${props.entityId}`"
          target="_blank"
          rel="noopener"
          class="hover:text-primary line-clamp-2 text-lg leading-7 font-semibold break-words"
        >
          {{ name }}
        </a>
        <div
          class="text-default-500 mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm"
        >
          <span class="inline-flex items-center gap-1">
            {{ meta.label }}
            <KunCopy :text="props.entityId" size="xs" variant="light" />
          </span>
          <span v-if="info?.site" class="inline-flex items-center gap-1">
            <KunIcon name="lucide:radio-tower" class="size-3.5" />
            {{ SITE_LABELS[info.site] ?? info.site }} 已认领
          </span>
          <span
            v-if="props.side === 'source' && request?.state === 'executed'"
            class="text-warning-700 inline-flex items-center gap-1"
          >
            <KunIcon name="lucide:corner-up-right" class="size-3.5" />
            已并入 #{{ request.target_id }}，编号会跳转
          </span>
          <span
            v-if="info && !info.live"
            class="text-danger-600 inline-flex items-center gap-1"
          >
            <KunIcon name="lucide:circle-x" class="size-3.5" />
            已被合并或删除
          </span>
        </div>
      </div>
    </template>

    <template v-else-if="props.editable">
      <EditRefPicker
        v-model="picked"
        :family="family as EditRefFamily"
        :exclude="other ? [other] : []"
        :aria-label="`选择${tone.label}的${meta.label}`"
        :placeholder="`搜索要${props.side === 'source' ? '并入' : '保留'}的${meta.label}`"
      />
      <div v-if="candidates.length" class="space-y-1.5">
        <p class="text-default-500 text-xs">可能重复的条目</p>
        <ul class="space-y-1">
          <li v-for="candidate in candidates" :key="candidate.id">
            <button
              type="button"
              class="hover:bg-default-100 flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm transition-colors"
              @click="emit('pick', candidate.id)"
            >
              <KunIcon name="lucide:copy" class="text-default-400 size-4" />
              <span class="min-w-0 flex-1 truncate">{{
                refs.nameOf(family as EditRefFamily, candidate.id) ||
                candidate.name
              }}</span>
              <span class="text-default-400 text-xs">
                #{{ candidate.id }} · {{ REASONS[candidate.reason] }}
              </span>
            </button>
          </li>
        </ul>
      </div>
    </template>
    <p v-else class="text-default-400 text-sm">未选择</p>
  </div>
</template>
