<script setup lang="ts">
import type { CatalogWalkthrough } from '#shared/types/catalog'

interface BacklogPage {
  items: CatalogWalkthrough[]
  next_cursor?: string | null
  total?: number
}

const api = useEditorApi()
const refs = useRefNames()

const items = ref<CatalogWalkthrough[]>([])
const cursor = ref<string | null>(null)
const total = ref<number | null>(null)
const loading = ref(true)
const loadingMore = ref(false)

const load = async (more = false) => {
  if (more) loadingMore.value = true
  const page = await api<BacklogPage>('/v2/me/walkthrough-backlog', {
    query: {
      limit: 50,
      include_total: !more,
      ...(more && cursor.value ? { cursor: cursor.value } : {})
    }
  }).catch(() => null)
  const fresh = page?.items ?? []
  items.value = more ? [...items.value, ...fresh] : fresh
  cursor.value = page?.next_cursor ?? null
  if (!more) total.value = page?.total ?? null
  await refs.resolve('work', [...new Set(fresh.map((item) => item.work_id))])
  loading.value = false
  loadingMore.value = false
}

const characters = (item: CatalogWalkthrough) =>
  item.characters.map((c) => c.display_name || c.name).join('、') || '没有角色'

const progress = (item: CatalogWalkthrough) => {
  const review = item.import_review
  if (!review) return ''
  const parts = [`覆盖 ${Math.round(review.coverage * 100)}%`]
  if (review.missed.length) parts.push(`缺 ${review.missed.length} 个选项`)
  if (review.endings_reached < review.end_markers) {
    parts.push(`结局 ${review.endings_reached}/${review.end_markers}`)
  }
  return parts.join(' · ')
}

onMounted(() => load())
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-6">
    <KunHeader
      name="待补全的攻略"
      description="这些攻略在导入时没有完整提取出来：有的漏了选项，有的缺了通往结局的路线。它们暂时隐藏，在编辑器里补全后取消隐藏，就会出现在作品页上。"
    />

    <KunLoading v-if="loading" spinner />
    <template v-else-if="items.length">
      <p v-if="total !== null" class="text-default-500 text-sm">
        共 {{ total }} 篇
      </p>
      <KunCard bordered padding="none">
        <ul class="divide-default-200 divide-y">
          <li
            v-for="item in items"
            :key="item.id"
            class="flex flex-col gap-3 p-4 sm:flex-row sm:items-center"
          >
            <div class="min-w-0 flex-1">
              <p class="truncate font-medium">
                {{
                  refs.nameOf('work', item.work_id) || `作品 #${item.work_id}`
                }}
              </p>
              <p class="text-default-500 mt-0.5 truncate text-sm">
                {{ characters(item) }}
              </p>
              <p class="text-warning-700 mt-1 text-xs">{{ progress(item) }}</p>
            </div>
            <KunButton
              :href="`/walkthroughs/${item.id}/edit`"
              target="_blank"
              size="sm"
              color="primary"
              variant="flat"
            >
              去补全
            </KunButton>
          </li>
        </ul>
      </KunCard>
      <div v-if="cursor" class="flex justify-center">
        <KunButton variant="light" :loading="loadingMore" @click="load(true)">
          加载更多
        </KunButton>
      </div>
    </template>
    <KunNull v-else description="没有待补全的攻略" :is-show-sticker="false" />
  </div>
</template>
