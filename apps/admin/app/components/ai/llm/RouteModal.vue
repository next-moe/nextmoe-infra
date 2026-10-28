<script setup lang="ts">
import { LLM_API, LLM_ROUTE_STATUS_OPTIONS } from '~/constants/llm'
import type {
  LlmModel,
  LlmPrice,
  LlmProvider,
  LlmRoute,
  LlmRouteStatus
} from '~~/shared/types/llm'

const props = defineProps<{
  route: LlmRoute | null
  models: LlmModel[]
  providers: LlmProvider[]
}>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const api = useApi('ai')

const PRICE_FIELDS = [
  { key: 'input_micro', label: '输入' },
  { key: 'cached_micro', label: '缓存命中输入' },
  { key: 'output_micro', label: '输出' }
] as const
type PriceKey = (typeof PRICE_FIELDS)[number]['key']

const modelId = ref(0)
const providerId = ref(0)
const upstreamId = ref('')
const priority = ref(0)
const weight = ref(1)
const status = ref<LlmRouteStatus>('active')
const price = ref<Record<PriceKey, string>>({
  input_micro: '',
  cached_micro: '',
  output_micro: ''
})
let initialPrice = ''
const saving = ref(false)
const error = ref('')

const modelOptions = computed(() =>
  props.models.map((m) => ({ value: m.id, label: `${m.key} · ${m.kind}` }))
)
const providerOptions = computed(() =>
  props.providers.map((p) => ({ value: p.id, label: p.name }))
)

watch(open, (v) => {
  if (!v) return
  const r = props.route
  modelId.value = r?.model_id ?? props.models[0]?.id ?? 0
  providerId.value = r?.provider_id ?? props.providers[0]?.id ?? 0
  upstreamId.value = r?.upstream_id ?? ''
  priority.value = r?.priority ?? 0
  weight.value = r?.weight ?? 1
  status.value = r?.status ?? 'active'
  price.value = {
    input_micro: llmMicroToUsd(r?.price?.input_micro),
    cached_micro: llmMicroToUsd(r?.price?.cached_micro),
    output_micro: llmMicroToUsd(r?.price?.output_micro)
  }
  initialPrice = JSON.stringify(price.value)
  error.value = ''
})

const parsePrice = (): LlmPrice | null | 'invalid' => {
  const raw = price.value
  if (PRICE_FIELDS.every((f) => raw[f.key].trim() === '')) return null
  const out = { input_micro: 0, cached_micro: 0, output_micro: 0 }
  for (const f of PRICE_FIELDS) {
    const n = llmUsdToMicro(raw[f.key])
    if (Number.isNaN(n)) return 'invalid'
    out[f.key] = n ?? 0
  }
  return out
}

const save = async () => {
  error.value = ''
  const upstream = upstreamId.value.trim()
  if (!upstream || !modelId.value || !providerId.value) {
    error.value = '模型、供应商和上游模型 ID 是必填的'
    return
  }
  const parsed = parsePrice()
  if (parsed === 'invalid') {
    error.value = '价格须为非负数(美元 / 1M tokens),留空表示不定价'
    return
  }
  const priceChanged = JSON.stringify(price.value) !== initialPrice
  const sendPrice = parsed && (!props.route || priceChanged) ? parsed : null
  const statusChanged = !props.route || status.value !== props.route.status

  const body = {
    upstream_id: upstream,
    priority: priority.value,
    weight: weight.value,
    ...(statusChanged ? { status: status.value } : {}),
    ...(sendPrice ? { price: sendPrice } : {})
  }

  saving.value = true
  try {
    const res = props.route
      ? await api.patch<LlmRoute>(`${LLM_API}/routes/${props.route.id}`, body)
      : await api.post<LlmRoute>(`${LLM_API}/routes`, {
          model_id: modelId.value,
          provider_id: providerId.value,
          ...body
        })
    if (res.code !== 0) {
      error.value = res.message || '保存失败'
      return
    }
    useKunMessage(props.route ? '已保存' : '已创建', 'success')
    open.value = false
    emit('saved')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" :title="route ? '编辑路由' : '新建路由'" size="lg">
    <div class="space-y-4">
      <div class="grid gap-3 sm:grid-cols-2">
        <KunSelect
          v-model="modelId"
          label="模型"
          :options="modelOptions"
          :disabled="!!route"
        />
        <KunSelect
          v-model="providerId"
          label="供应商"
          :options="providerOptions"
          :disabled="!!route"
        />
      </div>
      <KunInput
        v-model="upstreamId"
        label="上游模型 ID"
        placeholder="@cf/deepseek-ai/deepseek-v4-flash"
        description="供应商那边的模型名,原样放进请求的 model 字段"
        required
      />
      <div class="grid gap-3 sm:grid-cols-3">
        <KunNumberInput
          :model-value="priority"
          label="优先级"
          description="数值越大越先试"
          @update:model-value="priority = $event ?? 0"
        />
        <KunNumberInput
          :model-value="weight"
          label="权重"
          :min="0"
          description="同一优先级内的流量占比"
          @update:model-value="weight = $event ?? 0"
        />
        <KunSelect
          v-model="status"
          label="状态"
          :options="LLM_ROUTE_STATUS_OPTIONS"
        />
      </div>

      <div class="border-default-200 space-y-2 rounded-lg border p-4">
        <p class="text-default-500 text-sm">
          价格(美元 / 1M tokens,可选;全部留空 = 不定价{{
            route ? ',修改后生成新的价格版本' : ''
          }})
        </p>
        <div class="grid gap-3 sm:grid-cols-3">
          <KunInput
            v-for="f in PRICE_FIELDS"
            :key="f.key"
            v-model="price[f.key]"
            :label="f.label"
            type="number"
            min="0"
            step="0.0001"
            placeholder="0.00"
          />
        </div>
      </div>

      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
        {{ error }}
      </div>
      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :loading="saving" @click="save">
          保存
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
