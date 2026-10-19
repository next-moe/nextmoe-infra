<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogSchemaMember } from '#shared/types/edit'

const MEMBER_LABELS: Record<string, string> = {
  kind: '类型',
  portrait_pinned: '竖版主封面',
  sexual: '性暗示',
  violence: '暴力',
  caption: '说明'
}

const props = defineProps<{ fieldKey: string }>()

const { context, value, baseValue, disabled, field, issues } = useEditorField(
  () => props.fieldKey
)

type Row = Record<string, unknown>

const rows = computed<Row[]>(() =>
  Array.isArray(value.value) ? (value.value as Row[]) : []
)
const baseByHash = computed(
  () =>
    new Map(
      (Array.isArray(baseValue.value) ? (baseValue.value as Row[]) : []).map(
        (row) => [String(row.image_hash), row]
      )
    )
)
const members = computed<CatalogSchemaMember[]>(() =>
  (field.value?.element?.members ?? []).filter((m) => m.key !== 'image_hash')
)

const setMember = (index: number, key: string, next: unknown) => {
  value.value = rows.value.map((row, i) =>
    i === index ? { ...row, [key]: next } : row
  )
}

const changed = (row: Row) =>
  !sameValue(baseByHash.value.get(String(row.image_hash)), row)
const memberInvalid = (index: number, key: string) =>
  issues.value.some((issue) => issue.index === index && issue.member === key)
</script>

<template>
  <div>
    <p v-if="!rows.length" class="text-default-400 text-sm">
      这部作品没有资料源导入的图片。
    </p>
    <div v-else class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
      <article
        v-for="(row, index) in rows"
        :key="String(row.image_hash)"
        :class="
          cn(
            'border-default-200 overflow-hidden rounded-lg border',
            changed(row) && 'border-primary-300'
          )
        "
      >
        <div class="bg-default-100">
          <KunImage
            v-if="context.images.value[String(row.image_hash)]"
            :src="context.images.value[String(row.image_hash)]!.url"
            :thumbhash="
              context.images.value[String(row.image_hash)]!.thumbhash ??
              undefined
            "
            alt=""
            aspect-ratio="4 / 3"
            object-fit="contain"
          />
          <div
            v-else
            class="text-default-400 flex aspect-[4/3] items-center justify-center font-mono text-xs"
          >
            {{ String(row.image_hash).slice(0, 12) }}…
          </div>
        </div>
        <div class="grid grid-cols-2 gap-2 p-2">
          <div
            v-for="member in members"
            :key="member.key"
            :class="
              cn('min-w-0 space-y-1', member.type === 'text' && 'col-span-2')
            "
          >
            <span class="text-default-500 text-xs">
              {{ MEMBER_LABELS[member.key] ?? member.key }}
            </span>
            <EditMemberInput
              :member="member"
              :config="{ label: MEMBER_LABELS[member.key] ?? member.key }"
              :model-value="row[member.key]"
              :disabled="disabled"
              :invalid="memberInvalid(index, member.key)"
              @update:model-value="setMember(index, member.key, $event)"
            />
          </div>
        </div>
      </article>
    </div>
  </div>
</template>
