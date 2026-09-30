<script setup lang="ts">
import type {
  TelemetryApp,
  TelemetrySymbolsToken
} from '~~/shared/types/telemetry'

const props = defineProps<{ app: TelemetryApp }>()
const emit = defineEmits<{
  changed: []
  token: [string]
}>()

const api = useApi('telemetry')
const busy = ref('')

const setEnabled = async (enabled: boolean) => {
  busy.value = 'enabled'
  try {
    const res = await api.patch(`/admin/telemetry/apps/${props.app.id}`, {
      enabled
    })
    if (res.code === 0) {
      useKunMessage(enabled ? '已启用' : '已停用', 'success')
      emit('changed')
    } else {
      useKunMessage(res.message || '更新失败', 'error')
    }
  } finally {
    busy.value = ''
  }
}

const rotateKey = async () => {
  const confirmed = await useKunAlert({
    title: '轮换上报密钥？',
    message:
      '该密钥写进每一份已发布的安装包。轮换后，所有已安装版本都会立刻停止上报，直到发出使用新密钥的新包。',
    type: 'danger',
    confirmText: '轮换',
    confirmColor: 'danger'
  })
  if (!confirmed) return
  busy.value = 'key'
  try {
    const res = await api.post<TelemetryApp>(
      `/admin/telemetry/apps/${props.app.id}/rotate-key`
    )
    if (res.code === 0) {
      useKunMessage('密钥已轮换', 'success')
      emit('changed')
    } else {
      useKunMessage(res.message || '轮换失败', 'error')
    }
  } finally {
    busy.value = ''
  }
}

const rotateToken = async () => {
  const confirmed = await useKunAlert({
    title: props.app.has_symbols_token ? '轮换 CI 令牌？' : '生成 CI 令牌？',
    message: props.app.has_symbols_token
      ? '旧令牌立即失效，正在跑的 CI 需要换成新令牌才能继续上传符号。'
      : '将生成仅显示一次的明文令牌，请立刻写入 CI 的 TELEMETRY_SYMBOLS_TOKEN。',
    type: 'warning',
    confirmText: props.app.has_symbols_token ? '轮换' : '生成',
    confirmColor: 'warning'
  })
  if (!confirmed) return
  busy.value = 'token'
  try {
    const res = await api.post<TelemetrySymbolsToken>(
      `/admin/telemetry/apps/${props.app.id}/rotate-symbols-token`
    )
    if (res.code === 0 && res.data?.token) {
      emit('token', res.data.token)
      emit('changed')
    } else {
      useKunMessage(res.message || '操作失败', 'error')
    }
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <KunCard content-class="justify-start items-stretch gap-0" class-name="p-4">
    <div class="space-y-4">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <h2 class="text-foreground text-lg font-semibold">
            {{ app.display_name }}
          </h2>
          <p class="text-default-500 font-mono text-sm">
            {{ app.service_name }}
          </p>
        </div>
        <KunSwitch
          :model-value="app.enabled"
          :disabled="busy === 'enabled'"
          label="启用"
          @update:model-value="(v: boolean) => setEnabled(v)"
        />
      </div>

      <div class="space-y-2">
        <p class="text-default-500 text-xs">上报密钥</p>
        <div class="flex flex-wrap items-start gap-2">
          <p class="text-foreground min-w-0 flex-1 font-mono text-sm break-all">
            {{ app.ingest_key }}
          </p>
          <KunCopy :text="app.ingest_key" name="复制" size="sm" />
          <KunButton
            size="sm"
            variant="flat"
            color="danger"
            :loading="busy === 'key'"
            :disabled="!!busy"
            @click="rotateKey"
          >
            轮换
          </KunButton>
        </div>
      </div>

      <div class="space-y-2">
        <p class="text-default-500 text-xs">CI 符号令牌</p>
        <div class="flex flex-wrap items-center gap-2">
          <KunChip
            :color="app.has_symbols_token ? 'success' : 'default'"
            variant="flat"
            size="xs"
          >
            {{ app.has_symbols_token ? '已配置' : '未生成' }}
          </KunChip>
          <KunButton
            size="sm"
            variant="flat"
            :loading="busy === 'token'"
            :disabled="!!busy"
            @click="rotateToken"
          >
            {{ app.has_symbols_token ? '轮换' : '生成' }}
          </KunButton>
        </div>
      </div>

      <TelemetryAppsSettings :app="app" @changed="emit('changed')" />
      <TelemetryAppsUploads :app-id="app.id" />
    </div>
  </KunCard>
</template>
