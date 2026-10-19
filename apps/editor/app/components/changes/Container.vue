<script setup lang="ts">
import type { KunSelectOption } from '@kungal/ui-vue'
import type {
  CatalogRevision,
  CatalogRevisionList
} from '#shared/types/catalog'
import { ENTITY_FAMILY } from '~/constants/catalog'

useHead({ title: '最近修订 · 未萌编辑台' })

const PAGE_SIZE = 30

const api = useEditorApi()
const names = useEntityNames()

const family = ref('')
const revisions = ref<CatalogRevision[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(false)

const familyOptions: KunSelectOption<string>[] = [
  { value: '', label: '全部类型' },
  ...Object.entries(ENTITY_FAMILY).map(([value, { label }]) => ({
    value,
    label
  }))
]

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CatalogRevisionList>('/v2/catalog/revisions', {
      query: {
        limit: PAGE_SIZE,
        ...(family.value ? { object: family.value } : {}),
        ...(cursor ? { cursor } : {})
      }
    })
    revisions.value = cursor ? [...revisions.value, ...list.items] : list.items
    nextCursor.value = list.next_cursor ?? null
    await names.resolve(list.items)
  } catch {
    if (!cursor) {
      revisions.value = []
    }
    nextCursor.value = null
  } finally {
    loading.value = false
  }
}

watch(family, () => fetchPage(null))

await fetchPage(null)
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-5">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"
    >
      <KunHeader
        name="最近修订"
        description="资料库里每一次生效的修改，按时间倒序。"
      />
      <KunSelect
        v-model="family"
        :options="familyOptions"
        aria-label="条目类型"
        class-name="w-40"
      />
    </div>

    <KunCard bordered padding="md">
      <ul v-if="revisions.length" class="divide-default-200 divide-y">
        <CommonRevisionItem
          v-for="revision in revisions"
          :key="revision.id"
          :revision="revision"
          :name="names.nameOf(revision)"
        />
      </ul>
      <KunNull v-else-if="!loading" description="暂无修订记录" />
      <div class="flex justify-center pt-3">
        <KunLoading v-if="loading" spinner />
        <KunButton
          v-else-if="nextCursor"
          variant="bordered"
          @click="fetchPage(nextCursor)"
        >
          加载更多
        </KunButton>
      </div>
    </KunCard>
  </div>
</template>
