<script setup lang="ts">
import { MERGE_USER_COUNT_KEYS } from '~/constants/merge'

const { preview } = useMergeWorkbench()

const counts = computed(() =>
  (preview.value?.counts ?? []).filter(
    (count) =>
      !MERGE_USER_COUNT_KEYS.includes(count.key) &&
      (count.on_source > 0 || count.on_target > 0)
  )
)
</script>

<template>
  <MergeWorkbenchSection v-if="preview" section-key="related">
    <MergeWorkbenchCountTable v-if="counts.length" :counts="counts" />
    <p v-else class="text-default-400 text-sm">两边都没有关联数据。</p>
  </MergeWorkbenchSection>
</template>
