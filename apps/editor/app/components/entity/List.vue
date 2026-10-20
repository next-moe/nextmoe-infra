<script setup lang="ts">
import type {
  CatalogEntityList,
  CatalogEntitySummary
} from '#shared/types/catalog'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'

const PAGE_SIZE = 30

const props = defineProps<{ family: EditFamily }>()

const meta = ENTITY_META[props.family]
const api = useEditorApi()
const route = useRoute()
const router = useRouter()

const canSearch = meta.searchable || meta.listQuery
const keyword = ref(
  canSearch && typeof route.query.q === 'string' ? route.query.q : ''
)
const items = ref<CatalogEntitySummary[]>([])
const nextCursor = ref<string | null>(null)
const total = ref<number | null>(null)
const loading = ref(false)
const failed = ref(false)

const request = (cursor: string | null): Promise<CatalogEntityList> => {
  const q = keyword.value.trim()
  const paging = { limit: PAGE_SIZE, nsfw: true, ...(cursor ? { cursor } : {}) }
  if (q && meta.searchable && !meta.listQuery) {
    return api<CatalogEntityList>('/v2/catalog/search', {
      query: { ...paging, q, object: props.family, include_total: true }
    })
  }
  return api<CatalogEntityList>(`/v2/catalog/${meta.collection}`, {
    query: { ...paging, include_total: true, ...(q ? { q } : {}) }
  })
}

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  failed.value = false
  try {
    const list = await request(cursor)
    items.value = cursor ? [...items.value, ...list.items] : list.items
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

const nameOf = (item: CatalogEntitySummary) =>
  item.display_name || item.title || `#${item.id}`
const pictureOf = (item: CatalogEntitySummary) =>
  item.image ?? item.photo ?? item.logo ?? null
const secondaryOf = (item: CatalogEntitySummary) =>
  [item.latin, item.platform, item.date]
    .filter((part) => typeof part === 'string' && part)
    .join(' · ')

await fetchPage(null)

useHead({ title: `${meta.label} · 未萌编辑台` })
</script>

<template>
  <div class="mx-auto max-w-7xl space-y-5">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"
    >
      <KunHeader :name="meta.label" :description="meta.description" />
      <p
        v-if="total !== null"
        class="text-default-500 shrink-0 text-sm tabular-nums"
      >
        共 {{ total.toLocaleString() }} 条
      </p>
    </div>

    <div class="space-y-1">
      <KunInput
        v-model="keyword"
        :placeholder="canSearch ? `按名称搜索${meta.label}` : '不支持搜索'"
        :disabled="!canSearch"
        is-clearable
        class-name="max-w-xl"
      />
      <p v-if="meta.searchHint" class="text-default-400 text-xs">
        {{ meta.searchHint }}
      </p>
    </div>

    <KunInfo
      v-if="failed"
      color="danger"
      variant="flat"
      title="列表加载失败"
      description="请稍后重试。"
    />

    <KunNull
      v-else-if="!loading && items.length === 0"
      :description="keyword ? '没有找到匹配的条目' : '暂无条目'"
    />

    <ul v-else class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <li v-for="item in items" :key="item.id">
        <NuxtLink
          :to="`${meta.path}/${item.id}`"
          class="border-default-200 bg-content1 hover:border-primary-300 flex items-center gap-3 rounded-xl border p-3 transition-colors"
        >
          <div
            class="bg-default-100 flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-lg"
          >
            <KunImage
              v-if="pictureOf(item)?.url"
              :src="pictureOf(item)!.url"
              :thumbhash="pictureOf(item)!.thumbhash ?? undefined"
              :alt="nameOf(item)"
              aspect-ratio="1 / 1"
              object-fit="cover"
            />
            <KunIcon v-else :name="meta.icon" class="text-default-300 size-5" />
          </div>
          <div class="min-w-0 flex-1">
            <p class="truncate font-medium">{{ nameOf(item) }}</p>
            <p class="text-default-400 truncate text-xs">
              {{ secondaryOf(item) || `#${item.id}` }}
            </p>
          </div>
        </NuxtLink>
      </li>
    </ul>

    <div v-if="nextCursor" class="flex justify-center">
      <KunButton
        variant="flat"
        :loading="loading"
        @click="fetchPage(nextCursor)"
      >
        加载更多
      </KunButton>
    </div>
  </div>
</template>
