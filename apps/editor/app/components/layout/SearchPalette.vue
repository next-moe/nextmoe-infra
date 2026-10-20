<script setup lang="ts">
import type { KunCommandGroup, KunCommandItem } from '@kungal/ui-vue'
import type { CatalogSearchList } from '#shared/types/catalog'
import { SEARCH_FAMILIES } from '~/constants/search'

const api = useEditorApi()

const query = ref('')
const loading = ref(false)
const groups = ref<KunCommandGroup[]>([])

let timer: ReturnType<typeof setTimeout> | undefined
let generation = 0

const runSearch = async (q: string) => {
  const current = ++generation
  loading.value = true
  const results = await Promise.all(
    SEARCH_FAMILIES.map(async (family) => {
      if (family.searchVia === 'list') {
        const list = await api<{
          items?: { id: string; display_name?: string; title?: string }[]
        }>(`/v2/catalog/${family.collection}`, {
          query: { q, limit: 5, nsfw: true }
        }).catch(() => null)
        const items: KunCommandItem[] = (list?.items ?? []).map((item) => ({
          value: `${family.path}/${item.id}`,
          label: item.display_name ?? item.title ?? `#${item.id}`,
          icon: family.icon
        }))
        return { label: family.label, items }
      }
      const list = await api<CatalogSearchList>('/v2/catalog/search', {
        query: { q, object: family.object, limit: 5, nsfw: true }
      }).catch(() => null)
      const items: KunCommandItem[] = (list?.items ?? []).map((hit) => ({
        value: `${family.path}/${hit.id}`,
        label: hit.display_name,
        description: hit.latin ?? undefined,
        icon: family.icon
      }))
      return { label: family.label, items }
    })
  )
  if (current !== generation) {
    return
  }
  groups.value = results.filter((group) => group.items.length > 0)
  loading.value = false
}

watch(query, (value) => {
  clearTimeout(timer)
  const q = value.trim()
  if (!q) {
    generation++
    groups.value = []
    loading.value = false
    return
  }
  timer = setTimeout(() => runSearch(q), 250)
})

const handleSelect = (item: KunCommandItem) => {
  if (typeof item.value === 'string') {
    navigateTo(item.value)
  }
}
</script>

<template>
  <KunCommandPalette
    v-model:query="query"
    :items="groups"
    :loading="loading"
    placeholder="搜索作品、角色、署名名、厂商、标签、特质…"
    empty-text="输入关键词开始搜索"
    no-result-text="没有找到匹配的条目"
    aria-label="搜索条目"
    @select="handleSelect"
  >
    <template #trigger="{ open, shortcut }">
      <button
        type="button"
        class="border-default-200 bg-default-100 text-default-500 hover:border-default-300 hover:text-foreground flex h-8 w-full max-w-md items-center gap-2 rounded-lg border px-3 text-sm transition-colors"
        @click="open"
      >
        <KunIcon name="lucide:search" class="size-4 shrink-0" />
        <span class="min-w-0 flex-1 truncate text-left">搜索条目</span>
        <kbd
          class="border-default-200 bg-content1 text-default-400 hidden rounded border px-1.5 text-xs sm:inline"
        >
          {{ shortcut }}
        </kbd>
      </button>
    </template>
  </KunCommandPalette>
</template>
