<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { LLM_BUDGET_WARN_RATIO } from '~/constants/llm'

const props = defineProps<{ spentNano: number; budgetMicro: number }>()

const ratio = computed(() => llmBudgetRatio(props.spentNano, props.budgetMicro))
const tone = computed(() =>
  ratio.value >= 1
    ? 'text-danger-600 font-medium'
    : ratio.value >= LLM_BUDGET_WARN_RATIO
      ? 'text-warning-600 font-medium'
      : 'text-default-500'
)
</script>

<template>
  <p :class="cn('text-xs whitespace-nowrap tabular-nums', tone)">
    今日 {{ llmNanoUsd(spentNano) }} /
    {{ budgetMicro > 0 ? `预算 ${llmUsdPerM(budgetMicro)}` : '不限' }}
  </p>
</template>
