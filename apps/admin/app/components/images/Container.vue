<script setup lang="ts">
import { IMAGE_STATUS_TABS } from '~/constants/admin'

const api = useApi()

const site = ref('')
const reviewStatus = ref<string>('')
const currentPage = ref(1)
const limit = 50

const { data: listData, status: listStatus, refresh: refreshList } =
  await useApiFetch<ImageAdminListResponse>('/admin/image/list', {
    query: computed(() => ({
      page: currentPage.value,
      limit,
      ...(reviewStatus.value ? { review_status: reviewStatus.value } : {}),
      ...(site.value ? { site: site.value } : {})
    }))
  })
const { data: statsData, refresh: refreshStats } =
  await useApiFetch<ImageAdminStats>('/admin/image/stats')

const items = computed(() => listData.value?.items ?? [])
const total = computed(() => listData.value?.total ?? 0)
const stats = computed(() => statsData.value)
const isLoading = computed(() => listStatus.value === 'pending')
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / limit)))

watch(site, () => {
  currentPage.value = 1
})

const onReview = async (hash: string, status: string, reason?: string) => {
  const res = await api.patch(`/admin/image/${hash}/review`, { status, reason })
  if (res.code === 0) {
    useKunMessage('审核状态已更新', 'success')
    await refreshList()
    await refreshStats()
  } else {
    useKunMessage(res.message || '操作失败', 'error')
  }
}

const delOpen = ref(false)
const delHash = ref('')
const delForce = ref(false)

const onDelete = (hash: string, force = false) => {
  delHash.value = hash
  delForce.value = force
  delOpen.value = true
}

const onDeleted = async () => {
  await refreshList()
  await refreshStats()
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold text-foreground">图片管理</h1>
        <p class="mt-1 text-default-500">
          共 {{ total }} 张图片
        </p>
      </div>
    </div>

    <div v-if="stats" class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <KunCard content-class="justify-start gap-0" class-name="p-4">
        <div class="text-sm text-default-500">总上传次数</div>
        <div class="mt-1 text-2xl font-bold text-foreground">
          {{ stats.upload_count }}
        </div>
      </KunCard>
      <KunCard content-class="justify-start gap-0" class-name="p-4">
        <div class="text-sm text-default-500">唯一图片</div>
        <div class="mt-1 text-2xl font-bold text-foreground">
          {{ stats.unique_images }}
        </div>
        <div class="text-xs text-default-400">
          去重 {{ stats.deduplicated_count }}
        </div>
      </KunCard>
      <KunCard content-class="justify-start gap-0" class-name="p-4">
        <div class="text-sm text-default-500">存储用量</div>
        <div class="mt-1 text-2xl font-bold text-foreground">
          {{ formatFileSize(stats.total_bytes) }}
        </div>
      </KunCard>
      <KunCard content-class="justify-start gap-0" class-name="p-4">
        <div class="text-sm text-default-500">待审 / 已拒</div>
        <div class="mt-1 text-2xl font-bold text-foreground">
          {{ stats.review_pending }} / {{ stats.review_rejected }}
        </div>
      </KunCard>
    </div>

    <div class="rounded-xl bg-content1 p-4 shadow-sm space-y-3">
      <div class="flex flex-wrap gap-2">
        <KunButton
          v-for="tab in IMAGE_STATUS_TABS"
          :key="tab.id"
          :color="reviewStatus === tab.id ? 'primary' : 'default'"
          :variant="reviewStatus === tab.id ? 'solid' : 'flat'"
          size="sm"
          @click="reviewStatus = tab.id; currentPage = 1"
        >
          <KunIcon :name="tab.icon" class="mr-1 size-4" />
          {{ tab.label }}
        </KunButton>
      </div>
      <div class="flex gap-3">
        <KunInput
          v-model="site"
          type="text"
          placeholder="按站点过滤 (kungal / moyu / galgame_wiki)"
          class="flex-1"
        />
        <KunButton :disabled="isLoading" @click="currentPage = 1; refreshList()">
          刷新
        </KunButton>
      </div>
    </div>

    <div v-if="isLoading" class="flex items-center justify-center py-12">
      <KunIcon name="lucide:loader-circle" class="size-8 animate-spin text-primary" />
    </div>

    <template v-else>
      <ImagesTable
        :items="items"
        @review="onReview"
        @delete="onDelete"
      />

      <div v-if="totalPages > 1" class="flex justify-center">
        <KunPagination
          v-model:current-page="currentPage"
          :total-page="totalPages"
          :is-loading="isLoading"
        />
      </div>
    </template>

    <ImagesDeleteModal
      v-model="delOpen"
      :hash="delHash"
      :force="delForce"
      @deleted="onDeleted"
    />
  </div>
</template>
