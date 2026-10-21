<script setup lang="ts">
import type {
  CatalogSplitRequest,
  CatalogSplitRequestList
} from '#shared/types/catalog'
import { problemStatus } from '~/utils/review/problem'

const props = defineProps<{ mode: 'review' | 'mine' }>()

const STATES = [
  { value: 'open', label: '待审核' },
  { value: 'approved', label: '冷静期 / 待执行' },
  { value: 'executed', label: '已拆分' },
  { value: 'undone', label: '已撤销' },
  { value: 'rejected', label: '已驳回' },
  { value: 'withdrawn', label: '已撤回' }
]

const api = useEditorApi()
const route = useRoute()
const router = useRouter()

const state = computed({
  get: () =>
    STATES.some((item) => item.value === route.query.state)
      ? String(route.query.state)
      : props.mode === 'review'
        ? 'open'
        : '',
  set: (value: string) =>
    router.replace({ query: { ...route.query, state: value || undefined } })
})

const items = ref<CatalogSplitRequest[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(true)
const forbidden = ref(false)
const now = ref(Date.now())

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CatalogSplitRequestList>(
      props.mode === 'review'
        ? '/v2/moderation/split-requests'
        : '/v2/me/split-requests',
      {
        query: {
          limit: 30,
          ...(state.value ? { state: state.value } : {}),
          ...(cursor ? { cursor } : {})
        }
      }
    )
    forbidden.value = false
    items.value = cursor ? [...items.value, ...list.items] : list.items
    nextCursor.value = list.next_cursor ?? null
  } catch (error) {
    forbidden.value = problemStatus(error) === 403
    if (!cursor) {
      items.value = []
    }
    nextCursor.value = null
  } finally {
    loading.value = false
    now.value = Date.now()
  }
}

watch(state, () => fetchPage(null))
onMounted(() => fetchPage(null))

const stateOptions = computed(() =>
  props.mode === 'mine' ? [{ value: '', label: '全部' }, ...STATES] : STATES
)
</script>

<template>
  <div class="space-y-4">
    <KunRadioGroup
      v-model="state"
      :options="stateOptions"
      variant="pill"
      orientation="horizontal"
      size="sm"
      aria-label="状态"
    />

    <KunInfo
      v-if="forbidden"
      color="warning"
      variant="flat"
      title="需要合并审核权限"
      description="拆分请求和合并请求一样，由版主、管理员审核。"
    />
    <KunCard v-else bordered padding="md">
      <CommonSkeletonRows v-if="loading && !items.length" :rows="5" trailing />
      <ul v-else-if="items.length" class="divide-default-200 divide-y">
        <SplitRow
          v-for="request in items"
          :key="request.id"
          :request="request"
          :now="now"
        />
      </ul>
      <KunNull
        v-else-if="!loading"
        :description="
          props.mode === 'mine'
            ? '还没有发起过拆分请求；在作品页点「拆分」开始'
            : '没有这个状态的拆分请求'
        "
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
