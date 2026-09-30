<script setup lang="ts">
import type {
  TelemetryAlertChannel,
  TelemetryAlertChannelTestResult
} from '~~/shared/types/telemetry'

const api = useApi('telemetry')
const { data, status, error, refresh } = await useApiFetch<
  TelemetryAlertChannel[]
>('/admin/telemetry/alert-channels', {}, 'telemetry')

const channels = computed(() => data.value ?? [])
const isLoading = computed(() => status.value === 'pending')

const target = ref('')
const creating = ref(false)
const busyId = ref(0)

const emailOk = (s: string) =>
  s.length > 0 && s.length <= 254 && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s)

const create = async () => {
  const t = target.value.trim()
  if (!emailOk(t)) {
    useKunMessage('请填写有效的邮箱地址', 'warn')
    return
  }
  creating.value = true
  try {
    const res = await api.post<TelemetryAlertChannel>(
      '/admin/telemetry/alert-channels',
      { kind: 'email', target: t }
    )
    if (res.code === 0) {
      useKunMessage('已添加', 'success')
      target.value = ''
      await refresh()
    } else {
      useKunMessage(res.message || '添加失败', 'error')
    }
  } finally {
    creating.value = false
  }
}

const setEnabled = async (ch: TelemetryAlertChannel, enabled: boolean) => {
  busyId.value = ch.id
  try {
    const res = await api.patch(`/admin/telemetry/alert-channels/${ch.id}`, {
      enabled
    })
    if (res.code === 0) {
      useKunMessage(enabled ? '已启用' : '已停用', 'success')
      await refresh()
    } else {
      useKunMessage(res.message || '更新失败', 'error')
    }
  } finally {
    busyId.value = 0
  }
}

const test = async (ch: TelemetryAlertChannel) => {
  busyId.value = ch.id
  try {
    const res = await api.post<TelemetryAlertChannelTestResult>(
      `/admin/telemetry/alert-channels/${ch.id}/test`
    )
    if (res.code !== 0) {
      useKunMessage(res.message || '测试失败', 'error')
      return
    }
    const body = res.data
    if (body?.ok) {
      useKunMessage(
        `发送成功（ok=${body.ok}, error=${body.error || '无'}）`,
        'success'
      )
    } else {
      useKunMessage(
        `发送失败（ok=${body?.ok ?? false}, error=${body?.error || '未知'}）`,
        'error'
      )
    }
  } finally {
    busyId.value = 0
  }
}

const remove = async (ch: TelemetryAlertChannel) => {
  const confirmed = await useKunAlert({
    title: `删除渠道「${ch.target}」？`,
    message: '删除后该地址不再收到监测告警。',
    type: 'danger',
    confirmText: '删除',
    confirmColor: 'danger'
  })
  if (!confirmed) return
  busyId.value = ch.id
  try {
    const res = await api.delete(`/admin/telemetry/alert-channels/${ch.id}`)
    if (res.code === 0) {
      useKunMessage('已删除', 'success')
      await refresh()
    } else {
      useKunMessage(res.message || '删除失败', 'error')
    }
  } finally {
    busyId.value = 0
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
      <KunInput
        v-model="target"
        label="邮箱"
        placeholder="ops@example.com"
        class="sm:max-w-sm sm:flex-1"
      />
      <KunButton color="primary" :loading="creating" @click="create">
        添加
      </KunButton>
    </div>

    <CommonFetchError v-if="error" :message="error.message" @retry="refresh" />

    <div v-else-if="isLoading" class="flex justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <KunCard
      v-else-if="!channels.length"
      content-class="justify-start gap-0"
      class-name="py-12 text-center"
    >
      <p class="text-default-400">还没有告警渠道。</p>
    </KunCard>

    <div v-else class="space-y-2">
      <div
        v-for="ch in channels"
        :key="ch.id"
        class="border-default-200 bg-content1 flex flex-wrap items-center justify-between gap-3 rounded-xl border p-4"
      >
        <div class="min-w-0">
          <p class="text-foreground break-all">{{ ch.target }}</p>
          <p class="text-default-400 text-xs">{{ ch.kind }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <KunSwitch
            :model-value="ch.enabled"
            :disabled="busyId === ch.id"
            label="启用"
            @update:model-value="(v: boolean) => setEnabled(ch, v)"
          />
          <KunButton
            size="sm"
            variant="flat"
            :loading="busyId === ch.id"
            :disabled="busyId === ch.id"
            @click="test(ch)"
          >
            发送测试
          </KunButton>
          <KunButton
            size="sm"
            variant="light"
            color="danger"
            :disabled="busyId === ch.id"
            @click="remove(ch)"
          >
            删除
          </KunButton>
        </div>
      </div>
    </div>
  </div>
</template>
