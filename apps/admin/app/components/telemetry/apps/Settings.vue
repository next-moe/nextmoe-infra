<script setup lang="ts">
import {
  TELEMETRY_ALERT_SETTING_FIELDS,
  TELEMETRY_ALERT_DEFAULTS,
  type TelemetryAlertSettingKey
} from '~/constants/telemetry'
import type {
  TelemetryAlertSettingsInput,
  TelemetryApp
} from '~~/shared/types/telemetry'

const props = defineProps<{ app: TelemetryApp }>()
const emit = defineEmits<{ changed: [] }>()

const api = useApi('telemetry')
const prefixesText = ref('')
const values = ref<Record<TelemetryAlertSettingKey, number | null>>({
  crash_rate: null,
  anr_rate: null,
  min_sessions: null,
  regression_factor: null,
  regression_min_delta: null,
  server_faults_per_hour: null,
  silent_hours: null,
  silent_min_daily_sessions: null
})
const saving = ref(false)

const sync = () => {
  prefixesText.value = props.app.in_app_prefixes.join('\n')
  const s = props.app.alert_settings
  values.value = {
    crash_rate: s.crash_rate * 100,
    anr_rate: s.anr_rate * 100,
    min_sessions: s.min_sessions,
    regression_factor: s.regression_factor,
    regression_min_delta: s.regression_min_delta * 100,
    server_faults_per_hour: s.server_faults_per_hour,
    silent_hours: s.silent_hours,
    silent_min_daily_sessions: s.silent_min_daily_sessions
  }
}

watch(
  () => [props.app.id, props.app.updated_at],
  () => sync(),
  { immediate: true }
)

const parsePrefixes = () => {
  const lines = prefixesText.value
    .split('\n')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
  if (lines.length > 20) {
    useKunMessage('应用内前缀最多 20 条', 'warn')
    return null
  }
  if (lines.some((s) => s.length > 100)) {
    useKunMessage('每条前缀须为 1–100 个字符', 'warn')
    return null
  }
  return lines
}

const n = (raw: number | null, fallback: number) =>
  raw == null || !Number.isFinite(raw) ? fallback : raw

const save = async () => {
  const prefixes = parsePrefixes()
  if (!prefixes) return
  const v = values.value
  const cur = props.app.alert_settings
  const alertSettings: TelemetryAlertSettingsInput = {
    crash_rate: n(v.crash_rate, cur.crash_rate * 100) / 100,
    anr_rate: n(v.anr_rate, cur.anr_rate * 100) / 100,
    min_sessions: n(v.min_sessions, cur.min_sessions),
    regression_factor: n(v.regression_factor, cur.regression_factor),
    regression_min_delta:
      n(v.regression_min_delta, cur.regression_min_delta * 100) / 100,
    server_faults_per_hour: n(
      v.server_faults_per_hour,
      cur.server_faults_per_hour
    ),
    silent_hours: n(v.silent_hours, cur.silent_hours),
    silent_min_daily_sessions: n(
      v.silent_min_daily_sessions,
      cur.silent_min_daily_sessions
    )
  }
  saving.value = true
  try {
    const res = await api.patch(`/admin/telemetry/apps/${props.app.id}`, {
      in_app_prefixes: prefixes,
      alert_settings: alertSettings
    })
    if (res.code === 0) {
      useKunMessage('已保存', 'success')
      emit('changed')
    } else {
      useKunMessage(res.message || '保存失败', 'error')
    }
  } finally {
    saving.value = false
  }
}

const setValue = (key: TelemetryAlertSettingKey, n: number | null) => {
  values.value = { ...values.value, [key]: n }
}

const defaultHint = (key: TelemetryAlertSettingKey) => {
  const d = TELEMETRY_ALERT_DEFAULTS[key]
  if (
    key === 'crash_rate' ||
    key === 'anr_rate' ||
    key === 'regression_min_delta'
  ) {
    return `${d * 100}`
  }
  return String(d)
}
</script>

<template>
  <div class="space-y-4">
    <KunTextarea
      v-model="prefixesText"
      label="应用内前缀"
      description="每行一条，最多 20 条，用于识别应用代码帧。例如 package:kungal/"
      :rows="4"
    />
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <KunNumberInput
        v-for="field in TELEMETRY_ALERT_SETTING_FIELDS"
        :key="field.key"
        :model-value="values[field.key]"
        :label="field.kind === 'percent' ? `${field.label}（%）` : field.label"
        :description="field.hint"
        :placeholder="field.placeholder"
        :min="field.min"
        :max="field.max"
        :step="field.step"
        :precision="field.kind === 'int' ? 0 : 4"
        @update:model-value="(n: number | null) => setValue(field.key, n)"
      />
    </div>
    <p class="text-default-400 text-xs">
      占位符为系统默认值：崩溃率 {{ defaultHint('crash_rate') }}%，ANR 率
      {{ defaultHint('anr_rate') }}%，最少会话
      {{ defaultHint('min_sessions') }}。
    </p>
    <KunButton color="primary" variant="flat" :loading="saving" @click="save">
      保存前缀与告警阈值
    </KunButton>
  </div>
</template>
