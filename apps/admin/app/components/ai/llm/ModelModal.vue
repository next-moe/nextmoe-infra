<script setup lang="ts">
import {
  LLM_API,
  LLM_MODEL_CAPABILITIES,
  LLM_MODEL_KIND_OPTIONS
} from '~/constants/llm'
import type { LlmModel, LlmModelCreateRequest } from '~~/shared/types/llm'

const props = defineProps<{ item: LlmModel | null }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const api = useApi('ai')

const blank = (): LlmModelCreateRequest => ({
  key: '',
  kind: 'chat',
  context_tokens: 0,
  max_output: 0,
  tool_call: false,
  vision: false,
  reasoning: false,
  enabled: true
})

const form = ref<LlmModelCreateRequest>(blank())
const saving = ref(false)
const error = ref('')

watch(open, (v) => {
  if (!v) return
  const m = props.item
  form.value = m
    ? {
        key: m.key,
        kind: m.kind,
        context_tokens: m.context_tokens,
        max_output: m.max_output,
        tool_call: m.tool_call,
        vision: m.vision,
        reasoning: m.reasoning,
        enabled: m.enabled
      }
    : blank()
  error.value = ''
})

const save = async () => {
  error.value = ''
  const key = form.value.key.trim()
  if (!key) {
    error.value = 'Key 是必填的'
    return
  }
  saving.value = true
  try {
    const body = { ...form.value, key }
    const res = props.item
      ? await api.patch<LlmModel>(`${LLM_API}/models/${props.item.id}`, body)
      : await api.post<LlmModel>(`${LLM_API}/models`, body)
    if (res.code !== 0) {
      error.value = res.message || '保存失败'
      return
    }
    useKunMessage(props.item ? '已保存' : '已创建', 'success')
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
    :title="item ? `编辑模型「${item.key}」` : '新建模型'"
    size="md"
  >
    <div class="space-y-4">
      <div class="grid gap-3 sm:grid-cols-2">
        <KunInput
          v-model="form.key"
          label="Key"
          placeholder="deepseek-v4-flash"
          :disabled="!!item"
          :description="item ? '创建后不可改,场景与访问密钥按它引用' : ''"
          required
        />
        <KunSelect
          v-model="form.kind"
          label="类型"
          :options="LLM_MODEL_KIND_OPTIONS"
          :disabled="!!item"
        />
        <KunNumberInput
          :model-value="form.context_tokens"
          label="上下文 tokens"
          :min="0"
          @update:model-value="form.context_tokens = $event ?? 0"
        />
        <KunNumberInput
          :model-value="form.max_output"
          label="最大输出 tokens"
          :min="0"
          @update:model-value="form.max_output = $event ?? 0"
        />
      </div>
      <div class="flex flex-wrap gap-4">
        <KunSwitch
          v-for="c in LLM_MODEL_CAPABILITIES"
          :key="c.key"
          v-model="form[c.key]"
          :label="c.label"
        />
        <KunSwitch v-model="form.enabled" label="启用" />
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
