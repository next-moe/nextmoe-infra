<script setup lang="ts">
import { LLM_API, LLM_ROUTE_HINT } from '~/constants/llm'
import type {
  LlmModel,
  LlmProvider,
  LlmRoute,
  LlmRouteTestResult
} from '~~/shared/types/llm'

const props = defineProps<{
  routes: LlmRoute[]
  models: LlmModel[]
  providers: LlmProvider[]
}>()
const emit = defineEmits<{ changed: [] }>()

const api = useApi('ai')
const busy = ref('')
const testResults = ref<Record<number, LlmRouteTestResult>>({})

const providerName = (id: number) =>
  props.providers.find((p) => p.id === id)?.name ?? `#${id}`

const groups = computed(() =>
  props.models
    .map((model) => ({
      model,
      routes: props.routes
        .filter((r) => r.model_id === model.id)
        .sort((a, b) => b.priority - a.priority || b.weight - a.weight)
    }))
    .filter((g) => g.routes.length)
)

const modalOpen = ref(false)
const editing = ref<LlmRoute | null>(null)
const openModal = (r: LlmRoute | null) => {
  editing.value = r
  modalOpen.value = true
}

const setStatus = async (r: LlmRoute) => {
  const status = r.status === 'active' ? 'disabled' : 'active'
  busy.value = `${r.id}:status`
  try {
    const res = await api.patch(`${LLM_API}/routes/${r.id}`, { status })
    if (res.code !== 0) {
      useKunMessage(res.message || '更新失败', 'error')
      return
    }
    useKunMessage(status === 'active' ? '已启用' : '已停用', 'success')
    emit('changed')
  } finally {
    busy.value = ''
  }
}

const test = async (r: LlmRoute) => {
  busy.value = `${r.id}:test`
  try {
    const res = await api.post<LlmRouteTestResult>(
      `${LLM_API}/routes/${r.id}/test`
    )
    if (res.code !== 0) {
      useKunMessage(res.message || '测试请求失败', 'error')
      return
    }
    testResults.value = { ...testResults.value, [r.id]: res.data }
  } finally {
    busy.value = ''
  }
}

const remove = async (r: LlmRoute) => {
  const confirmed = await useKunAlert({
    title: `删除路由「${providerName(r.provider_id)} · ${r.upstream_id}」?`,
    message: '删除后该模型不再经由这条路由调用。',
    type: 'danger',
    confirmText: '删除',
    confirmColor: 'danger'
  })
  if (!confirmed) return
  busy.value = `${r.id}:delete`
  try {
    const res = await api.delete(`${LLM_API}/routes/${r.id}`)
    if (res.code !== 0) {
      useKunMessage(res.message || '删除失败', 'error')
      return
    }
    useKunMessage('已删除', 'success')
    emit('changed')
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between gap-3">
      <p class="text-default-500 text-sm">{{ LLM_ROUTE_HINT }}</p>
      <KunButton
        color="primary"
        :disabled="!models.length || !providers.length"
        @click="openModal(null)"
      >
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        新建路由
      </KunButton>
    </div>

    <KunCard v-if="!groups.length" class-name="py-12 text-center">
      <p class="text-default-400">
        还没有路由;先建好供应商和模型,再把它们连起来
      </p>
    </KunCard>

    <div v-for="g in groups" :key="g.model.id" class="space-y-2">
      <h3 class="text-foreground flex items-center gap-2 font-semibold">
        <span class="font-mono">{{ g.model.key }}</span>
        <KunChip
          v-if="!g.model.enabled"
          color="default"
          variant="flat"
          size="xs"
        >
          模型已停用
        </KunChip>
      </h3>
      <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
        <table class="w-full min-w-[64rem] text-sm">
          <thead class="bg-content2 text-default-500">
            <tr>
              <th class="px-3 py-2 text-left font-medium">供应商 / 上游模型</th>
              <th class="px-3 py-2 text-right font-medium">优先级</th>
              <th class="px-3 py-2 text-right font-medium">权重</th>
              <th class="px-3 py-2 text-left font-medium">状态</th>
              <th class="px-3 py-2 text-left font-medium">
                价格 (每 1M tokens / 每次)
              </th>
              <th class="px-3 py-2 text-left font-medium">近 24 小时</th>
              <th class="px-3 py-2 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="r in g.routes" :key="r.id">
              <AiLlmRouteRow
                :route="r"
                :provider-name="providerName(r.provider_id)"
                :busy="busy"
                @test="test(r)"
                @toggle="setStatus(r)"
                @edit="openModal(r)"
                @remove="remove(r)"
              />
              <tr v-if="testResults[r.id]">
                <td colspan="7" class="px-3 pb-3">
                  <AiLlmRouteTestResult :result="testResults[r.id]!" />
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>

    <AiLlmRouteModal
      v-model="modalOpen"
      :route="editing"
      :models="models"
      :providers="providers"
      @saved="emit('changed')"
    />
  </div>
</template>
