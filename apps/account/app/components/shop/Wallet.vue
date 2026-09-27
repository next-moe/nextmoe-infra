<script setup lang="ts">
import { MOEMOEPOINT_ICON } from '~/constants/moemoepoint'

defineProps<{ balance: number }>()
</script>

<template>
  <KunCard padding="md" class-name="sm:min-w-72">
    <div class="flex items-center gap-4">
      <div
        class="bg-secondary-100 text-secondary-600 flex size-12 shrink-0 items-center justify-center rounded-2xl"
      >
        <KunIcon :name="MOEMOEPOINT_ICON" class="size-6" />
      </div>
      <div class="min-w-0 flex-1">
        <p class="text-default-500 text-xs">我的萌萌点</p>
        <p
          class="text-foreground mt-1 text-3xl leading-none font-bold tracking-tight tabular-nums"
        >
          {{ balance }}
        </p>
      </div>
      <KunButton
        href="/profile"
        size="sm"
        variant="light"
        color="secondary"
        class-name="self-start"
      >
        明细
        <KunIcon name="lucide:chevron-right" class="size-4" />
      </KunButton>
    </div>
    <p class="text-default-400 text-xs leading-relaxed">
      每日签到、投稿被采纳、内容被点赞，都能攒萌萌点
    </p>
  </KunCard>
</template>
