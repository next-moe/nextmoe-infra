<script setup lang="ts">
import type {
  TelemetryIssueDetail,
  TelemetryIssueView
} from '~~/shared/types/telemetry'

const route = useRoute()
const api = useApi('telemetry')

const id = computed(() => Number(route.params.id))

const { data, status, error, refresh } =
  await useApiFetch<TelemetryIssueDetail>(
    () => `/admin/telemetry/issues/${id.value}`,
    {},
    'telemetry'
  )

const issue = computed(() => data.value)
const isLoading = computed(() => status.value === 'pending')

const pending = ref(false)
const resolveOpen = ref(false)
const resolvedVersion = ref('')

const backTo = computed(() => ({
  path: '/telemetry/issues',
  query: issue.value ? { app: String(issue.value.app_id) } : {}
}))

const applyStatus = async (
  next: 'open' | 'resolved' | 'ignored',
  resolvedIn?: string
) => {
  if (!issue.value) return
  pending.value = true
  try {
    const body: Record<string, unknown> = { status: next }
    if (next === 'resolved' && resolvedIn) {
      body.resolved_in_version = resolvedIn
    }
    const res = await api.patch<TelemetryIssueView>(
      `/admin/telemetry/issues/${issue.value.id}`,
      body
    )
    if (res.code === 0) {
      useKunMessage('已更新', 'success')
      resolveOpen.value = false
      await refresh()
    } else {
      useKunMessage(res.message || '更新失败', 'error')
    }
  } finally {
    pending.value = false
  }
}

const openResolve = () => {
  resolvedVersion.value = issue.value?.resolved_in_version ?? ''
  resolveOpen.value = true
}

const confirmResolve = () =>
  applyStatus('resolved', resolvedVersion.value.trim())
</script>

<template>
  <div class="space-y-5">
    <NuxtLink :to="backTo" class="text-primary text-sm hover:underline">
      ← 返回问题列表
    </NuxtLink>

    <CommonFetchError v-if="error" :message="error.message" @retry="refresh" />

    <div v-else-if="isLoading" class="flex justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <KunCard
      v-else-if="!issue"
      content-class="justify-start gap-0"
      class-name="py-12 text-center"
    >
      <p class="text-default-400">未找到该问题</p>
    </KunCard>

    <template v-else>
      <div class="space-y-3">
        <div class="flex flex-wrap items-center gap-2">
          <TelemetryKindChip :kind="issue.kind" />
          <TelemetryIssueStatusChip :status="issue.status" />
          <KunChip
            v-if="issue.regressed"
            color="danger"
            variant="flat"
            size="xs"
          >
            回归
          </KunChip>
        </div>
        <h1 class="text-foreground text-2xl font-bold break-all">
          {{ issue.title }}
        </h1>
        <p class="text-default-500 font-mono text-sm break-all">
          {{ issue.culprit }}
        </p>
        <p class="text-default-400 text-xs">
          首次 {{ issue.first_seen_day }} · {{ issue.first_version }} · 最近
          {{ issue.last_seen_day }} · {{ issue.last_version }}
          <span v-if="issue.resolved_in_version">
            · 解决于 {{ issue.resolved_in_version }}
          </span>
        </p>
        <div class="flex flex-wrap gap-2">
          <KunButton
            v-if="issue.status !== 'resolved'"
            color="success"
            variant="flat"
            size="sm"
            :disabled="pending"
            :loading="pending"
            @click="openResolve"
          >
            标记已解决
          </KunButton>
          <KunButton
            v-if="issue.status !== 'ignored'"
            color="default"
            variant="flat"
            size="sm"
            :disabled="pending"
            :loading="pending"
            @click="applyStatus('ignored')"
          >
            忽略
          </KunButton>
          <KunButton
            v-if="issue.status !== 'open'"
            color="primary"
            variant="flat"
            size="sm"
            :disabled="pending"
            :loading="pending"
            @click="applyStatus('open')"
          >
            重新打开
          </KunButton>
        </div>
      </div>

      <TelemetryIssuesDetailChart :daily="issue.daily" />
      <TelemetryIssuesDetailCrashes :crashes="issue.crashes" />
    </template>

    <KunModal v-model="resolveOpen" title="标记已解决" size="sm">
      <div class="space-y-4">
        <KunInput
          v-model="resolvedVersion"
          label="修复版本（可选）"
          placeholder="例如 1.2.0"
        />
        <div class="flex justify-end gap-3">
          <KunButton
            color="default"
            variant="flat"
            :disabled="pending"
            @click="resolveOpen = false"
          >
            取消
          </KunButton>
          <KunButton color="success" :loading="pending" @click="confirmResolve">
            确认
          </KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
