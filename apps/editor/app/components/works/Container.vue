<script setup lang="ts">
import type { CatalogWork, CatalogWorkList } from '#shared/types/catalog'

useHead({ title: '作品 · 未萌编辑台' })

const PAGE_SIZE = 24

const api = useEditorApi()
const route = useRoute()
const router = useRouter()

const keyword = ref(typeof route.query.q === 'string' ? route.query.q : '')
const works = ref<CatalogWork[]>([])
const nextCursor = ref<string | null>(null)
const total = ref<number | null>(null)
const loading = ref(false)
const failed = ref(false)

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  failed.value = false
  try {
    const q = keyword.value.trim()
    const list = await api<CatalogWorkList>('/v2/catalog/works', {
      query: {
        limit: PAGE_SIZE,
        nsfw: true,
        include_total: true,
        ...(q ? { q } : {}),
        ...(cursor ? { cursor } : {})
      }
    })
    works.value = cursor ? [...works.value, ...list.items] : list.items
    nextCursor.value = list.next_cursor ?? null
    total.value = list.total ?? null
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

let timer: ReturnType<typeof setTimeout> | undefined
watch(keyword, (value) => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    router.replace({ query: value.trim() ? { q: value.trim() } : {} })
    fetchPage(null)
  }, 300)
})

await fetchPage(null)
</script>

<template>
  <div class="mx-auto max-w-7xl space-y-5">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"
    >
      <KunHeader
        name="作品"
        description="浏览与搜索资料库中的全部作品，进入条目查看与编辑。"
      />
      <div class="flex shrink-0 items-center gap-3">
        <p
          v-if="total !== null"
          class="text-default-500 shrink-0 text-sm tabular-nums"
        >
          共 {{ total.toLocaleString() }} 部
        </p>
        <KunButton href="/new/work" color="primary" size="sm">
          <KunIcon name="lucide:plus" class="size-4" />
          新建作品
        </KunButton>
      </div>
    </div>

    <KunInput
      v-model="keyword"
      placeholder="按标题、别名或外部 ID 搜索"
      is-clearable
      class-name="max-w-xl"
    />

    <KunInfo
      v-if="failed"
      color="danger"
      variant="flat"
      title="加载失败"
      description="资料库暂时无法访问，请稍后重试。"
    />

    <div
      v-if="works.length"
      class="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6"
    >
      <WorksCard v-for="work in works" :key="work.id" :work="work" />
    </div>
    <KunNull v-else-if="!loading && !failed" description="没有找到作品" />

    <div class="flex justify-center py-2">
      <KunLoading v-if="loading" spinner />
      <KunButton
        v-else-if="nextCursor"
        variant="bordered"
        @click="fetchPage(nextCursor)"
      >
        加载更多
      </KunButton>
    </div>
  </div>
</template>
