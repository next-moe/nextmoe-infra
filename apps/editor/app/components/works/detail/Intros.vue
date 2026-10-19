<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'
import { LANGUAGE_LABELS } from '~/constants/catalog'

const props = defineProps<{ work: CatalogWork }>()

const intros = computed(() => props.work.intros ?? [])
</script>

<template>
  <div v-if="intros.length" class="space-y-4">
    <WorksDetailSection
      v-for="(intro, index) in intros"
      :key="`${intro.lang}-${intro.source}-${index}`"
      :title="labelOf(LANGUAGE_LABELS, intro.lang) || '未标注语言'"
    >
      <div
        class="text-default-400 -mt-2 mb-3 flex flex-wrap items-center gap-1.5 text-xs"
      >
        <span>来源：{{ intro.source }}</span>
        <KunChip
          v-if="intro.is_machine"
          size="xs"
          variant="flat"
          color="warning"
          >机翻</KunChip
        >
      </div>
      <p class="text-sm leading-relaxed whitespace-pre-line">
        {{ intro.value }}
      </p>
    </WorksDetailSection>
  </div>
  <KunNull v-else description="暂无简介" />
</template>
