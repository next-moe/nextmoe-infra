<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'

useHead({ title: '请求审核 · 未萌编辑台' })

const route = useRoute()
const router = useRouter()

const TABS: KunTabItem[] = [
  { value: 'merge', textValue: '合并', icon: 'lucide:git-merge' },
  { value: 'split', textValue: '拆分', icon: 'lucide:git-fork' },
  { value: 'delete', textValue: '删除', icon: 'lucide:trash-2' }
]

const tab = computed({
  get: () =>
    TABS.some((item) => item.value === route.query.tab)
      ? String(route.query.tab)
      : 'merge',
  set: (value: string) =>
    router.replace({
      query: {
        ...route.query,
        tab: value === 'merge' ? undefined : value,
        state: undefined
      }
    })
})
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"
    >
      <KunHeader
        name="请求审核"
        description="合并、拆分、删除条目都要经过审核；批准后先过冷静期再执行，执行后一段时间内可以撤销。"
      />
      <KunButton href="/review" variant="light" size="sm">
        <KunIcon name="lucide:inbox" class="size-4" />
        编辑提案
      </KunButton>
    </div>

    <KunTab
      v-model="tab"
      :items="TABS"
      variant="underlined"
      name="review-requests"
    />

    <ReviewRequestsMergeTab v-if="tab === 'merge'" />
    <ReviewRequestsSplitTab v-else-if="tab === 'split'" />
    <ReviewRequestsDeleteTab v-else />
  </div>
</template>
