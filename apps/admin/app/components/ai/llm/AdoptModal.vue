<script setup lang="ts">
import {
  LLM_API,
  LLM_MODEL_KIND_META,
  LLM_MODEL_KIND_OPTIONS
} from '~/constants/llm'
import type {
  LlmCatalogAdoptRequest,
  LlmCatalogAdoptResult,
  LlmCatalogEntry,
  LlmModelKind
} from '~~/shared/types/llm'

const props = defineProps<{ entry: LlmCatalogEntry | null }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ adopted: [LlmCatalogAdoptResult] }>()

const api = useApi('ai')

const isGatewayKind = (k: string): k is LlmModelKind => k in LLM_MODEL_KIND_META

const modelKey = ref('')
const kind = ref<LlmModelKind>('chat')
const priority = ref(0)
const weight = ref(100)
const price = ref(llmPriceDraft())
const saving = ref(false)
const error = ref('')

watch(open, (v) => {
  if (!v || !props.entry) return
  modelKey.value = llmAdoptModelKey(props.entry.upstream_id)
  kind.value = isGatewayKind(props.entry.kind) ? props.entry.kind : 'chat'
  priority.value = 0
  weight.value = 100
  price.value = llmPriceDraft()
  error.value = ''
})

const save = async () => {
  if (!props.entry) return
  error.value = ''
  const key = modelKey.value.trim()
  if (!key) {
    error.value = '模型 key 是必填的'
    return
  }
  if (weight.value < 1 || weight.value > 10000) {
    error.value = '权重须在 1 到 10000 之间'
    return
  }
  const parsed = llmParsePrice(price.value)
  if (parsed === 'invalid') {
    error.value = '价格须为非负数(美元),留空表示不定价'
    return
  }
  const body: LlmCatalogAdoptRequest = {
    model_key: key,
    kind: kind.value,
    priority: priority.value,
    weight: weight.value,
    ...(parsed ? { price: parsed } : {})
  }

  saving.value = true
  try {
    const res = await api.post<LlmCatalogAdoptResult>(
      `${LLM_API}/provider-models/${props.entry.id}/adopt`,
      { ...body }
    )
    if (res.code !== 0) {
      error.value = res.message || '加入失败'
      return
    }
    const { model, route, model_created } = res.data
    useKunMessage(
      model_created
        ? `已新建模型「${model.key}」并加入路由 #${route.id}`
        : `已加入现有模型「${model.key}」,路由 #${route.id}`,
      'success'
    )
    open.value = false
    emit('adopted', res.data)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" title="加入网关" size="lg">
    <div v-if="entry" class="space-y-4">
      <p class="text-default-500 text-sm">
        为上游模型
        <span class="text-foreground font-mono break-all">
          {{ entry.upstream_id }}
        </span>
        新建一条路由;模型 key 不存在时会一并创建。
      </p>
      <div class="grid gap-3 sm:grid-cols-2">
        <KunInput
          v-model="modelKey"
          label="模型 key"
          maxlength="64"
          description="已存在的模型须与所选类型一致"
          required
        />
        <KunSelect
          v-model="kind"
          label="类型"
          :options="LLM_MODEL_KIND_OPTIONS"
        />
      </div>
      <div class="grid gap-3 sm:grid-cols-2">
        <KunNumberInput
          :model-value="priority"
          label="优先级"
          description="数值越大越先试"
          @update:model-value="priority = $event ?? 0"
        />
        <KunNumberInput
          :model-value="weight"
          label="权重"
          :min="1"
          :max="10000"
          description="同一优先级内的流量占比"
          @update:model-value="weight = $event ?? 1"
        />
      </div>

      <AiLlmPriceFields v-model="price" />

      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
        {{ error }}
      </div>
      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :loading="saving" @click="save">
          加入
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
