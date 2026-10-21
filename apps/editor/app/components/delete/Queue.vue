<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'
import type {
  CatalogDeletionRequest,
  CatalogDeletionRequestList
} from '#shared/types/catalog'
import { problemStatus } from '~/utils/review/problem'

const props = defineProps<{ mode: 'review' | 'mine' }>()

const REVIEW_TABS: KunTabItem[] = [
  { value: 'open', textValue: '待审核', icon: 'lucide:inbox' },
  { value: 'approved', textValue: '冷静期', icon: 'lucide:hourglass' },
  { value: 'executed', textValue: '已删除', icon: 'lucide:trash-2' },
  { value: 'restored', textValue: '已恢复', icon: 'lucide:archive-restore' },
  { value: 'rejected', textValue: '已驳回', icon: 'lucide:circle-x' },
  { value: 'all', textValue: '全部', icon: 'lucide:list' }
]
const MINE_TABS: KunTabItem[] = [
  { value: 'all', textValue: '全部', icon: 'lucide:list' },
  { value: 'open', textValue: '待审核', icon: 'lucide:inbox' },
  { value: 'approved', textValue: '冷静期', icon: 'lucide:hourglass' },
  { value: 'executed', textValue: '已删除', icon: 'lucide:trash-2' }
]
const tabs = computed(() => (props.mode === 'review' ? REVIEW_TABS : MINE_TABS))

const api = useEditorApi()
const route = useRoute()
const router = useRouter()

const queryString = (key: string) =>
  typeof route.query[key] === 'string' ? (route.query[key] as string) : ''

const state = ref(
  tabs.value.some((t) => t.value === queryString('state'))
    ? queryString('state')
    : String(tabs.value[0]!.value)
)
const selectedId = ref<string | null>(queryString('id') || null)
const items = ref<CatalogDeletionRequest[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(true)
const forbidden = ref(false)
const now = ref(Date.now())
const wide = ref(true)

const endpoint = computed(() =>
  props.mode === 'review'
    ? '/v2/moderation/deletion-requests'
    : '/v2/me/deletion-requests'
)

const selected = computed(
  () => items.value.find((item) => item.id === selectedId.value) ?? null
)

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CatalogDeletionRequestList>(endpoint.value, {
      query: {
        limit: 30,
        ...(state.value === 'all' ? {} : { state: state.value }),
        ...(cursor ? { cursor } : {})
      }
    })
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

const select = (id: string | null) => {
  selectedId.value = id
}

watch([state, selectedId], ([s, id]) => {
  router.replace({
    query: {
      ...route.query,
      state: s === tabs.value[0]!.value ? undefined : s,
      id: id ?? undefined
    }
  })
})

watch(state, () => {
  selectedId.value = null
  fetchPage(null)
})

watch(items, (list) => {
  if (wide.value && !selected.value && list[0]) {
    selectedId.value = list[0].id
  }
})

const onChanged = (updated: CatalogDeletionRequest) => {
  items.value = items.value.map((item) =>
    item.id === updated.id ? updated : item
  )
  now.value = Date.now()
}

const drawerOpen = computed({
  get: () => !wide.value && selected.value !== null,
  set: (open: boolean) => {
    if (!open) {
      selectedId.value = null
    }
  }
})

let media: MediaQueryList | null = null
const onMedia = () => {
  wide.value = media?.matches ?? true
}
let ticker: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  media = window.matchMedia('(min-width: 1024px)')
  onMedia()
  media.addEventListener('change', onMedia)
  ticker = setInterval(() => {
    now.value = Date.now()
  }, 30_000)
})
onBeforeUnmount(() => {
  media?.removeEventListener('change', onMedia)
  clearInterval(ticker)
})

fetchPage(null)
</script>

<template>
  <div class="space-y-4">
    <KunTab
      v-model="state"
      :items="tabs"
      variant="underlined"
      :name="`deletion-${props.mode}`"
    />

    <KunInfo
      v-if="forbidden"
      color="warning"
      variant="flat"
      title="需要删除审核权限"
      description="删除请求由版主、管理员审核。"
    />

    <div
      v-else
      class="grid gap-4 lg:grid-cols-[minmax(18rem,22rem)_minmax(0,1fr)]"
    >
      <KunCard bordered padding="sm" class-name="self-start">
        <CommonSkeletonRows v-if="loading && !items.length" :rows="6" />
        <ul v-else-if="items.length" class="space-y-0.5">
          <DeleteRow
            v-for="request in items"
            :key="request.id"
            :request="request"
            :now="now"
            :selected="request.id === selectedId"
            @select="select(request.id)"
          />
        </ul>
        <KunNull
          v-else-if="!loading"
          :description="
            props.mode === 'review'
              ? '没有这个状态的删除请求'
              : '你还没有提交过删除请求'
          "
        />
        <div class="flex justify-center pt-2">
          <KunLoading v-if="loading && items.length" spinner />
          <KunButton
            v-else-if="nextCursor"
            variant="light"
            size="sm"
            @click="fetchPage(nextCursor)"
          >
            加载更多
          </KunButton>
        </div>
      </KunCard>

      <KunCard
        v-if="wide"
        bordered
        padding="lg"
        class-name="hidden lg:block lg:sticky lg:top-4 self-start"
      >
        <DeleteDetail
          v-if="selected"
          :request="selected"
          :mode="props.mode"
          :now="now"
          @changed="onChanged"
        />
        <div v-else-if="loading" class="space-y-3" aria-hidden="true">
          <KunSkeleton variant="text" width="40%" />
          <KunSkeleton variant="text" width="70%" class-name="h-6" />
          <KunSkeleton variant="rect" class-name="h-24 w-full" />
        </div>
        <KunNull v-else description="选择左侧的一条请求查看详情" />
      </KunCard>
    </div>

    <KunDrawer
      v-model="drawerOpen"
      placement="right"
      size="lg"
      aria-label="删除请求详情"
    >
      <DeleteDetail
        v-if="selected"
        :request="selected"
        :mode="props.mode"
        :now="now"
        narrow
        @changed="onChanged"
      />
    </KunDrawer>
  </div>
</template>
