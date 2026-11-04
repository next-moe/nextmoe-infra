<script setup lang="ts">
import { LLM_PRICE_FIELDS } from '~/constants/llm'
import type { LlmPriceDraft } from '~~/shared/types/llm'

defineProps<{ hint?: string }>()
const price = defineModel<LlmPriceDraft>({ required: true })
</script>

<template>
  <div class="border-default-200 space-y-2 rounded-lg border p-4">
    <p class="text-default-500 text-sm">
      价格(美元,可选;全部留空 = 不定价{{ hint ? `,${hint}` : '' }})
    </p>
    <div class="grid gap-3 sm:grid-cols-2">
      <KunInput
        v-for="f in LLM_PRICE_FIELDS"
        :key="f.key"
        v-model="price[f.key]"
        :label="f.label"
        type="number"
        min="0"
        step="0.0001"
        placeholder="0.00"
      />
    </div>
    <p class="text-default-400 text-xs">
      图片路由的「每次调用」按每张图计价;上游在 usage
      里报告的费用优先于这里的标价。
    </p>
  </div>
</template>
