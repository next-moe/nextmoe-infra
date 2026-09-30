<script setup lang="ts">
import {
  TELEMETRY_ISSUE_KINDS,
  TELEMETRY_KIND_META,
  TELEMETRY_ISSUE_STATUSES,
  TELEMETRY_ISSUE_STATUS_META,
  TELEMETRY_WINDOW_OPTIONS,
  TELEMETRY_DEFAULT_WINDOW,
  TELEMETRY_ISSUE_SORT_OPTIONS,
  telemetryWindowRange,
  telemetryPageCount,
  type TelemetryWindowDays
} from '~/constants/telemetry'
import type { TelemetryIssue } from '~~/shared/types/telemetry'

const {
  apps,
  selectedId,
  appModel,
  options,
  error: appsError,
  refresh: refreshApps,
  isLoading: appsLoading
} = await useTelemetryApps()

const kind = ref('')
const issueStatus = ref('open')
const version = ref('')
const windowDays = ref<TelemetryWindowDays>(TELEMETRY_DEFAULT_WINDOW)
const sort = ref('events')
const page = ref(1)
const limit = 50

watch([selectedId, kind, issueStatus, version, windowDays, sort], () => {
  page.value = 1
})

const range = computed(() => telemetryWindowRange(windowDays.value))
const offset = computed(() => (page.value - 1) * limit)

const kindOptions = computed(() => [
  { value: '', label: '全部类型' },
  ...TELEMETRY_ISSUE_KINDS.map((k) => ({
    value: k,
    label: TELEMETRY_KIND_META[k].label
  }))
])

const statusOptions = TELEMETRY_ISSUE_STATUSES.map((s) => ({
  value: s,
  label: TELEMETRY_ISSUE_STATUS_META[s].label
}))

const { data, status, error, refresh } = await useApiFetch<TelemetryIssue[]>(
  '/admin/telemetry/issues',
  {
    query: computed(() => ({
      app_id: selectedId.value,
      kind: kind.value || undefined,
      status: issueStatus.value,
      version: version.value.trim() || undefined,
      from: range.value.from,
      to: range.value.to,
      sort: sort.value,
      limit,
      offset: offset.value
    })),
    immediate: false
  },
  'telemetry'
)

watch(
  [selectedId, kind, issueStatus, version, range, sort, offset],
  () => {
    if (selectedId.value > 0) refresh()
  },
  { immediate: true }
)

const items = computed(() => data.value ?? [])
const isLoading = computed(
  () => appsLoading.value || status.value === 'pending'
)
const totalPages = computed(() =>
  telemetryPageCount(page.value, limit, items.value.length)
)
</script>

<template>
  <div class="space-y-5">
    <h1 class="text-foreground text-2xl font-bold">问题</h1>

    <div class="flex flex-wrap items-end gap-2">
      <TelemetryAppPicker v-model="appModel" :options="options" />
      <KunSelect
        v-model="kind"
        :options="kindOptions"
        label="类型"
        aria-label="类型"
        :full-width="false"
        class="w-40"
      />
      <KunSelect
        v-model="issueStatus"
        :options="statusOptions"
        label="状态"
        aria-label="状态"
        :full-width="false"
        class="w-32"
      />
      <KunInput
        v-model="version"
        label="版本"
        placeholder="全部版本"
        class="w-40"
      />
      <KunSelect
        v-model="windowDays"
        :options="[...TELEMETRY_WINDOW_OPTIONS]"
        label="窗口"
        aria-label="窗口"
        :full-width="false"
        class="w-32"
      />
      <KunSelect
        v-model="sort"
        :options="[...TELEMETRY_ISSUE_SORT_OPTIONS]"
        label="排序"
        aria-label="排序"
        :full-width="false"
        class="w-32"
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

    <div
      v-else-if="isLoading && !items.length"
      class="flex justify-center py-12"
    >
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <KunCard
      v-else-if="!apps.length"
      content-class="justify-start gap-0"
      class-name="py-12 text-center"
    >
      <p class="text-default-400">还没有监测应用。</p>
    </KunCard>

    <template v-else>
      <TelemetryIssuesTable :items="items" :loading="isLoading" />
      <div v-if="totalPages > 1" class="flex justify-center">
        <KunPagination
          v-model:current-page="page"
          :total-page="totalPages"
          :is-loading="isLoading"
        />
      </div>
    </template>
  </div>
</template>
