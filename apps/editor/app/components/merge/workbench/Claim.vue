<script setup lang="ts">
import { SITE_LABELS } from '~/constants/catalog'

const { preview } = useMergeWorkbench()

const site = (key: string | null | undefined) =>
  key ? (SITE_LABELS[key] ?? key) : ''

const claimText = computed(() => {
  const claim = preview.value?.claim
  if (!claim || claim.fate === 'none') {
    return '被并入的作品没有站点认领，认领情况不变。'
  }
  if (claim.fate === 'moves') {
    return `被并入方的认领（${site(claim.source_site)}）转到保留方。`
  }
  return `两边都被认领：保留方的认领（${site(claim.target_site)}）不变；${site(claim.source_site)} 上被并入作品的页面以后跟随跳转。`
})
</script>

<template>
  <MergeWorkbenchSection v-if="preview" section-key="claim">
    <div class="grid gap-3 md:grid-cols-2">
      <div
        class="border-default-200 bg-content1 space-y-1.5 rounded-xl border p-4"
      >
        <p class="flex items-center gap-2 text-sm font-medium">
          <KunIcon name="lucide:radio-tower" class="text-default-500 size-4" />
          站点认领
        </p>
        <p class="text-default-600 text-sm">{{ claimText }}</p>
      </div>
      <div
        class="border-default-200 bg-content1 space-y-1.5 rounded-xl border p-4"
      >
        <p class="flex items-center gap-2 text-sm font-medium">
          <KunIcon
            name="lucide:corner-up-right"
            class="text-default-500 size-4"
          />
          编号与下游
        </p>
        <p class="text-default-600 text-sm">
          #{{ preview.source.id }} 以后自动跳转到 #{{
            preview.target.id
          }}。论坛、补丁站、LetMoe
          与资料库使用相同的作品编号，会通过重定向通知跟着跳转；站内挂在旧编号上的内容由各站按重定向处理。
        </p>
      </div>
    </div>
  </MergeWorkbenchSection>
</template>
