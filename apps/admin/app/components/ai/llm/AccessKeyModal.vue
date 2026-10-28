<script setup lang="ts">
import { LLM_API } from '~/constants/llm'
import type {
  LlmAccessKey,
  LlmAccessKeyCreated,
  LlmModel,
  LlmScene
} from '~~/shared/types/llm'

const props = defineProps<{
  accessKey: LlmAccessKey | null
  models: LlmModel[]
  scenes: LlmScene[]
}>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: []; created: [secret: string] }>()

const api = useApi('ai')

const name = ref('')
const allowed = ref<string[]>([])
const saving = ref(false)
const error = ref('')

const options = computed(() => {
  const seen = new Set<string>()
  const out: { value: string; label: string }[] = []
  for (const s of props.scenes) {
    seen.add(s.key)
    out.push({ value: s.key, label: `场景 · ${s.key}` })
  }
  for (const m of props.models) {
    if (seen.has(m.key)) continue
    out.push({ value: m.key, label: `模型 · ${m.key}` })
  }
  for (const k of allowed.value) {
    if (!out.some((o) => o.value === k)) out.push({ value: k, label: k })
  }
  return out
})

watch(open, (v) => {
  if (!v) return
  name.value = props.accessKey?.name ?? ''
  allowed.value = [...(props.accessKey?.allowed_models ?? [])]
  error.value = ''
})

const save = async () => {
  error.value = ''
  const n = name.value.trim()
  if (!n) {
    error.value = '名称是必填的'
    return
  }
  saving.value = true
  try {
    const body = { name: n, allowed_models: allowed.value }
    if (props.accessKey) {
      const res = await api.patch<LlmAccessKey>(
        `${LLM_API}/access-keys/${props.accessKey.id}`,
        body
      )
      if (res.code !== 0) {
        error.value = res.message || '保存失败'
        return
      }
      useKunMessage('已保存', 'success')
      open.value = false
      emit('saved')
      return
    }
    const res = await api.post<LlmAccessKeyCreated>(
      `${LLM_API}/access-keys`,
      body
    )
    if (res.code !== 0 || !res.data?.secret) {
      error.value = res.message || '创建失败'
      return
    }
    open.value = false
    emit('created', res.data.secret)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="accessKey ? `编辑访问密钥「${accessKey.name}」` : '新建访问密钥'"
    size="md"
  >
    <div class="space-y-4">
      <KunInput
        v-model="name"
        label="名称"
        placeholder="例如:moyu-worker"
        description="写清楚是哪个服务在用"
        required
      />
      <KunSelect
        v-model="allowed"
        label="允许的模型 / 场景"
        :options="options"
        multiple
        searchable
        placeholder="不选 = 全部允许"
      />

      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
        {{ error }}
      </div>
      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :loading="saving" @click="save">
          {{ accessKey ? '保存' : '创建' }}
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
