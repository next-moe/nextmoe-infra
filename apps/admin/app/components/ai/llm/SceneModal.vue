<script setup lang="ts">
import {
  LLM_API,
  LLM_BUDGET_HINT,
  LLM_SCENE_CHAINS,
  LLM_SCENE_CLASS_OPTIONS,
  LLM_SCENE_DEFAULTS,
  LLM_SCENE_STRATEGY_META,
  LLM_SCENE_STRATEGY_OPTIONS
} from '~/constants/llm'
import type {
  LlmModel,
  LlmScene,
  LlmScenePutRequest
} from '~~/shared/types/llm'

const props = defineProps<{
  scene: LlmScene | null
  models: LlmModel[]
  takenKeys: string[]
}>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const api = useApi('ai')

const key = ref('')
const form = ref<LlmScenePutRequest>({ ...LLM_SCENE_DEFAULTS })
const budget = ref('')
const saving = ref(false)
const error = ref('')

watch(open, (v) => {
  if (!v) return
  const s = props.scene
  key.value = s?.key ?? ''
  form.value = s
    ? {
        model_id: s.model_id,
        fallback_model_ids: [...s.fallback_model_ids],
        context_fallback_model_ids: [...s.context_fallback_model_ids],
        content_fallback_model_ids: [...s.content_fallback_model_ids],
        strategy: s.strategy,
        daily_budget_micro: s.daily_budget_micro,
        class: s.class,
        temperature: s.temperature,
        max_output: s.max_output,
        ttft_ms: s.ttft_ms,
        idle_ms: s.idle_ms,
        total_ms: s.total_ms
      }
    : {
        ...LLM_SCENE_DEFAULTS,
        model_id: props.models[0]?.id ?? 0,
        fallback_model_ids: [],
        context_fallback_model_ids: [],
        content_fallback_model_ids: []
      }
  budget.value = llmBudgetDraft(form.value.daily_budget_micro)
  error.value = ''
})

const byId = (id: number) => props.models.find((m) => m.id === id)
const primaryKind = computed(() => byId(form.value.model_id)?.kind)

const primaryOptions = computed(() =>
  props.models.map((m) => ({ value: m.id, label: `${m.key} · ${m.kind}` }))
)
watch(
  () => form.value.model_id,
  () => {
    for (const c of LLM_SCENE_CHAINS) {
      form.value[c.key] = form.value[c.key].filter(
        (id) =>
          id !== form.value.model_id && byId(id)?.kind === primaryKind.value
      )
    }
  }
)

const NUMBER_FIELDS = [
  { key: 'max_output', label: '最大输出 tokens', hint: '0 = 用模型默认' },
  { key: 'ttft_ms', label: '首字超时 (ms)', hint: '' },
  { key: 'idle_ms', label: '空闲超时 (ms)', hint: '两段输出之间' },
  { key: 'total_ms', label: '总超时 (ms)', hint: '' }
] as const

const save = async () => {
  error.value = ''
  const k = key.value.trim()
  if (!k || !form.value.model_id) {
    error.value = '场景 key 和主模型是必填的'
    return
  }
  if (!props.scene && props.takenKeys.includes(k)) {
    error.value = '已有同名场景,请直接编辑它'
    return
  }
  const dailyBudget = llmParseBudget(budget.value)
  if (dailyBudget === 'invalid') {
    error.value = '每日预算须为非负数(美元)'
    return
  }
  saving.value = true
  try {
    const res = await api.put<LlmScene>(
      `${LLM_API}/scenes/${encodeURIComponent(k)}`,
      { ...form.value, daily_budget_micro: dailyBudget }
    )
    if (res.code !== 0) {
      error.value = res.message || '保存失败'
      return
    }
    useKunMessage(props.scene ? '已保存' : '已创建', 'success')
    open.value = false
    emit('saved')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="scene ? `编辑场景「${scene.key}」` : '新建场景'"
    size="lg"
  >
    <div class="space-y-4">
      <div class="grid gap-3 sm:grid-cols-2">
        <KunInput
          v-model="key"
          label="场景 key"
          placeholder="agent.assistant"
          :disabled="!!scene"
          required
        />
        <KunSelect
          v-model="form.class"
          label="类别"
          :options="LLM_SCENE_CLASS_OPTIONS"
        />
      </div>

      <div class="border-default-200 space-y-3 rounded-lg border p-4">
        <KunSelect
          v-model="form.model_id"
          label="主模型"
          :options="primaryOptions"
          searchable
        />
        <KunSelect
          v-model="form.strategy"
          label="路由策略"
          :options="LLM_SCENE_STRATEGY_OPTIONS"
          :description="LLM_SCENE_STRATEGY_META[form.strategy].hint"
        />
        <p class="text-default-400 text-xs">
          以下三条链都只能选与主模型同类型的模型。
        </p>
        <AiLlmSceneChain
          v-for="c in LLM_SCENE_CHAINS"
          :key="c.key"
          v-model="form[c.key]"
          :models="models"
          :primary-id="form.model_id"
          :label="c.label"
          :hint="c.hint"
        />
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <KunNumberInput
          v-model="form.temperature"
          label="温度"
          :min="0"
          :max="2"
          :step="0.1"
          :precision="2"
          placeholder="留空 = 默认"
        />
        <KunNumberInput
          v-for="f in NUMBER_FIELDS"
          :key="f.key"
          :model-value="form[f.key]"
          :label="f.label"
          :description="f.hint"
          :min="0"
          @update:model-value="form[f.key] = $event ?? 0"
        />
        <KunInput
          v-model="budget"
          label="每日预算（美元）"
          type="number"
          min="0"
          step="0.01"
          placeholder="不限"
          :description="LLM_BUDGET_HINT"
        />
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
