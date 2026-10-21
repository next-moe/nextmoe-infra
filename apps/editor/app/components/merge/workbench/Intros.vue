<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogMergePreviewIntro } from '#shared/types/catalog'
import { EDIT_LANGUAGE_LABELS } from '~/constants/vocabLabels'

const { preview } = useMergeWorkbench()

const intros = computed(() => preview.value?.intros ?? [])
const survivor = (intro: CatalogMergePreviewIntro) =>
  intro.fate === 'moves' || intro.fate === 'replaces_machine'
    ? intro.source_text
    : intro.target_text
</script>

<template>
  <MergeWorkbenchSection v-if="preview" section-key="intros">
    <p v-if="!intros.length" class="text-default-400 text-sm">
      两边都没有简介。
    </p>
    <div v-for="intro in intros" :key="intro.lang" class="space-y-1.5">
      <p class="text-default-600 flex items-center gap-2 text-sm font-medium">
        {{ EDIT_LANGUAGE_LABELS[intro.lang] ?? intro.lang }}
        <MergeWorkbenchFate v-if="intro.source_has" :fate="intro.fate" />
      </p>
      <div class="grid gap-3 md:grid-cols-3">
        <MergeWorkbenchCell side="source">
          <p
            :class="
              cn(
                'line-clamp-5 px-3 py-2.5 text-sm leading-6 whitespace-pre-line',
                !intro.source_has && 'text-default-400',
                intro.source_has &&
                  intro.fate === 'kept_out' &&
                  'text-default-400'
              )
            "
          >
            {{ intro.source_has ? intro.source_text : '（无）' }}
          </p>
        </MergeWorkbenchCell>
        <MergeWorkbenchCell side="target">
          <p
            :class="
              cn(
                'line-clamp-5 px-3 py-2.5 text-sm leading-6 whitespace-pre-line',
                !intro.target_has && 'text-default-400'
              )
            "
          >
            {{ intro.target_has ? intro.target_text : '（无）' }}
          </p>
        </MergeWorkbenchCell>
        <MergeWorkbenchCell side="result">
          <p
            class="line-clamp-5 px-3 py-2.5 text-sm leading-6 whitespace-pre-line"
          >
            {{ survivor(intro) || '（无）' }}
          </p>
        </MergeWorkbenchCell>
      </div>
    </div>
  </MergeWorkbenchSection>
</template>
