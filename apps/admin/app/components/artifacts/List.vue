<script setup lang="ts">
import type {
  ArtifactAdminListResponse,
  ArtifactAdminRow
} from '~~/shared/types/artifact'
import {
  ARTIFACT_STATUS_MAP,
  ARTIFACT_STATUS_TABS
} from '~~/shared/types/artifact'

const api = useApi()

const page = ref(1)
const limit = 50
const status = ref('')
const site = ref('')
const search = ref('')
const searchInput = ref('')

let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(searchInput, (v) => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    search.value = v.trim()
    page.value = 1
  }, 400)
})
watch([status, site], () => {
  page.value = 1
})

const { data, status: fetchStatus, refresh } =
  await useApiFetch<ArtifactAdminListResponse>('/admin/artifact/list', {
    query: computed(() => ({
      page: page.value,
      limit,
      ...(status.value ? { status: status.value } : {}),
      ...(site.value ? { site: site.value } : {}),
      ...(search.value ? { search: search.value } : {})
    }))
  })

const items = computed(() => data.value?.items ?? [])
const total = computed(() => data.value?.total ?? 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / limit)))
const isLoading = computed(() => fetchStatus.value === 'pending')

const fmtTime = (iso: string) => iso.slice(0, 19).replace('T', ' ')

// hydration mismatch on a relative time.
const nowTs = ref(0)
onMounted(() => {
  nowTs.value = Date.now()
})
const idleSince = (iso: string) => {
  if (!nowTs.value) return '…'
  const ms = nowTs.value - new Date(iso).getTime()
  if (!Number.isFinite(ms) || ms < 0) return '—'
  const m = Math.floor(ms / 60000)
  if (m < 60) return `${m} 分钟`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时 ${m % 60} 分`
  return `${Math.floor(h / 24)} 天 ${h % 24} 小时`
}

const reclaimOpen = ref(false)
const reclaimRow = ref<ArtifactAdminRow | null>(null)
const reclaimLoading = ref(false)
const askReclaim = (row: ArtifactAdminRow) => {
  reclaimRow.value = row
  reclaimOpen.value = true
}
const confirmReclaim = async () => {
  if (!reclaimRow.value) return
  reclaimLoading.value = true
  try {
    const res = await api.post(`/admin/artifact/${reclaimRow.value.uuid}/reclaim`, {})
    if (res.code === 0) {
      useKunMessage('已回收该上传（已中止分片并清理）', 'success')
      await refresh()
    } else {
      useKunMessage(res.message || '回收失败', 'error')
    }
  } finally {
    reclaimLoading.value = false
    reclaimOpen.value = false
  }
}

const delOpen = ref(false)
const delRow = ref<ArtifactAdminRow | null>(null)
const delLoading = ref(false)
const askDelete = (row: ArtifactAdminRow) => {
  delRow.value = row
  delOpen.value = true
}
const confirmDelete = async () => {
  if (!delRow.value) return
  delLoading.value = true
  try {
    const res = await api.delete(`/admin/artifact/${delRow.value.uuid}`)
    if (res.code === 0) {
      useKunMessage('文件已软删除', 'success')
      await refresh()
    } else {
      useKunMessage(res.message || '删除失败', 'error')
    }
  } finally {
    delLoading.value = false
    delOpen.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-foreground text-2xl font-bold">文件列表</h1>

    <div class="flex flex-wrap items-center gap-2">
      <KunButton
        v-for="tab in ARTIFACT_STATUS_TABS"
        :key="tab.id"
        :color="status === tab.id ? 'primary' : 'default'"
        :variant="status === tab.id ? 'solid' : 'flat'"
        size="sm"
        @click="status = tab.id"
      >
        <KunIcon :name="tab.icon" class="mr-1 size-4" />
        {{ tab.label }}
      </KunButton>
      <KunInput
        v-model="site"
        type="text"
        placeholder="按 site_key 过滤…"
        class="w-40"
      />
      <KunInput
        v-model="searchInput"
        type="text"
        placeholder="搜索文件名或 UUID…"
        class="min-w-48 flex-1"
      />
    </div>

    <p class="text-default-500 text-sm">共 {{ total }} 个文件</p>

    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[64rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">文件名 / UUID</th>
            <th class="px-3 py-2 text-left font-medium">站点</th>
            <th class="px-3 py-2 text-right font-medium">大小</th>
            <th class="px-3 py-2 text-left font-medium">状态</th>
            <th class="px-3 py-2 text-left font-medium">上传者</th>
            <th class="px-3 py-2 text-left font-medium">创建时间</th>
            <th class="px-3 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="item.uuid"
            class="border-default-200 border-t align-top"
          >
            <td class="px-3 py-2">
              <div class="text-foreground max-w-[22rem] truncate font-medium">
                {{ item.name }}
              </div>
              <div class="text-default-400 flex items-center gap-1 font-mono text-xs">
                <span class="truncate">{{ item.uuid }}</span>
                <KunCopy :text="item.uuid" />
              </div>
            </td>
            <td class="px-3 py-2 font-mono text-xs">{{ item.site_key }}</td>
            <td class="px-3 py-2 text-right whitespace-nowrap">
              {{ formatFileSize(item.file_size) }}
            </td>
            <td class="px-3 py-2">
              <KunChip
                :color="ARTIFACT_STATUS_MAP[item.status]?.color || 'default'"
                variant="flat"
                size="xs"
              >
                {{ ARTIFACT_STATUS_MAP[item.status]?.label || item.status }}
              </KunChip>
              <div
                v-if="item.status === 'uploading'"
                class="text-default-400 mt-1 text-xs"
              >
                闲置 {{ idleSince(item.updated_at) }}
              </div>
            </td>
            <td class="px-3 py-2">
              <div class="text-default-500 max-w-[12rem] truncate text-xs">
                {{ item.uploader_sub || '—' }}
              </div>
              <div class="text-default-400 max-w-[12rem] truncate text-xs">
                {{ item.uploader_client }}
              </div>
            </td>
            <td class="text-default-500 px-3 py-2 whitespace-nowrap">
              {{ fmtTime(item.created_at) }}
            </td>
            <td class="px-3 py-2 text-right">
              <KunButton
                v-if="item.status === 'uploading'"
                color="warning"
                variant="flat"
                size="sm"
                @click="askReclaim(item)"
              >
                <KunIcon name="lucide:eraser" class="mr-1 size-4" />
                回收
              </KunButton>
              <KunButton
                v-else
                color="danger"
                variant="flat"
                size="sm"
                @click="askDelete(item)"
              >
                <KunIcon name="lucide:trash-2" class="size-4" />
              </KunButton>
            </td>
          </tr>
          <tr v-if="!items.length">
            <td colspan="7" class="text-default-400 px-3 py-10 text-center">
              没有匹配的文件
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

    <KunModal v-model="delOpen" aria-label="软删除文件">
      <div class="space-y-4">
        <h2 class="text-foreground text-xl font-bold">软删除文件</h2>
        <p class="text-default-500 text-sm">
          将软删除
          <span class="text-foreground font-medium">{{ delRow?.name }}</span>
          （状态置为已删除，对应站点立即无法下载；B2 实际对象由 artifact GC
          在保留期后回收）。此操作可由 GC 回收前的数据库恢复，但请谨慎。
        </p>
        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" @click="delOpen = false">
            取消
          </KunButton>
          <KunButton color="danger" :disabled="delLoading" @click="confirmDelete">
            <KunIcon
              v-if="delLoading"
              name="lucide:loader-circle"
              class="mr-2 size-4 animate-spin"
            />
            确认软删除
          </KunButton>
        </div>
      </div>
    </KunModal>

    <KunModal v-model="reclaimOpen" aria-label="回收上传中的文件">
      <div class="space-y-4">
        <h2 class="text-foreground text-xl font-bold">回收上传中的文件</h2>
        <p class="text-default-500 text-sm">
          将立即回收
          <span class="text-foreground font-medium">{{ reclaimRow?.name }}</span>
          （一个未完成的上传，闲置 {{ reclaimRow ? idleSince(reclaimRow.updated_at) : '' }}）：
          中止其 B2 分片上传、删除已传字节、并移除记录。
          <span class="text-danger font-medium">此操作不可恢复</span>，
          且会中断仍在进行 / 可续传的上传。若该上传仍活跃（闲置时间过短），服务器会拒绝。
        </p>
        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" @click="reclaimOpen = false">
            取消
          </KunButton>
          <KunButton color="warning" :disabled="reclaimLoading" @click="confirmReclaim">
            <KunIcon
              v-if="reclaimLoading"
              name="lucide:loader-circle"
              class="mr-2 size-4 animate-spin"
            />
            确认回收
          </KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
