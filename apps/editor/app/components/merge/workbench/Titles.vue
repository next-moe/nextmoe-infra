<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogMergePreviewTitle } from '#shared/types/catalog'
import { EDIT_LANGUAGE_LABELS, vocabLabel } from '~/constants/vocabLabels'
import { mergedTitles } from '~/utils/merge/plan'

const { preview } = useMergeWorkbench()

const titles = computed(() => preview.value?.titles ?? [])
const bySide = (side: 'source' | 'target') =>
  titles.value.filter((title) => title.side === side)
const result = computed(() => mergedTitles(titles.value))

const langText = (lang: string) =>
  lang ? (EDIT_LANGUAGE_LABELS[lang] ?? lang) : '不分语言'
const muted = (title: CatalogMergePreviewTitle) =>
  title.side === 'source' &&
  (title.fate === 'duplicate' || title.fate === 'dropped')
</script>

<template>
  <MergeWorkbenchSection v-if="preview" section-key="titles">
    <div class="grid gap-3 md:grid-cols-3">
      <MergeWorkbenchCell
        v-for="side in ['source', 'target'] as const"
        :key="side"
        :side="side"
      >
        <ul class="divide-default-100 divide-y px-3 py-1">
          <li
            v-for="(title, index) in bySide(side)"
            :key="`${side}-${index}`"
            class="flex items-start gap-2 py-2"
          >
            <span class="min-w-0 flex-1">
              <span
                :class="
                  cn(
                    'block text-sm break-words',
                    muted(title) && 'text-default-400 line-through'
                  )
                "
              >
                {{ title.title }}
              </span>
              <span class="text-default-400 text-xs">
                {{ langText(title.lang) }} ·
                {{ vocabLabel('title_kind', title.title_kind) }}
              </span>
            </span>
            <MergeWorkbenchFate v-if="side === 'source'" :fate="title.fate" />
          </li>
          <li v-if="!bySide(side).length" class="text-default-400 py-3 text-sm">
            没有标题
          </li>
        </ul>
      </MergeWorkbenchCell>
      <MergeWorkbenchCell side="result">
        <ul class="divide-primary-100 divide-y px-3 py-1">
          <li
            v-for="(title, index) in result"
            :key="`result-${index}`"
            class="flex items-start gap-2 py-2"
          >
            <span
              v-if="title.side === 'source'"
              class="bg-success-500 mt-2 size-1.5 shrink-0 rounded-full"
              aria-label="来自被并入方"
            />
            <span class="min-w-0 flex-1">
              <span class="block text-sm break-words">{{ title.title }}</span>
              <span class="text-default-400 text-xs">
                {{ langText(title.lang) }} ·
                {{ vocabLabel('title_kind', title.title_kind) }}
              </span>
            </span>
          </li>
        </ul>
      </MergeWorkbenchCell>
    </div>
  </MergeWorkbenchSection>
</template>
