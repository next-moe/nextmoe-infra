<script setup lang="ts">
import {
  LLM_API,
  LLM_HEALTH_REFRESH_MS,
  LLM_HEALTH_STATE_META
} from '~/constants/llm'
import type { LlmRouteHealth } from '~~/shared/types/llm'

const api = useApi('ai')

const rows = ref<LlmRouteHealth[]>([])
const loading = ref(true)
const failed = ref('')
const busy = ref(0)
const refreshedAt = ref<number | null>(null)
const now = ref(Date.now())

const load = async () => {
  const res = await api.get<LlmRouteHealth[]>(`${LLM_API}/health`)
  loading.value = false
  if (res.code !== 0) {
    failed.value = res.message || '加载失败'
    return
  }
  failed.value = ''
  rows.value = res.data ?? []
  refreshedAt.value = Date.now()
}

let refreshTimer: ReturnType<typeof setInterval> | null = null
let clockTimer: ReturnType<typeof setInterval> | null = null
const onVisibility = () => {
  if (document.visibilityState === 'visible') load()
}
onMounted(() => {
  load()
  refreshTimer = setInterval(() => {
    if (document.visibilityState === 'visible') load()
  }, LLM_HEALTH_REFRESH_MS)
  clockTimer = setInterval(() => (now.value = Date.now()), 1000)
  document.addEventListener('visibilitychange', onVisibility)
})
onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
  if (clockTimer) clearInterval(clockTimer)
  document.removeEventListener('visibilitychange', onVisibility)
})

const groups = computed(() => {
  const map = new Map<string, LlmRouteHealth[]>()
  for (const r of rows.value) {
    map.set(r.model_key, [...(map.get(r.model_key) ?? []), r])
  }
  return [...map.entries()].map(([model, routes]) => ({ model, routes }))
})

const coolLeft = (r: LlmRouteHealth) => {
  if (!r.health.cool_until) return ''
  const s = Math.ceil(
    (new Date(r.health.cool_until).getTime() - now.value) / 1000
  )
  if (s <= 0) return ''
  return s >= 60 ? `${Math.floor(s / 60)} 分 ${s % 60} 秒` : `${s} 秒`
}

const errorRate = (r: LlmRouteHealth) =>
  r.last_hour.calls
    ? `${((r.last_hour.errors / r.last_hour.calls) * 100).toFixed(1)}%`
    : '—'

const limitText = (used: number, limit: number) =>
  limit > 0 ? `${used} / ${limit}` : `${used} / 不限`

const ms = (n: number) => (n > 0 ? `${n.toLocaleString()} ms` : '—')

const resetCooldown = async (r: LlmRouteHealth) => {
  busy.value = r.route_id
  try {
    const res = await api.post(`${LLM_API}/routes/${r.route_id}/reset-cooldown`)
    if (res.code !== 0) {
      useKunMessage(res.message || '操作失败', 'error')
      return
    }
    useKunMessage('已解除冷却', 'success')
    await load()
  } finally {
    busy.value = 0
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <p class="text-default-500 text-sm">
        每条路由的熔断、负载与近 1 小时表现;页面可见时每 10 秒刷新。
      </p>
      <div class="flex items-center gap-2">
        <span v-if="refreshedAt" class="text-default-400 text-xs">
          更新于 {{ new Date(refreshedAt).toLocaleTimeString() }}
        </span>
        <KunButton size="sm" variant="flat" @click="load">
          <KunIcon name="lucide:refresh-cw" class="mr-1 size-4" />
          刷新
        </KunButton>
      </div>
    </div>

    <CommonFetchError v-if="failed" :message="failed" @retry="load" />

    <div v-if="loading" class="flex justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <KunCard v-else-if="!groups.length" class-name="py-12 text-center">
      <p class="text-default-400">还没有路由</p>
    </KunCard>

    <div v-for="g in groups" :key="g.model" class="space-y-2">
      <h3 class="text-foreground font-mono font-semibold">{{ g.model }}</h3>
      <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
        <table class="w-full min-w-[64rem] text-sm">
          <thead class="bg-content2 text-default-500">
            <tr>
              <th class="px-3 py-2 text-left font-medium">供应商 / 上游模型</th>
              <th class="px-3 py-2 text-left font-medium">状态</th>
              <th class="px-3 py-2 text-left font-medium">并发 / RPM</th>
              <th class="px-3 py-2 text-left font-medium">近 1 小时</th>
              <th class="px-3 py-2 text-left font-medium">p50 / p95</th>
              <th class="px-3 py-2 text-left font-medium">最近错误</th>
              <th class="px-3 py-2 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="r in g.routes"
              :key="r.route_id"
              class="border-default-200 border-t align-top"
            >
              <td class="px-3 py-2">
                <p class="text-foreground font-medium">
                  {{ r.provider }}
                  <span class="text-default-400 text-xs"
                    >#{{ r.route_id }}</span
                  >
                </p>
                <p class="text-default-500 font-mono text-xs break-all">
                  {{ r.upstream_id }}
                </p>
              </td>
              <td class="px-3 py-2">
                <KunChip
                  :color="LLM_HEALTH_STATE_META[r.state].color"
                  variant="flat"
                  size="xs"
                >
                  {{ LLM_HEALTH_STATE_META[r.state].label }}
                </KunChip>
                <p
                  v-if="r.state === 'cooling' && coolLeft(r)"
                  class="text-warning-600 mt-1 text-xs tabular-nums"
                >
                  还剩 {{ coolLeft(r) }}
                </p>
                <p
                  v-if="r.health.strikes > 0"
                  class="text-default-500 mt-1 text-xs"
                >
                  连续失败 {{ r.health.strikes }} 次
                </p>
                <p v-if="r.health.shared_only" class="text-default-400 text-xs">
                  由其它网关进程触发
                </p>
              </td>
              <td class="px-3 py-2 text-xs tabular-nums">
                <p>并发 {{ limitText(r.load.in_flight, r.max_in_flight) }}</p>
                <p>RPM {{ limitText(r.load.rpm_used, r.rpm) }}</p>
              </td>
              <td class="px-3 py-2 text-xs tabular-nums">
                <p>{{ r.last_hour.calls.toLocaleString() }} 次</p>
                <p
                  :class="
                    r.last_hour.errors ? 'text-danger-600' : 'text-default-500'
                  "
                >
                  错误 {{ r.last_hour.errors }} · {{ errorRate(r) }}
                </p>
              </td>
              <td class="px-3 py-2 text-xs tabular-nums">
                <p>
                  {{ ms(r.last_hour.p50_ms) }} / {{ ms(r.last_hour.p95_ms) }}
                </p>
                <p class="text-default-500">近期 {{ ms(r.latency_ms) }}</p>
              </td>
              <td class="max-w-64 px-3 py-2 text-xs">
                <template v-if="r.last_hour.last_error">
                  <p class="text-danger-600 font-mono break-all">
                    {{ r.last_hour.last_error }}
                  </p>
                  <p class="text-default-400">
                    {{ llmTime(r.last_hour.last_error_at) }}
                  </p>
                </template>
                <span v-else class="text-default-400">—</span>
              </td>
              <td class="px-3 py-2 text-right whitespace-nowrap">
                <KunButton
                  v-if="r.state === 'cooling'"
                  size="sm"
                  variant="flat"
                  color="warning"
                  :loading="busy === r.route_id"
                  @click="resetCooldown(r)"
                >
                  解除冷却
                </KunButton>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
