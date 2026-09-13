<script setup lang="ts">
import { DASHBOARD_STAT_CARDS } from '~/constants/dashboard'
import { jobStatusMeta } from '~/constants/jobs'
import type { JobInfo } from '~~/shared/types/jobs'

const auth = useAuth()

const { data: usersData } = await useApiFetch<{ total: number }>(
  '/admin/users',
  { query: { page: 1, limit: 1 } }
)
const { data: sitesData } = await useApiFetch<Site[]>('/sites')
const { data: clientsData } = await useApiFetch<OAuthClient[]>('/oauth/clients')
const { data: jobsData } = await useApiFetch<JobInfo[]>('/admin/jobs')

const counts = computed<Record<string, number>>(() => ({
  users: usersData.value?.total ?? 0,
  sites: sitesData.value?.length ?? 0,
  clients: clientsData.value?.length ?? 0,
}))

const stats = computed(() =>
  DASHBOARD_STAT_CARDS.map((c) => ({
    label: c.label,
    icon: c.icon,
    color: c.color,
    value: String(counts.value[c.key] ?? 0),
  }))
)

const recentRuns = computed(() =>
  (jobsData.value ?? [])
    .filter((j) => j.latest_run)
    .map((j) => ({ name: j.name, desc: j.desc || j.name, run: j.latest_run! }))
    .sort(
      (a, b) =>
        new Date(b.run.started_at).getTime() -
        new Date(a.run.started_at).getTime()
    )
    .slice(0, 6)
)
const fmtRun = (s: string) => new Date(s).toLocaleString('zh-CN')
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-bold text-foreground">仪表盘</h1>
      <p class="mt-1 text-default-500">
        欢迎回来，{{ auth.user.value?.name }}
      </p>
    </div>

    <div class="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
      <DashboardStatCard
        v-for="stat in stats"
        :key="stat.label"
        v-bind="stat"
      />
    </div>

    <DashboardRegistrationStats />

    <DashboardQuickActions />

    <KunCard content-class="justify-start gap-0" class-name="p-6">
      <div class="mb-4 flex items-center justify-between">
        <h2 class="text-lg font-semibold text-foreground">最近后台任务</h2>
        <NuxtLink to="/jobs" class="text-primary text-sm hover:underline">
          查看全部
        </NuxtLink>
      </div>
      <ul v-if="recentRuns.length" class="divide-default-200 divide-y">
        <li
          v-for="item in recentRuns"
          :key="item.name"
          class="flex items-center justify-between gap-3 py-2.5"
        >
          <div class="flex min-w-0 items-center gap-2">
            <KunChip
              :color="jobStatusMeta(item.run.status).color"
              variant="flat"
              size="xs"
            >
              {{ jobStatusMeta(item.run.status).label }}
            </KunChip>
            <span class="text-foreground truncate text-sm">{{ item.desc }}</span>
          </div>
          <span class="text-default-400 shrink-0 text-xs">
            {{ fmtRun(item.run.started_at) }}
          </span>
        </li>
      </ul>
      <div v-else class="py-8 text-center text-default-400">
        <KunIcon name="lucide:inbox" class="mx-auto mb-2 size-12 opacity-50" />
        <p>暂无活动记录</p>
      </div>
    </KunCard>
  </div>
</template>
