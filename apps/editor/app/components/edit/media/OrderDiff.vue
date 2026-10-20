<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import {
  MEDIA_FIELDS,
  applyOrder,
  asHashList,
  contextMediaRows
} from '~/utils/mediaGallery'

const props = defineProps<{
  fieldKey: string
  before: unknown
  after: unknown
}>()

const context = useEditorContext()
const session = useWorkspaceSession()

const keys = computed(() =>
  props.fieldKey === MEDIA_FIELDS.screenshot.order
    ? MEDIA_FIELDS.screenshot
    : MEDIA_FIELDS.cover
)
const natural = computed(() => {
  const entity = session.open('work', context.entityId.value)
  const field = entity.workspace.editContext.context.value?.fields?.find(
    (entry) => entry.key === keys.value.human
  )
  return contextMediaRows(field).map((row) => row.hash)
})

const beforeList = computed(() =>
  applyOrder(natural.value, asHashList(props.before))
)
const afterList = computed(() => asHashList(props.after))
const reset = computed(() => afterList.value.length === 0)
const moved = (hash: string, index: number) =>
  beforeList.value.indexOf(hash) !== index

const urlOf = (hash: string) =>
  context.images.value[hash]?.url ?? imageUrl(hash)
</script>

<template>
  <p v-if="reset" class="text-default-600 text-sm">恢复资料源的默认顺序</p>
  <div v-else class="space-y-2">
    <div
      v-for="strip in [
        { label: '修改前', list: beforeList, after: false },
        { label: '修改后', list: afterList, after: true }
      ]"
      :key="strip.label"
      class="space-y-1"
    >
      <p class="text-default-500 text-xs">{{ strip.label }}</p>
      <ol class="flex flex-wrap gap-1.5">
        <li
          v-for="(hash, index) in strip.list"
          :key="hash"
          :class="
            cn(
              'bg-default-100 relative size-12 overflow-hidden rounded-md border',
              strip.after && moved(hash, index)
                ? 'border-primary-500 ring-primary-300 ring-1'
                : 'border-default-200'
            )
          "
        >
          <img
            :src="urlOf(hash)"
            alt=""
            loading="lazy"
            class="size-full object-cover"
          />
          <span
            class="bg-background/85 absolute top-0.5 left-0.5 rounded px-1 text-[10px] leading-4 tabular-nums"
          >
            {{ index + 1 }}
          </span>
        </li>
      </ol>
    </div>
  </div>
</template>
