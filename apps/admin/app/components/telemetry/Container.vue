<script setup lang="ts">
import {
  TELEMETRY_ENVIRONMENTS,
  TELEMETRY_ENVIRONMENT_LABELS,
  TELEMETRY_WINDOW_OPTIONS,
  TELEMETRY_DEFAULT_WINDOW,
  telemetryWindowRange,
  type TelemetryEnvironment,
  type TelemetryWindowDays
} from '~/constants/telemetry'
import type { TelemetryDailyMetric } from '~~/shared/types/telemetry'

const {
  apps,
  selected,
  selectedId,
  appModel,
  options,
  error: appsError,
  refresh: refreshApps,
  isLoading: appsLoading
} = await useTelemetryApps()

const environment = ref<TelemetryEnvironment>('direct')
const windowDays = ref<TelemetryWindowDays>(TELEMETRY_DEFAULT_WINDOW)
const version = ref('')

const range = computed(() => telemetryWindowRange(windowDays.value))

const { data, status, error, refresh } = await useApiFetch<
  TelemetryDailyMetric[]
>(
  '/admin/telemetry/metrics/daily',
  {
    query: computed(() => ({
      app_id: selectedId.value,
      environment: environment.value,
      from: range.value.from,
      to: range.value.to
    })),
    immediate: false
  },
  'telemetry'
)

watch(
  [selectedId, environment, range],
  () => {
    if (selectedId.value > 0) refresh()
  },
  { immediate: true }
)

watch([selectedId, environment, windowDays], () => {
  version.value = ''
})

const allRows = computed(() => data.value ?? [])
const rows = computed(() => {
  if (!version.value) return allRows.value
  return allRows.value.filter((r) => r.service_version === version.value)
})

const versionOptions = computed(() => {
  const vs = [...new Set(allRows.value.map((r) => r.service_version))].sort()
  return [
    { value: '', label: '全部版本' },
    ...vs.map((v) => ({ value: v, label: v }))
  ]
})

const envOptions = TELEMETRY_ENVIRONMENTS.map((v) => ({
  value: v,
  label: TELEMETRY_ENVIRONMENT_LABELS[v]
}))

const isLoading = computed(
  () => appsLoading.value || status.value === 'pending'
)

const weightedMs = (
  list: TelemetryDailyMetric[],
  pick: (r: TelemetryDailyMetric) => number | null
) => {
  let num = 0
  let den = 0
  for (const r of list) {
    const v = pick(r)
    if (v == null || r.startup_count <= 0) continue
    num += v * r.startup_count
    den += r.startup_count
  }
  return den === 0 ? null : num / den
}

const kpis = computed(() => {
  const list = rows.value
  const sessions = list.reduce((s, r) => s + r.sessions, 0)
  const crashed = list.reduce((s, r) => s + r.crashed_sessions, 0)
  const anr = list.reduce((s, r) => s + r.anr_sessions, 0)
  const unhandled = list.reduce((s, r) => s + r.unhandled_sessions, 0)
  const jankOver = list.reduce((s, r) => s + r.jank_frames_over, 0)
  const jankTotal = list.reduce((s, r) => s + r.jank_frames_total, 0)
  return {
    sessions,
    crashFree: sessions === 0 ? null : 1 - crashed / sessions,
    anrRate: sessions === 0 ? null : anr / sessions,
    unhandledRate: sessions === 0 ? null : unhandled / sessions,
    ttidP50: weightedMs(list, (r) => r.ttid_p50_ms),
    ttidP90: weightedMs(list, (r) => r.ttid_p90_ms),
    jankRatio: jankTotal === 0 ? null : jankOver / jankTotal
  }
})
</script>

<template>
  <div class="space-y-5">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
    >
      <h1 class="text-foreground text-2xl font-bold">应用监测</h1>
      <div class="flex flex-wrap items-end gap-2">
        <TelemetryAppPicker v-model="appModel" :options="options" />
        <KunSelect
          v-model="environment"
          :options="envOptions"
          label="环境"
          aria-label="环境"
          :full-width="false"
          class="w-36"
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
          v-model="version"
          :options="versionOptions"
          label="版本"
          aria-label="版本"
          :full-width="false"
          class="w-40"
        />
      </div>
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

    <div v-else-if="appsLoading" class="flex justify-center py-12">
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
      <p class="text-default-400">还没有监测应用。请到「应用与密钥」创建。</p>
    </KunCard>

    <template v-else>
      <div v-if="isLoading" class="flex justify-center py-8">
        <KunIcon
          name="lucide:loader-circle"
          class="text-primary size-8 animate-spin"
        />
      </div>
      <template v-else-if="!allRows.length">
        <KunCard
          content-class="justify-start gap-0"
          class-name="py-12 text-center"
        >
          <p class="text-default-400">
            该应用在此窗口内还没有上报。安装包接入监测并放出后，这里会出现会话与崩溃趋势。
          </p>
        </KunCard>
      </template>
      <template v-else>
        <TelemetryKpis :kpis="kpis" />
        <TelemetryVersionTable
          :rows="allRows"
          :min-sessions="selected?.alert_settings.min_sessions ?? 200"
          :crash-threshold="selected?.alert_settings.crash_rate ?? 0.0109"
          :anr-threshold="selected?.alert_settings.anr_rate ?? 0.0047"
        />
        <TelemetryTrend :rows="rows" :range="range" />
      </template>
    </template>
  </div>
</template>
