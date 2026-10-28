<script setup lang="ts">
import {
  LLM_API,
  LLM_SCENE_CLASS_OPTIONS,
  LLM_SCENE_DEFAULTS
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
const addId = ref<number | null>(null)
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
        fallback_model_ids: []
      }
  addId.value = null
  error.value = ''
})

const byId = (id: number) => props.models.find((m) => m.id === id)
const primaryKind = computed(() => byId(form.value.model_id)?.kind)

const primaryOptions = computed(() =>
  props.models.map((m) => ({ value: m.id, label: `${m.key} · ${m.kind}` }))
)
const fallbackOptions = computed(() =>
  props.models
    .filter(
      (m) =>
        m.kind === primaryKind.value &&
        m.id !== form.value.model_id &&
        !form.value.fallback_model_ids.includes(m.id)
    )
    .map((m) => ({ value: m.id, label: m.key }))
)

watch(
  () => form.value.model_id,
  () => {
    form.value.fallback_model_ids = form.value.fallback_model_ids.filter(
      (id) => id !== form.value.model_id && byId(id)?.kind === primaryKind.value
    )
  }
)

watch(addId, (id) => {
  if (id == null) return
  form.value.fallback_model_ids.push(id)
  nextTick(() => (addId.value = null))
})

const move = (i: number, delta: number) => {
  const list = form.value.fallback_model_ids
  const j = i + delta
  if (j < 0 || j >= list.length) return
  ;[list[i], list[j]] = [list[j]!, list[i]!]
}

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
  saving.value = true
  try {
    const res = await api.put<LlmScene>(
      `${LLM_API}/scenes/${encodeURIComponent(k)}`,
      { ...form.value }
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
        <div class="space-y-2">
          <p class="text-default-500 text-sm">
            备选模型(按顺序尝试,只能选与主模型同类型的模型)
          </p>
          <div
            v-for="(id, i) in form.fallback_model_ids"
            :key="id"
            class="border-default-200 flex items-center gap-2 rounded-lg border px-3 py-1.5"
          >
            <span class="text-default-400 w-5 text-xs tabular-nums">
              {{ i + 1 }}
            </span>
            <span class="text-foreground flex-1 font-mono text-sm">
              {{ byId(id)?.key ?? `#${id}` }}
            </span>
            <KunButton
              size="sm"
              variant="light"
              is-icon-only
              aria-label="上移"
              :disabled="i === 0"
              @click="move(i, -1)"
            >
              <KunIcon name="lucide:arrow-up" class="size-4" />
            </KunButton>
            <KunButton
              size="sm"
              variant="light"
              is-icon-only
              aria-label="下移"
              :disabled="i === form.fallback_model_ids.length - 1"
              @click="move(i, 1)"
            >
              <KunIcon name="lucide:arrow-down" class="size-4" />
            </KunButton>
            <KunButton
              size="sm"
              variant="light"
              color="danger"
              is-icon-only
              aria-label="移除"
              @click="form.fallback_model_ids.splice(i, 1)"
            >
              <KunIcon name="lucide:x" class="size-4" />
            </KunButton>
          </div>
          <KunSelect
            v-model="addId"
            :options="fallbackOptions"
            :disabled="!fallbackOptions.length"
            :placeholder="
              fallbackOptions.length
                ? '添加备选模型…'
                : '没有可添加的同类型模型'
            "
            aria-label="添加备选模型"
          />
        </div>
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
