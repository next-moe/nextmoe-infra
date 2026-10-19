<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { EditRefFamily, EditUpstreamRow } from '#shared/types/edit'

const props = withDefaults(
  defineProps<{
    fieldKey: string
    title?: string
    exclude?: string[]
    images?: boolean
  }>(),
  { title: '', exclude: () => [], images: false }
)

const { context, config, companion, suppressed, disabled, toggleSuppressed } =
  useEditorField(() => props.fieldKey)
const refs = context.refs

const heading = computed(
  () => props.title || config.value?.upstreamTitle || '资料源导入'
)

const rows = computed<EditUpstreamRow[]>(() => {
  const excluded = new Set(props.exclude)
  return (context.upstreamRows.value[props.fieldKey] ?? []).filter(
    (row) => !row.match || !excluded.has(row.match)
  )
})

const canSuppress = (row: EditUpstreamRow) =>
  Boolean(companion.value && row.identity) && !disabled.value
const isSuppressed = (row: EditUpstreamRow) =>
  Boolean(row.identity && suppressed.value.includes(row.identity))

const labelOfRow = (row: EditUpstreamRow) =>
  row.ref ? refs.nameOf(row.ref.family, row.ref.id) || row.label : row.label

const open = ref(true)

watch(
  rows,
  (list) => {
    const byFamily = new Map<EditRefFamily, string[]>()
    const add = (family: EditRefFamily, id: string) =>
      byFamily.set(family, [...(byFamily.get(family) ?? []), id])
    for (const row of list) {
      if (row.ref) {
        add(row.ref.family, row.ref.id)
      }
      if (row.characterId) {
        add('character', row.characterId)
      }
    }
    for (const [family, ids] of byFamily) {
      refs.resolve(family, ids)
    }
  },
  { immediate: true }
)
</script>

<template>
  <div v-if="rows.length" class="border-default-200 mt-4 rounded-lg border">
    <button
      type="button"
      class="text-default-600 hover:bg-default-50 flex w-full items-center justify-between gap-2 rounded-lg px-3 py-2 text-left text-sm"
      :aria-expanded="open"
      @click="open = !open"
    >
      <span class="inline-flex items-center gap-1.5">
        <KunIcon name="lucide:database" class="size-4" />
        {{ heading }}
        <span class="text-default-400 tabular-nums">{{ rows.length }}</span>
      </span>
      <KunIcon
        :name="open ? 'lucide:chevron-up' : 'lucide:chevron-down'"
        class="size-4"
      />
    </button>
    <div v-if="open" class="border-default-200 border-t px-3 py-2">
      <p v-if="!companion" class="text-default-400 mb-2 text-xs">
        资料源导入的条目暂时只能查看，不能在这里隐藏。
      </p>
      <div
        v-if="props.images"
        class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4"
      >
        <figure
          v-for="row in rows"
          :key="row.key"
          :class="
            cn(
              'border-default-200 relative overflow-hidden rounded-lg border',
              isSuppressed(row) && 'opacity-40'
            )
          "
        >
          <KunImage
            v-if="row.imageUrl"
            :src="row.imageUrl"
            :thumbhash="row.thumbhash ?? undefined"
            :alt="row.label"
            aspect-ratio="4 / 3"
            object-fit="contain"
            class-name="bg-default-100"
          />
          <figcaption
            class="flex items-center justify-between gap-1 px-2 py-1 text-xs"
          >
            <span class="text-default-500 truncate">
              {{
                [row.source, row.label, row.detail].filter(Boolean).join(' · ')
              }}
            </span>
            <KunButton
              v-if="canSuppress(row)"
              size="xs"
              variant="light"
              :color="isSuppressed(row) ? 'primary' : 'danger'"
              @click="toggleSuppressed(row.identity!)"
            >
              {{ isSuppressed(row) ? '恢复' : '隐藏' }}
            </KunButton>
          </figcaption>
        </figure>
      </div>
      <ul v-else class="divide-default-100 divide-y">
        <li
          v-for="row in rows"
          :key="row.key"
          class="flex items-center gap-2 py-1.5 text-sm"
        >
          <div
            :class="
              cn(
                'min-w-0 flex-1',
                isSuppressed(row) && 'text-default-400 line-through'
              )
            "
          >
            <span class="break-all">{{ labelOfRow(row) }}</span>
            <span v-if="row.characterId" class="text-default-500 ml-1 text-xs">
              （{{
                refs.nameOf('character', row.characterId) ||
                `角色 #${row.characterId}`
              }}）
            </span>
            <span v-if="row.detail" class="text-default-400 ml-2 text-xs">
              {{ row.detail }}
            </span>
          </div>
          <KunChip v-if="row.source" size="xs" variant="flat">
            {{ row.source }}
          </KunChip>
          <KunButton
            v-if="canSuppress(row)"
            size="xs"
            variant="light"
            :color="isSuppressed(row) ? 'primary' : 'danger'"
            @click="toggleSuppressed(row.identity!)"
          >
            {{ isSuppressed(row) ? '恢复' : '隐藏' }}
          </KunButton>
        </li>
      </ul>
    </div>
  </div>
</template>
