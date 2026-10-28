<script setup lang="ts">
import {
  LLM_API,
  LLM_EVENT_KIND_LABEL,
  LLM_EVENTS_LIMIT,
  llmEventColor
} from '~/constants/llm'
import type {
  LlmEvent,
  LlmModel,
  LlmProvider,
  LlmRoute
} from '~~/shared/types/llm'

const props = defineProps<{
  routes: LlmRoute[]
  models: LlmModel[]
  providers: LlmProvider[]
}>()

const api = useApi('ai')
const events = ref<LlmEvent[]>([])
const loading = ref(false)

const load = async () => {
  loading.value = true
  try {
    const res = await api.get<LlmEvent[]>(`${LLM_API}/events`, {
      limit: LLM_EVENTS_LIMIT
    })
    if (res.code === 0) events.value = res.data ?? []
    else useKunMessage(res.message || '加载失败', 'error')
  } finally {
    loading.value = false
  }
}
onMounted(load)

const providerName = (id: number) =>
  id ? (props.providers.find((p) => p.id === id)?.name ?? `#${id}`) : ''

const routeLabel = (id: number) => {
  if (!id) return ''
  const r = props.routes.find((x) => x.id === id)
  if (!r) return `路由 #${id}`
  const model = props.models.find((m) => m.id === r.model_id)?.key
  return `${model ?? `#${r.model_id}`} · ${providerName(r.provider_id)}`
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-3">
      <p class="text-default-500 text-sm">
        最近
        {{ LLM_EVENTS_LIMIT }} 条:上游鉴权/额度持续失败与管理员改动,新的在前。
      </p>
      <KunButton variant="flat" :loading="loading" @click="load">
        <KunIcon name="lucide:refresh-cw" class="mr-2 size-4" />
        刷新
      </KunButton>
    </div>

    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[48rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">时间</th>
            <th class="px-3 py-2 text-left font-medium">类型</th>
            <th class="px-3 py-2 text-left font-medium">路由 / 供应商</th>
            <th class="px-3 py-2 text-left font-medium">详情</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="e in events"
            :key="e.id"
            class="border-default-200 border-t align-top"
          >
            <td class="text-default-500 px-3 py-2 text-xs whitespace-nowrap">
              {{ llmTime(e.created_at) }}
            </td>
            <td class="px-3 py-2">
              <KunChip :color="llmEventColor(e.kind)" variant="flat" size="xs">
                {{ LLM_EVENT_KIND_LABEL[e.kind] ?? e.kind }}
              </KunChip>
            </td>
            <td class="px-3 py-2">
              <p v-if="e.route_id">{{ routeLabel(e.route_id) }}</p>
              <p v-else-if="e.provider_id">{{ providerName(e.provider_id) }}</p>
              <span v-else class="text-default-400">—</span>
            </td>
            <td class="text-default-500 px-3 py-2 text-xs break-all">
              {{ e.detail }}
            </td>
          </tr>
          <tr v-if="!events.length">
            <td colspan="4" class="text-default-400 px-3 py-8 text-center">
              {{ loading ? '加载中…' : '暂无事件' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
