<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogMergePreviewField } from '#shared/types/catalog'
import { LANGUAGE_LABELS } from '~/constants/catalog'
import { EDIT_LANGUAGE_LABELS, vocabLabel } from '~/constants/vocabLabels'
import { MERGE_FIELD_LABELS } from '~/constants/merge'
import { chosenValue, fixedSide, type MergeSide } from '~/utils/merge/plan'

const workbench = useMergeWorkbench()
const { preview, choices, canEditChoices } = workbench

const isEmpty = (value: unknown) =>
  value === null || value === undefined || value === ''

const text = (key: string, value: unknown) => {
  if (isEmpty(value)) {
    return ''
  }
  if (key === 'olang' || key === 'lang') {
    const tag = String(value)
    return EDIT_LANGUAGE_LABELS[tag] ?? LANGUAGE_LABELS[tag] ?? tag
  }
  if (key === 'gender') {
    return vocabLabel('gender', String(value))
  }
  if (key === 'birth' && typeof value === 'object') {
    const { y, m, d } = value as Record<string, number | null>
    return [y ?? '????', m, d].filter((part) => part != null).join('-')
  }
  if (key === 'primary_credit_name_id') {
    return `#${value}`
  }
  return String(value)
}
const isImage = (key: string) => key === 'image_hash' || key === 'logo_hash'
const sideValue = (field: CatalogMergePreviewField, side: MergeSide) =>
  side === 'source' ? field.source_value : field.target_value

const lockReason = (field: CatalogMergePreviewField, side: MergeSide) => {
  const pinned = fixedSide(field)
  if (!pinned || pinned === side) {
    return ''
  }
  if (isEmpty(field.target_value)) {
    return '保留方没有这一项，合并后自动采用被并入方的值'
  }
  if (field.human_protected) {
    return '保留方的值是人工填写的，不会被资料源导入的值替换'
  }
  return '被并入方没有这一项'
}

const pick = (field: CatalogMergePreviewField, side: MergeSide) => {
  if (!lockReason(field, side)) {
    workbench.choose(field.key, side)
  }
}
</script>

<template>
  <MergeWorkbenchSection v-if="preview" section-key="fields">
    <div class="space-y-4">
      <div v-for="field in preview.fields" :key="field.key" class="space-y-1.5">
        <p class="text-default-600 text-sm font-medium">
          {{ MERGE_FIELD_LABELS[field.key] ?? field.key }}
        </p>
        <div class="grid gap-3 md:grid-cols-3">
          <MergeWorkbenchCell
            v-for="side in ['source', 'target'] as const"
            :key="side"
            :side="side"
            plain
          >
            <button
              type="button"
              :disabled="!canEditChoices || !!lockReason(field, side)"
              :aria-pressed="choices.fields[field.key] === side"
              :class="
                cn(
                  'flex h-full w-full items-start gap-3 rounded-xl border p-3 text-left transition-colors',
                  choices.fields[field.key] === side
                    ? 'border-primary-400 bg-primary-50'
                    : 'border-default-200 bg-content1',
                  canEditChoices &&
                    !lockReason(field, side) &&
                    choices.fields[field.key] !== side &&
                    'hover:border-primary-300 hover:bg-default-50 cursor-pointer',
                  !!lockReason(field, side) && 'opacity-70'
                )
              "
              @click="pick(field, side)"
            >
              <span
                :class="
                  cn(
                    'mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border',
                    choices.fields[field.key] === side
                      ? 'border-primary-500 bg-primary-500 text-white'
                      : 'border-default-300'
                  )
                "
              >
                <KunIcon
                  v-if="choices.fields[field.key] === side"
                  name="lucide:check"
                  class="size-3"
                />
              </span>
              <span class="min-w-0 flex-1 space-y-1">
                <span
                  v-if="isImage(field.key) && !isEmpty(sideValue(field, side))"
                  class="block size-16 overflow-hidden rounded-lg"
                >
                  <KunImage
                    :src="imageUrl(sideValue(field, side))"
                    alt=""
                    aspect-ratio="1 / 1"
                    object-fit="cover"
                    class-name="size-16"
                  />
                </span>
                <span
                  v-else
                  :class="
                    cn(
                      'block text-sm break-words',
                      isEmpty(sideValue(field, side)) && 'text-default-400'
                    )
                  "
                >
                  {{ text(field.key, sideValue(field, side)) || '（空）' }}
                </span>
                <span
                  v-if="lockReason(field, side)"
                  class="text-default-500 block text-xs"
                >
                  {{ lockReason(field, side) }}
                </span>
              </span>
            </button>
          </MergeWorkbenchCell>
          <MergeWorkbenchCell side="result">
            <div class="flex items-start gap-2 p-3">
              <KunIcon
                name="lucide:arrow-right"
                class="text-primary-500 mt-0.5 hidden size-4 shrink-0 md:block"
              />
              <span
                v-if="
                  isImage(field.key) && !isEmpty(chosenValue(field, choices))
                "
                class="block size-16 overflow-hidden rounded-lg"
              >
                <KunImage
                  :src="imageUrl(chosenValue(field, choices))"
                  alt=""
                  aspect-ratio="1 / 1"
                  object-fit="cover"
                  class-name="size-16"
                />
              </span>
              <span v-else class="min-w-0 text-sm font-medium break-words">
                {{ text(field.key, chosenValue(field, choices)) || '（空）' }}
              </span>
            </div>
          </MergeWorkbenchCell>
        </div>
      </div>
    </div>
  </MergeWorkbenchSection>
</template>
