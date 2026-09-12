<script setup lang="ts">
import {
  NEWS_ATTEMPT_CEILING,
  NEWS_DEGRADED_LABELS,
  TIER0_COLORS,
  TIER0_LABELS
} from '~/constants/news'
import type { NewsAdminItem } from '~~/shared/types/news'

const props = defineProps<{ item: NewsAdminItem }>()

const verdict = computed(() => props.item.verdict)
const exhausted = computed(
  () => props.item.attempts >= NEWS_ATTEMPT_CEILING && !!verdict.value?.degraded
)
</script>

<template>
  <div class="flex flex-wrap items-center gap-1">
    <KunChip v-if="!verdict" color="default" variant="flat" size="xs">
      未评分
    </KunChip>

    <template v-else-if="verdict.degraded">
      <KunChip color="danger" variant="flat" size="xs">
        评分降级
        <template v-if="verdict.degraded_reason">
          ·
          {{
            NEWS_DEGRADED_LABELS[verdict.degraded_reason] ??
            verdict.degraded_reason
          }}
        </template>
      </KunChip>
      <KunChip v-if="exhausted" color="danger" variant="solid" size="xs">
        已达重试上限
      </KunChip>
    </template>

    <template v-else>
      <KunChip
        :color="TIER0_COLORS[verdict.tier0_decision] ?? 'default'"
        variant="flat"
        size="xs"
      >
        词表{{ TIER0_LABELS[verdict.tier0_decision] ?? verdict.tier0_decision }}
        <template v-if="verdict.tier0_matched?.length">
          · {{ verdict.tier0_matched.join('、') }}
        </template>
      </KunChip>
      <KunChip
        v-if="verdict.ai_flagged"
        color="danger"
        variant="flat"
        size="xs"
      >
        AI 标记
        <template v-if="verdict.ai_categories?.length">
          · {{ verdict.ai_categories.join('、') }}
        </template>
      </KunChip>
      <span v-if="verdict.ai_score != null" class="text-default-400 text-xs">
        {{ verdict.ai_score.toFixed(3) }}
      </span>
    </template>
  </div>
</template>
