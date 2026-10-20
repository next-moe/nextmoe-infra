<script setup lang="ts">
import type { EditRefFamily } from '#shared/types/edit'
import { vocabLabel } from '~/constants/vocabLabels'

const props = defineProps<{ fieldKey: string }>()

const { context, field, value } = useEditorField(() => props.fieldKey)
const { rowText, scalarText, refsInValue } = useRowLabel()

type Row = Record<string, unknown>

const control = computed(() =>
  field.value
    ? resolveEditControl(field.value, context.config.fields[props.fieldKey])
    : 'readonly'
)
const list = computed<unknown[]>(() =>
  Array.isArray(value.value) ? value.value : []
)
const isImageList = computed(() =>
  (field.value?.element?.members ?? []).some((m) => m.key === 'image_hash')
)
const imageOf = (hash: unknown) =>
  typeof hash === 'string' && hash
    ? (context.images.value[hash] ?? { url: imageUrl(hash), thumbhash: null })
    : null

const dateText = computed(() => {
  const date = value.value as { y?: number; m?: number; d?: number } | null
  if (!date || !date.y) {
    return '（空）'
  }
  return [date.y, date.m, date.d]
    .filter((part) => part !== undefined)
    .map((part, i) => (i === 0 ? String(part) : String(part).padStart(2, '0')))
    .join('-')
})

watch(
  value,
  (next) => {
    const byFamily = new Map<EditRefFamily, string[]>()
    for (const ref of refsInValue(props.fieldKey, next)) {
      byFamily.set(ref.family, [...(byFamily.get(ref.family) ?? []), ref.id])
    }
    const family = context.config.fields[props.fieldKey]?.ref
    if (field.value?.field_type === 'ref' && family && next != null) {
      byFamily.set(family, [...(byFamily.get(family) ?? []), String(next)])
    }
    for (const [refFamily, ids] of byFamily) {
      context.refs.resolve(refFamily, ids)
    }
  },
  { immediate: true }
)
</script>

<template>
  <EditFieldShell v-if="field" :field-key="props.fieldKey">
    <div v-if="control === 'image'">
      <div
        class="bg-default-100 border-default-200 w-40 overflow-hidden rounded-lg border"
      >
        <KunImage
          v-if="imageOf(value)"
          :src="imageOf(value)!.url"
          :thumbhash="imageOf(value)!.thumbhash ?? undefined"
          alt=""
          aspect-ratio="1 / 1"
          object-fit="contain"
        />
        <div
          v-else
          class="text-default-400 flex aspect-square items-center justify-center text-xs"
        >
          暂无图片
        </div>
      </div>
    </div>

    <p v-else-if="control === 'date'" class="text-sm tabular-nums">
      {{ dateText }}
    </p>

    <div
      v-else-if="field.field_type === 'list' && isImageList && list.length"
      class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4"
    >
      <figure
        v-for="(row, index) in list as Row[]"
        :key="index"
        class="border-default-200 overflow-hidden rounded-lg border"
      >
        <KunImage
          v-if="imageOf(row.image_hash)"
          :src="imageOf(row.image_hash)!.url"
          alt=""
          aspect-ratio="4 / 3"
          object-fit="contain"
          class-name="bg-default-100"
        />
        <figcaption class="text-default-500 truncate px-2 py-1 text-xs">
          {{ rowText(props.fieldKey, row) }}
        </figcaption>
      </figure>
    </div>

    <div v-else-if="control === 'intros' && list.length" class="space-y-3">
      <div v-for="(row, index) in list as Row[]" :key="index">
        <p class="text-default-500 mb-1 text-xs">
          {{ vocabLabel('intro_lang', String(row.lang ?? '')) }}
        </p>
        <p class="text-sm leading-6 whitespace-pre-line">{{ row.intro }}</p>
      </div>
    </div>

    <ul
      v-else-if="field.field_type === 'list' && list.length"
      class="divide-default-100 divide-y text-sm"
    >
      <li v-for="(row, index) in list" :key="index" class="py-1.5 break-all">
        {{ rowText(props.fieldKey, row) }}
      </li>
    </ul>

    <p v-else-if="field.field_type === 'list'" class="text-default-400 text-sm">
      没有人工维护的条目。
    </p>

    <p v-else class="text-sm break-all whitespace-pre-line">
      {{ scalarText(field, value) }}
    </p>

    <EditUpstream
      v-if="field.field_type === 'list'"
      :field-key="props.fieldKey"
      :images="isImageList"
    />
  </EditFieldShell>
</template>
