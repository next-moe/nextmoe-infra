<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogWalkthrough } from '#shared/types/catalog'

const props = defineProps<{ entityId: string }>()

const api = useEditorApi()
const doc = useWalkthroughDocument()

const { data: review } = useLazyAsyncData(
  `walkthrough-backlog-${props.entityId}`,
  async () => {
    const page = await api<{ items: CatalogWalkthrough[] }>(
      '/v2/me/walkthrough-backlog',
      { query: { ids: props.entityId } }
    ).catch(() => null)
    return page?.items[0]?.import_review ?? null
  }
)

const chartText = computed(() => {
  const texts = new Set<string>()
  for (const segment of doc.chart.value.segments) {
    for (const step of segment.steps) {
      if (step.text) texts.add(step.text.trim())
    }
  }
  return texts
})

const missed = computed(() =>
  (review.value?.missed ?? []).map((text) => {
    const bare = text.replace(/^[○●◆◇■□・\s]+/, '').trim()
    return { text, bare, added: chartText.value.has(bare) }
  })
)
const remaining = computed(() => missed.value.filter((m) => !m.added).length)

const copy = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text)
    useKunMessage(`已复制「${text}」`, 'success')
  } catch {
    useKunMessage('复制失败，请手动选择文字', 'error')
  }
}
</script>

<template>
  <KunInfo
    v-if="review"
    color="warning"
    variant="flat"
    icon="lucide:list-checks"
    title="这篇攻略导入时没有完整提取，补全后再取消隐藏"
  >
    <p>
      原攻略页标注了 {{ review.end_markers }} 个结局，图里连到了
      {{ review.endings_reached }} 个；选项覆盖了
      {{ Math.round(review.coverage * 100) }}%。
      <template v-if="missed.length">
        下面这些选项还没有放进图里（还剩 {{ remaining }} 个），点击可以复制：
      </template>
    </p>
    <div v-if="missed.length" class="mt-2 flex flex-wrap gap-1.5">
      <button
        v-for="item in missed"
        :key="item.text"
        type="button"
        :class="
          cn(
            'rounded-md px-2 py-0.5 text-xs transition-colors',
            item.added
              ? 'bg-success-100 text-success-700 line-through'
              : 'bg-content1 text-foreground hover:bg-default-100'
          )
        "
        :title="item.added ? '已经在图里了' : '复制这个选项'"
        @click="copy(item.bare)"
      >
        {{ item.text }}
      </button>
    </div>
  </KunInfo>
</template>
