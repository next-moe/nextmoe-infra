<script setup lang="ts">
import { LLM_API, LLM_TABS } from '~/constants/llm'
import type {
  LlmAccessKey,
  LlmModel,
  LlmProvider,
  LlmRoute,
  LlmScene,
  LlmStatus
} from '~~/shared/types/llm'

const { isRen } = useAuth()
const api = useApi('ai')

const tab = ref('providers')
const loading = ref(true)
const failed = ref('')

const kekConfigured = ref(true)
const providers = ref<LlmProvider[]>([])
const models = ref<LlmModel[]>([])
const routes = ref<LlmRoute[]>([])
const scenes = ref<LlmScene[]>([])
const accessKeys = ref<LlmAccessKey[]>([])

const fetchList = async <T,>(path: string, target: Ref<T[]>) => {
  const res = await api.get<T[]>(`${LLM_API}${path}`)
  if (res.code === 0) target.value = res.data ?? []
  else failed.value = res.message || '加载失败'
}

const loaders = {
  status: async () => {
    const res = await api.get<LlmStatus>(`${LLM_API}/status`)
    if (res.code === 0) kekConfigured.value = res.data.kek_configured
    else failed.value = res.message || '加载失败'
  },
  providers: () => fetchList('/providers', providers),
  models: () => fetchList('/models', models),
  routes: () => fetchList('/routes', routes),
  scenes: () => fetchList('/scenes', scenes),
  accessKeys: () => fetchList('/access-keys', accessKeys)
}
type LoaderName = keyof typeof loaders

const load = async (...names: LoaderName[]) => {
  failed.value = ''
  const list = names.length ? names : (Object.keys(loaders) as LoaderName[])
  await Promise.all(list.map((n) => loaders[n]()))
  loading.value = false
}

onMounted(() => {
  if (isRen.value) load()
})
</script>

<template>
  <div class="space-y-5">
    <div>
      <h1 class="text-foreground text-2xl font-bold">LLM 网关配置</h1>
      <p class="text-default-500 mt-1 text-sm">
        管理 ai
        服务的上游供应商与密钥、模型、路由(负载均衡与故障切换)、场景和内部访问密钥
      </p>
    </div>

    <KunCard v-if="!isRen" class-name="py-12 text-center">
      <p class="text-default-500">仅 ren 可访问</p>
    </KunCard>

    <template v-else>
      <div
        v-if="!kekConfigured"
        class="border-warning-200 bg-warning-50 flex items-start gap-3 rounded-xl border p-4"
      >
        <KunIcon
          name="lucide:triangle-alert"
          class="text-warning-600 size-5 shrink-0"
        />
        <p class="text-warning-700 text-sm">
          ai 服务未设置 KUN_LLM_KEK,暂时无法保存供应商 API key。请先在 ai
          服务的环境变量中配置 KUN_LLM_KEK 并重启。
        </p>
      </div>

      <CommonFetchError v-if="failed" :message="failed" @retry="load()" />

      <KunTab v-model="tab" :items="LLM_TABS" />

      <div v-if="loading" class="flex justify-center py-12">
        <KunIcon
          name="lucide:loader-circle"
          class="text-primary size-8 animate-spin"
        />
      </div>
      <template v-else>
        <AiLlmProviders
          v-if="tab === 'providers'"
          :providers="providers"
          :kek-configured="kekConfigured"
          @changed="load('providers')"
        />
        <AiLlmModels
          v-else-if="tab === 'models'"
          :models="models"
          @changed="load('models')"
        />
        <AiLlmRoutes
          v-else-if="tab === 'routes'"
          :routes="routes"
          :models="models"
          :providers="providers"
          @changed="load('routes')"
        />
        <AiLlmScenes
          v-else-if="tab === 'scenes'"
          :scenes="scenes"
          :models="models"
          @changed="load('scenes')"
        />
        <AiLlmAccessKeys
          v-else-if="tab === 'keys'"
          :access-keys="accessKeys"
          :models="models"
          :scenes="scenes"
          @changed="load('accessKeys')"
        />
        <AiLlmEvents
          v-else
          :routes="routes"
          :models="models"
          :providers="providers"
        />
      </template>
    </template>
  </div>
</template>
