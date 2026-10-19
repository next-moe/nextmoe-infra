<script setup lang="ts">
import type { CatalogSchemaField } from '#shared/types/catalog'
import type { DiffContext } from '~/utils/review/context'
import {
  MEMBER_LABELS,
  imageUrl,
  refCollectionOf,
  scalarText,
  vocabularyLabel
} from '~/utils/review/describe'

const props = defineProps<{
  field: CatalogSchemaField
  item: unknown
  context: DiffContext
  omit?: string[]
}>()

type Member = NonNullable<CatalogSchemaField['element']>['members'][number]

const memberText = (member: Member, raw: unknown) => {
  if (member.type === 'ref') {
    const collection = refCollectionOf(props.field.key, member.key)
    return raw === null || raw === undefined
      ? '—'
      : collection
        ? props.context.refName(collection, raw)
        : `#${String(raw)}`
  }
  if (member.vocabulary) {
    return vocabularyLabel(
      raw,
      props.context.vocabulary(member.vocabulary),
      member.type === 'int' ? 'int' : 'token',
      member.base
    )
  }
  return scalarText(raw)
}

const record = computed(() =>
  props.item && typeof props.item === 'object' && !Array.isArray(props.item)
    ? (props.item as Record<string, unknown>)
    : null
)

const image = computed(() => {
  const hashMember = props.field.element?.members.find(
    (member) => member.type === 'imagehash'
  )
  return hashMember && record.value
    ? imageUrl(record.value[hashMember.key])
    : ''
})

const parts = computed(() =>
  (props.field.element?.members ?? [])
    .filter(
      (member) =>
        member.type !== 'imagehash' && !props.omit?.includes(member.key)
    )
    .map((member) => ({
      key: member.key,
      label: MEMBER_LABELS[member.key] ?? member.key,
      text: memberText(member, record.value?.[member.key]),
      long:
        member.type === 'text' &&
        String(record.value?.[member.key] ?? '').length > 60
    }))
    .filter((part) => part.text !== '—' || part.key === 'title')
)

const refText = computed(() => {
  const collection = refCollectionOf(props.field.key)
  return collection
    ? props.context.refName(collection, props.item)
    : `#${String(props.item)}`
})
</script>

<template>
  <span v-if="props.field.element?.type === 'ref'" class="text-foreground">
    {{ refText }}
  </span>
  <span
    v-else-if="props.field.element?.type !== 'object' || !record"
    class="text-foreground break-all"
  >
    {{ scalarText(props.item) }}
  </span>
  <span v-else class="flex min-w-0 items-start gap-2">
    <span
      v-if="image"
      class="border-default-200 bg-default-100 size-12 shrink-0 overflow-hidden rounded border"
    >
      <KunImage :src="image" alt="" aspect-ratio="1 / 1" object-fit="cover" />
    </span>
    <span class="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-0.5">
      <span
        v-for="part in parts"
        :key="part.key"
        :class="part.long ? 'basis-full' : ''"
      >
        <span class="text-default-400 mr-1 text-xs">{{ part.label }}</span>
        <span class="text-foreground break-words whitespace-pre-wrap">{{
          part.text
        }}</span>
      </span>
    </span>
  </span>
</template>
