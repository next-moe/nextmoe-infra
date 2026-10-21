<script setup lang="ts">
import { MERGE_USER_COUNT_KEYS } from '~/constants/merge'

const { preview } = useMergeWorkbench()

const counts = computed(() =>
  (preview.value?.counts ?? []).filter((count) =>
    MERGE_USER_COUNT_KEYS.includes(count.key)
  )
)
</script>

<template>
  <MergeWorkbenchSection v-if="preview" section-key="users">
    <MergeWorkbenchCountTable :counts="counts" />
    <p class="text-default-500 flex items-start gap-1.5 text-sm">
      <KunIcon name="lucide:info" class="mt-0.5 size-4 shrink-0" />
      同一位用户在两部作品上都有评分或游玩状态时，合并后保留较新的一条；游玩时长取较长的一份。评分的编号不变，下游站点挂在评分上的点赞和讨论随之保留。
    </p>
  </MergeWorkbenchSection>
</template>
