<script setup lang="ts">
import type { PreferenceSummary } from '~~/shared/types/preferences'
import { PREFERENCE_GLOBAL_NAMESPACE } from '~/constants/preferences'

const { listPreferences, deletePreference } = usePreferences()

const rows = ref<PreferenceSummary[]>([])
const error = ref('')
const isLoading = ref(true)
const deleting = ref<string | null>(null)

const load = async () => {
  isLoading.value = true
  try {
    const response = await listPreferences()
    if (response.code === 0) {
      rows.value = response.data ?? []
      error.value = ''
    } else {
      error.value = response.message || '读取失败'
    }
  } finally {
    isLoading.value = false
  }
}

onMounted(load)

const namespaceLabel = (namespace: string) =>
  namespace === PREFERENCE_GLOBAL_NAMESPACE ? '全站通用' : namespace

const handleDelete = async (row: PreferenceSummary) => {
  if (deleting.value) return
  const confirmed = await useKunAlert({
    title: '删除应用数据',
    message: `将永久删除「${namespaceLabel(row.namespace)}」保存的偏好数据（${formatPreferenceSize(row.size_bytes)}）。该应用下次会从空白状态重新开始，此操作不可撤销。`,
    showCancel: true,
    confirmText: '删除',
    cancelText: '取消',
    type: 'danger'
  })
  if (!confirmed) return

  deleting.value = row.namespace
  try {
    const response = await deletePreference(row.namespace)
    if (response.code === 0) {
      rows.value = rows.value.filter((r) => r.namespace !== row.namespace)
      useKunMessage('已删除', 'success')
    } else {
      error.value = response.message || '删除失败'
    }
  } finally {
    deleting.value = null
  }
}
</script>

<template>
  <KunCard class="p-6">
    <h3 class="text-foreground mb-1 text-lg font-semibold">
      <KunIcon name="lucide:hard-drive" class="mr-2 inline size-5" />
      应用数据
    </h3>
    <p class="text-default-500 mb-4 text-sm">
      已接入的应用可以把你的界面偏好存在账号里，换设备后自动跟随。这里列出它们各自存了多少。
    </p>

    <div
      v-if="isLoading"
      class="text-default-400 flex items-center justify-center gap-2 py-8 text-sm"
    >
      <KunIcon name="lucide:loader-circle" class="size-4 animate-spin" />
      <span>加载中...</span>
    </div>

    <p
      v-else-if="!rows.length"
      class="text-default-400 border-default-200 rounded-lg border border-dashed py-8 text-center text-sm"
    >
      还没有应用保存过数据
    </p>

    <ul v-else class="divide-default-200 divide-y">
      <li
        v-for="row in rows"
        :key="row.namespace"
        class="flex items-center gap-3 py-3"
      >
        <div class="min-w-0 flex-1">
          <p class="text-foreground truncate text-sm font-medium">
            {{ namespaceLabel(row.namespace) }}
          </p>
          <p class="text-default-400 mt-0.5 truncate text-xs">
            {{ formatPreferenceSize(row.size_bytes) }} · 第
            {{ row.version }} 次写入 ·
            {{ formatPreferenceTime(row.updated_at) }}
          </p>
        </div>
        <KunButton
          variant="light"
          color="danger"
          size="sm"
          is-icon-only
          aria-label="删除这份应用数据"
          :disabled="deleting === row.namespace"
          @click="handleDelete(row)"
        >
          <KunIcon
            :name="
              deleting === row.namespace
                ? 'lucide:loader-circle'
                : 'lucide:trash-2'
            "
            :class="
              deleting === row.namespace ? 'size-4 animate-spin' : 'size-4'
            "
          />
        </KunButton>
      </li>
    </ul>

    <div
      v-if="error"
      class="bg-danger-50 text-danger mt-4 rounded-lg p-3 text-sm"
    >
      {{ error }}
    </div>
  </KunCard>
</template>
