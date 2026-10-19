<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'
import type {
  CatalogProposal,
  CatalogProposalList
} from '#shared/types/catalog'

useHead({ title: '我的提案 · 未萌编辑台' })

const PAGE_SIZE = 20

const STATES: KunTabItem[] = [
  { value: 'all', textValue: '全部' },
  { value: 'open', textValue: '待审' },
  { value: 'merged', textValue: '已合并' },
  { value: 'declined', textValue: '已驳回' },
  { value: 'withdrawn', textValue: '已撤回' }
]

const api = useEditorApi()
const route = useRoute()
const router = useRouter()
const names = useEntityNames()

const state = ref(
  typeof route.query.state === 'string' &&
    STATES.some((s) => s.value === route.query.state)
    ? route.query.state
    : 'all'
)
const proposals = ref<CatalogProposal[]>([])
const nextCursor = ref<string | null>(null)
const total = ref<number | null>(null)
const loading = ref(false)

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CatalogProposalList>('/v2/me/proposals', {
      query: {
        limit: PAGE_SIZE,
        include_total: true,
        ...(state.value !== 'all' ? { state: state.value } : {}),
        ...(cursor ? { cursor } : {})
      }
    })
    proposals.value = cursor ? [...proposals.value, ...list.items] : list.items
    nextCursor.value = list.next_cursor ?? null
    total.value = list.total ?? null
    await names.resolve(list.items)
  } catch {
    if (!cursor) {
      proposals.value = []
    }
    nextCursor.value = null
  } finally {
    loading.value = false
  }
}

watch(state, (value) => {
  router.replace({ query: value === 'all' ? {} : { state: value } })
  fetchPage(null)
})

await fetchPage(null)
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-5">
    <KunHeader
      name="我的提案"
      description="你提交过的全部修改提案，以及它们的审核状态。"
    />

    <KunTab
      v-model="state"
      :items="STATES"
      variant="underlined"
      name="my-proposals"
    />

    <KunCard bordered padding="md">
      <p
        v-if="total !== null"
        class="text-default-400 mb-1 text-xs tabular-nums"
      >
        共 {{ total }} 条
      </p>
      <ul v-if="proposals.length" class="divide-default-200 divide-y">
        <CommonProposalItem
          v-for="proposal in proposals"
          :key="proposal.id"
          :proposal="proposal"
          :name="names.nameOf(proposal)"
        />
      </ul>
      <KunNull
        v-else-if="!loading"
        description="这里还没有提案。打开任意条目即可开始编辑。"
      />
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
