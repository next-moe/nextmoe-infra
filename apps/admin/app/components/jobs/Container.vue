<script setup lang="ts">
import {
  jobStatusMeta,
  jobTriggerLabel,
  formatJobSchedule,
  JOB_DISABLED_LABEL
} from '~/constants/jobs'
import type { JobInfo } from '~~/shared/types/jobs'

const api = useApi()

const { data, status, refresh, error } =
  await useApiFetch<JobInfo[]>('/admin/jobs')
const jobs = computed(() => data.value ?? [])
const isLoading = computed(() => status.value === 'pending')

const running = ref('')
const runJob = async (name: string) => {
  running.value = name
  try {
    const res = await api.post(`/admin/jobs/${name}/run`)
    if (res.code === 0) {
      useKunMessage('任务已在后台触发', 'success')
      setTimeout(refresh, 1200)
    } else {
      useKunMessage(res.message || '触发失败', 'error')
    }
  } finally {
    running.value = ''
  }
}

const historyOpen = ref(false)
const historyJob = ref<string | null>(null)
const openHistory = (name: string) => {
  historyJob.value = name
  historyOpen.value = true
}

const fmt = (s?: string | null) =>
  s ? new Date(s).toLocaleString('zh-CN') : '—'

const duration = (run: JobInfo['latest_run']): string => {
  if (!run || !run.finished_at) return ''
  const ms =
    new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.floor(ms / 60_000)}m${Math.round((ms % 60_000) / 1000)}s`
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-foreground text-2xl font-bold">后台任务</h1>
        <p class="text-default-500 mt-1">
          调度状态、最近执行与手动触发 · 共 {{ jobs.length }} 个任务 ·
          <NuxtLink to="/settings" class="text-primary hover:underline">
            调度与开关在配置中心的「后台任务」域修改
          </NuxtLink>
        </p>
      </div>
      <KunButton variant="flat" :disabled="isLoading" @click="() => refresh()">
        <KunIcon name="lucide:refresh-cw" class="mr-1 size-4" />
        刷新
      </KunButton>
    </div>

    <CommonFetchError v-if="error" @retry="refresh" />

    <div v-else-if="isLoading" class="flex justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <div v-else class="grid gap-4 lg:grid-cols-2">
      <KunCard
        v-for="job in jobs"
        :key="job.name"
        content-class="justify-start gap-0"
        class-name="p-5"
      >
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h3 class="text-foreground font-semibold">
                {{ job.desc || job.name }}
              </h3>
              <KunChip
                v-if="job.latest_run"
                :color="jobStatusMeta(job.latest_run.status).color"
                variant="flat"
                size="xs"
              >
                {{ jobStatusMeta(job.latest_run.status).label }}
              </KunChip>
            </div>
            <p class="text-default-400 mt-0.5 font-mono text-xs">
              {{ job.name }}
            </p>
          </div>
          <div class="flex shrink-0 flex-wrap items-center justify-end gap-1">
            <KunChip color="default" variant="flat" size="xs">
              {{ formatJobSchedule(job.schedule) }}
            </KunChip>
            <KunChip
              v-if="!job.enabled"
              color="warning"
              variant="flat"
              size="xs"
            >
              {{ JOB_DISABLED_LABEL }}
            </KunChip>
          </div>
        </div>

        <div class="text-default-500 mt-3 space-y-1 text-sm">
          <template v-if="job.latest_run">
            <p>
              最近执行：{{ fmt(job.latest_run.started_at) }}
              <span v-if="duration(job.latest_run)" class="text-default-400">
                · 耗时 {{ duration(job.latest_run) }}
              </span>
              <span class="text-default-400">
                · {{ jobTriggerLabel(job.latest_run.trigger) }}触发
              </span>
            </p>
            <p v-if="job.latest_run.error" class="text-danger-600 break-words">
              {{ job.latest_run.error }}
            </p>
            <p
              v-else-if="job.latest_run.summary"
              class="text-default-400 truncate font-mono text-xs"
            >
              {{ JSON.stringify(job.latest_run.summary) }}
            </p>
          </template>
          <p v-else class="text-default-400">尚无执行记录</p>
        </div>

        <div class="mt-4 flex gap-2">
          <KunButton
            color="primary"
            variant="flat"
            size="sm"
            :disabled="running === job.name"
            @click="runJob(job.name)"
          >
            <KunIcon
              v-if="running === job.name"
              name="lucide:loader-circle"
              class="mr-1 size-4 animate-spin"
            />
            立即运行
          </KunButton>
          <KunButton
            color="default"
            variant="flat"
            size="sm"
            @click="openHistory(job.name)"
          >
            运行历史
          </KunButton>
        </div>
      </KunCard>
    </div>

    <JobsRunHistoryModal v-model:open="historyOpen" :job-name="historyJob" />
  </div>
</template>
