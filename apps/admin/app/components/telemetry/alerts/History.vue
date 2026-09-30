<script setup lang="ts">
import {
  TELEMETRY_ALERT_STATUSES,
  TELEMETRY_ALERT_STATUS_META,
  telemetryAlertRuleLabel,
  telemetryAlertStatusMeta,
  telemetryPageCount
} from '~/constants/telemetry'
import type { TelemetryAlert } from '~~/shared/types/telemetry'

const {
  apps,
  options,
  error: appsError,
  refresh: refreshApps
} = await useTelemetryApps()

const appId = ref('')
const alertStatus = ref('')
const page = ref(1)
const limit = 50

watch([appId, alertStatus], () => {
  page.value = 1
})

const appOptions = computed(() => [
  { value: '', label: '全部应用' },
  { value: '0', label: '引擎符号' },
  ...options.value
])

const statusOptions = [
  { value: '', label: '全部状态' },
  ...TELEMETRY_ALERT_STATUSES.map((s) => ({
    value: s,
    label: TELEMETRY_ALERT_STATUS_META[s].label
  }))
]

const offset = computed(() => (page.value - 1) * limit)

const { data, status, error, refresh } = await useApiFetch<TelemetryAlert[]>(
  '/admin/telemetry/alerts',
  {
    query: computed(() => ({
      app_id: appId.value || undefined,
      status: alertStatus.value || undefined,
      limit,
      offset: offset.value
    }))
  },
  'telemetry'
)

const items = computed(() => data.value ?? [])
const isLoading = computed(() => status.value === 'pending')
const totalPages = computed(() =>
  telemetryPageCount(page.value, limit, items.value.length)
)

const appName = (id: number) => {
  if (id === 0) return '引擎符号'
  return apps.value.find((a) => a.id === id)?.display_name ?? `#${id}`
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-end gap-2">
      <KunSelect
        v-model="appId"
        :options="appOptions"
        label="应用"
        aria-label="应用"
        :full-width="false"
        class="w-56"
      />
      <KunSelect
        v-model="alertStatus"
        :options="statusOptions"
        label="状态"
        aria-label="状态"
        :full-width="false"
        class="w-36"
      />
    </div>

    <CommonFetchError
      v-if="appsError"
      :message="appsError.message"
      @retry="refreshApps"
    />
    <CommonFetchError
      v-else-if="error"
      :message="error.message"
      @retry="refresh"
    />

    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[56rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">时间</th>
            <th class="px-3 py-2 text-left font-medium">应用</th>
            <th class="px-3 py-2 text-left font-medium">规则</th>
            <th class="px-3 py-2 text-left font-medium">标题</th>
            <th class="px-3 py-2 text-left font-medium">状态</th>
            <th class="px-3 py-2 text-right font-medium">尝试</th>
            <th class="px-3 py-2 text-left font-medium">最后错误</th>
            <th class="px-3 py-2 text-left font-medium">发送时间</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in items"
            :key="row.id"
            class="border-default-200 border-t align-top"
          >
            <td class="text-default-500 px-3 py-2 whitespace-nowrap">
              {{ new Date(row.created_at).toLocaleString('zh-CN') }}
            </td>
            <td class="px-3 py-2">{{ appName(row.app_id) }}</td>
            <td class="px-3 py-2">
              {{ telemetryAlertRuleLabel(row.rule) }}
            </td>
            <td class="px-3 py-2 break-all">{{ row.title }}</td>
            <td class="px-3 py-2">
              <KunChip
                :color="telemetryAlertStatusMeta(row.status).color"
                variant="flat"
                size="xs"
              >
                {{ telemetryAlertStatusMeta(row.status).label }}
              </KunChip>
            </td>
            <td class="px-3 py-2 text-right tabular-nums">
              {{ row.attempts }}
            </td>
            <td
              class="text-danger-600 max-w-[16rem] truncate px-3 py-2 text-xs"
            >
              {{ row.last_error || '—' }}
            </td>
            <td class="text-default-500 px-3 py-2 whitespace-nowrap">
              {{
                row.sent_at
                  ? new Date(row.sent_at).toLocaleString('zh-CN')
                  : '—'
              }}
            </td>
          </tr>
          <tr v-if="!items.length">
            <td colspan="8" class="text-default-400 px-3 py-10 text-center">
              {{ isLoading ? '加载中…' : '没有告警记录' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="totalPages > 1" class="flex justify-center">
      <KunPagination
        v-model:current-page="page"
        :total-page="totalPages"
        :is-loading="isLoading"
      />
    </div>
  </div>
</template>
