<script setup lang="ts">
import {
  NEWS_FILTER_ALL,
  NEWS_STATUS,
  NEWS_STATUS_LABELS,
  NEWS_STATUS_COLORS,
  NEWS_LANE_LABELS,
  NEWS_SOURCE_LABELS
} from '~/constants/news'
import type {
  NewsAdminItem,
  NewsAdminItemDetail,
  NewsAdminQueue,
  NewsAdminStats
} from '~~/shared/types/news'

const api = useApi('catalog')

const page = ref(1)
const limit = 30
const status = ref<number | ''>(NEWS_STATUS.pending)
const lane = ref<string>(NEWS_FILTER_ALL)
const source = ref<string>(NEWS_FILTER_ALL)
const ungradedOnly = ref(false)
const degradedOnly = ref(false)
watch([status, lane, source, ungradedOnly, degradedOnly], () => {
  page.value = 1
})

const {
  data,
  status: fetchStatus,
  refresh,
  error
} = await useApiFetch<NewsAdminQueue>(
  '/admin/news/items',
  {
    query: computed(() => ({
      status: status.value === '' ? undefined : status.value,
      lane: lane.value || undefined,
      source: source.value || undefined,
      ungraded: ungradedOnly.value ? 'true' : undefined,
      degraded: degradedOnly.value ? 'true' : undefined,
      offset: (page.value - 1) * limit,
      limit
    }))
  },
  'catalog'
)
const items = computed(() => data.value?.items ?? [])
const total = computed(() => data.value?.total ?? 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / limit)))
const isLoading = computed(() => fetchStatus.value === 'pending')

const { data: statsData, refresh: refreshStats } =
  await useApiFetch<NewsAdminStats>('/admin/news/stats', {}, 'catalog')
const stats = computed(() => statsData.value)

const statusOptions = [
  { value: NEWS_FILTER_ALL, label: '全部状态' },
  ...Object.entries(NEWS_STATUS_LABELS).map(([id, label]) => ({
    value: Number(id),
    label
  }))
]
const laneOptions = [
  { value: NEWS_FILTER_ALL, label: '全部泳道' },
  ...Object.entries(NEWS_LANE_LABELS).map(([id, label]) => ({
    value: id,
    label
  }))
]
const sourceOptions = [
  { value: NEWS_FILTER_ALL, label: '全部来源' },
  ...Object.entries(NEWS_SOURCE_LABELS).map(([id, label]) => ({
    value: id,
    label
  }))
]

const formatTime = (raw: string) =>
  new Date(raw).toLocaleString('zh-CN', { hour12: false })

const detailOpen = ref(false)
const detail = ref<NewsAdminItemDetail | null>(null)
const loadingDetail = ref(false)

const openDetail = async (item: NewsAdminItem) => {
  loadingDetail.value = true
  detail.value = null
  detailOpen.value = true
  const res = await api.get<NewsAdminItemDetail>(`/admin/news/items/${item.id}`)
  loadingDetail.value = false
  if (res.code === 0) {
    detail.value = res.data
  } else {
    useKunMessage(res.message || '加载详情失败', 'error')
    detailOpen.value = false
  }
}

const onDecided = async () => {
  detailOpen.value = false
  await Promise.all([refresh(), refreshStats()])
}
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-foreground text-2xl font-bold">情报审核队列</h1>
    <p class="text-default-500 text-sm">
      合作方授权我们转载预览与题图，正文留在对方站点。
      <span class="text-foreground">发布是人的行为</span>
      ——机器只给建议，任何条目都不会自动上线。
    </p>

    <div v-if="stats" class="flex flex-wrap gap-3">
      <KunCard v-for="s in [0, 1, 2, 3]" :key="s" class="min-w-32 p-3">
        <p class="text-default-500 text-xs">{{ NEWS_STATUS_LABELS[s] }}</p>
        <p class="text-foreground text-xl font-bold">
          {{ stats.by_status[String(s)] ?? 0 }}
        </p>
      </KunCard>
      <KunCard class="min-w-32 p-3">
        <p class="text-default-500 text-xs">待审中未评分</p>
        <p
          class="text-xl font-bold"
          :class="stats.ungraded > 0 ? 'text-warning' : 'text-foreground'"
        >
          {{ stats.ungraded }}
        </p>
      </KunCard>
      <KunCard class="min-w-32 p-3">
        <p class="text-default-500 text-xs">待审中评分降级</p>
        <p
          class="text-xl font-bold"
          :class="stats.degraded > 0 ? 'text-danger' : 'text-foreground'"
        >
          {{ stats.degraded }}
        </p>
      </KunCard>
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <KunSelect
        v-model="status"
        :options="statusOptions"
        aria-label="状态过滤"
        class="w-36"
      />
      <KunSelect
        v-model="lane"
        :options="laneOptions"
        aria-label="泳道过滤"
        class="w-36"
      />
      <KunSelect
        v-model="source"
        :options="sourceOptions"
        aria-label="来源过滤"
        class="w-44"
      />
      <div class="text-default-500 flex items-center gap-2 text-sm">
        <KunSwitch v-model="ungradedOnly" aria-label="只看未评分" />
        只看未评分
      </div>
      <div class="text-default-500 flex items-center gap-2 text-sm">
        <KunSwitch v-model="degradedOnly" aria-label="只看降级" />
        只看降级
      </div>
      <KunButton
        size="sm"
        variant="flat"
        :is-loading="isLoading"
        @click="refresh()"
      >
        <KunIcon name="lucide:refresh-cw" class="mr-1 size-4" />
        刷新
      </KunButton>
    </div>
    <p class="text-default-500 text-sm">共 {{ total }} 条</p>

    <CommonFetchError v-if="error" @retry="refresh" />

    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[60rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">标题</th>
            <th class="px-3 py-2 text-left font-medium">来源</th>
            <th class="px-3 py-2 text-left font-medium">机器意见</th>
            <th class="px-3 py-2 text-left font-medium">状态</th>
            <th class="px-3 py-2 text-left font-medium">发布时间</th>
            <th class="px-3 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="item.id"
            class="border-default-200 border-t align-top"
          >
            <td class="max-w-md px-3 py-2">
              <p class="text-foreground font-medium">{{ item.title }}</p>
              <p class="text-default-400 line-clamp-2 text-xs">
                {{ item.preview }}
              </p>
            </td>
            <td class="px-3 py-2">
              <p>
                {{ NEWS_SOURCE_LABELS[item.source_key] ?? item.source_key }}
              </p>
              <KunChip color="info" variant="flat" size="xs">
                {{ NEWS_LANE_LABELS[item.lane] ?? item.lane }}
              </KunChip>
            </td>
            <td class="px-3 py-2">
              <NewsVerdictBadges :item="item" />
            </td>
            <td class="px-3 py-2">
              <KunChip
                :color="NEWS_STATUS_COLORS[item.status] ?? 'default'"
                variant="flat"
                size="xs"
              >
                {{ NEWS_STATUS_LABELS[item.status] ?? item.status }}
              </KunChip>
            </td>
            <td class="text-default-500 px-3 py-2 whitespace-nowrap">
              {{ formatTime(item.published_at) }}
            </td>
            <td class="px-3 py-2 text-right">
              <KunButton size="xs" variant="flat" @click="openDetail(item)">
                审核
              </KunButton>
            </td>
          </tr>
          <tr v-if="!items.length && !isLoading">
            <td colspan="6" class="text-default-400 px-3 py-10 text-center">
              没有符合条件的条目
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <KunPagination
      v-model:current-page="page"
      :total-page="totalPages"
      :is-loading="isLoading"
    />

    <KunModal v-model="detailOpen" size="lg" aria-label="新闻详情">
      <div v-if="loadingDetail" class="flex justify-center py-10">
        <KunIcon
          name="lucide:loader-circle"
          class="text-primary size-8 animate-spin"
        />
      </div>
      <NewsDetail v-else-if="detail" :detail="detail" @decided="onDecided" />
    </KunModal>
  </div>
</template>
