<script setup lang="ts">
import type { KunSelectOption } from '@kungal/ui-vue'
import type {
  CatalogMergeRequest,
  CatalogMergeRequestList
} from '#shared/types/catalog'
import { MERGE_STATE_LABELS } from '~/constants/creation'
import { problemMessage } from '~/utils/review/problem'

useHead({ title: '我的合并请求 · 未萌编辑台' })

const api = useEditorApi()

const state = ref('')
const items = ref<CatalogMergeRequest[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(true)
const now = ref(Date.now())

const stateOptions: KunSelectOption<string>[] = [
  { value: '', label: '全部状态' },
  ...Object.entries(MERGE_STATE_LABELS).map(([value, label]) => ({
    value,
    label
  }))
]

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CatalogMergeRequestList>('/v2/me/merge-requests', {
      query: {
        limit: 30,
        ...(state.value ? { state: state.value } : {}),
        ...(cursor ? { cursor } : {})
      }
    })
    items.value = cursor ? [...items.value, ...list.items] : list.items
    nextCursor.value = list.next_cursor ?? null
  } catch {
    if (!cursor) {
      items.value = []
    }
    nextCursor.value = null
  } finally {
    loading.value = false
    now.value = Date.now()
  }
}

const withdraw = async (request: CatalogMergeRequest) => {
  try {
    await api(`/v2/me/merge-requests/${request.id}`, {
      method: 'PATCH',
      body: { state: 'withdrawn' }
    })
    useKunMessage('合并请求已撤回', 'success')
    await fetchPage(null)
  } catch (error) {
    useKunMessage(problemMessage(error, '撤回失败'), 'error')
  }
}

watch(state, () => fetchPage(null))
fetchPage(null)
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-5">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"
    >
      <KunHeader
        name="我的合并请求"
        description="你提交的重复条目合并请求。审核通过后要等冷却期结束才会执行；审核前可以撤回。"
      />
      <KunSelect
        v-model="state"
        :options="stateOptions"
        aria-label="状态"
        class="w-40"
      />
    </div>

    <KunCard bordered padding="md">
      <CommonSkeletonRows v-if="loading && !items.length" :rows="5" trailing />
      <ul v-else-if="items.length" class="divide-default-200 divide-y">
        <MergeRow
          v-for="request in items"
          :key="request.id"
          :request="request"
          :now="now"
        >
          <div v-if="request.state === 'open'" class="flex justify-end">
            <KunButton
              size="xs"
              variant="light"
              color="danger"
              @click="withdraw(request)"
            >
              撤回
            </KunButton>
          </div>
        </MergeRow>
      </ul>
      <KunNull
        v-else-if="!loading"
        description="还没有合并请求。在条目页面点「合并」即可提交。"
      />
      <div class="flex justify-center pt-3">
        <KunLoading v-if="loading && items.length" spinner />
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
