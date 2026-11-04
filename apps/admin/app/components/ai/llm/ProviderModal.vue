<script setup lang="ts">
import { LLM_API } from '~/constants/llm'
import type {
  LlmProvider,
  LlmProviderCreateRequest,
  LlmProviderPatchRequest
} from '~~/shared/types/llm'

const props = defineProps<{
  provider: LlmProvider | null
  kekConfigured: boolean
}>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const api = useApi('ai')

const name = ref('')
const baseUrl = ref('')
const apiKey = ref('')
const clearKey = ref(false)
const headers = ref<{ k: string; v: string }[]>([])
const multiplier = ref('1')
const enabled = ref(true)
const saving = ref(false)
const error = ref('')

watch(open, (v) => {
  if (!v) return
  const p = props.provider
  name.value = p?.name ?? ''
  baseUrl.value = p?.base_url ?? ''
  apiKey.value = ''
  clearKey.value = false
  headers.value = Object.entries(p?.headers ?? {}).map(([k, val]) => ({
    k,
    v: val
  }))
  multiplier.value = String((p?.price_multiplier_ppm ?? 1e6) / 1e6)
  enabled.value = p?.enabled ?? true
  error.value = ''
})

const keyDisabled = computed(() => !props.kekConfigured || clearKey.value)

const save = async () => {
  error.value = ''
  const n = name.value.trim()
  const url = baseUrl.value.trim()
  if (!n || !url) {
    error.value = '名称和 Base URL 是必填的'
    return
  }
  const mult = Number(multiplier.value)
  if (!Number.isFinite(mult) || mult < 0) {
    error.value = '价格倍率须为非负数'
    return
  }
  const headerMap: Record<string, string> = {}
  for (const h of headers.value) {
    if (h.k.trim()) headerMap[h.k.trim()] = h.v
  }
  const key = keyDisabled.value ? '' : apiKey.value.trim()
  const common = {
    name: n,
    base_url: url,
    headers: headerMap,
    price_multiplier_ppm: Math.round(mult * 1e6),
    enabled: enabled.value
  }

  saving.value = true
  try {
    let res
    if (props.provider) {
      const body: LlmProviderPatchRequest = {
        ...common,
        ...(key ? { api_key: key } : {}),
        ...(clearKey.value ? { clear_key: true } : {})
      }
      res = await api.patch<LlmProvider>(
        `${LLM_API}/providers/${props.provider.id}`,
        { ...body }
      )
    } else {
      const body: LlmProviderCreateRequest = {
        ...common,
        ...(key ? { api_key: key } : {})
      }
      res = await api.post<LlmProvider>(`${LLM_API}/providers`, { ...body })
    }
    if (res.code !== 0) {
      error.value = res.message || '保存失败'
      return
    }
    useKunMessage(props.provider ? '已保存' : '已创建', 'success')
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
    :title="provider ? `编辑供应商「${provider.name}」` : '新建供应商'"
    size="lg"
  >
    <div class="space-y-4">
      <div class="grid gap-3 sm:grid-cols-2">
        <KunInput
          v-model="name"
          label="名称"
          placeholder="workers-ai"
          required
        />
        <KunInput
          v-model="multiplier"
          label="价格倍率"
          type="number"
          min="0"
          step="0.01"
          description="1 = 按路由标价计费"
        />
      </div>
      <KunInput
        v-model="baseUrl"
        label="Base URL"
        placeholder="https://api.example.com/v1"
        required
      />

      <div class="border-default-200 space-y-3 rounded-lg border p-4">
        <KunInput
          v-model="apiKey"
          label="API key"
          type="password"
          reveal-password
          autocomplete="new-password"
          :disabled="keyDisabled"
          :placeholder="
            provider?.key_configured ? '留空 = 保留现有 key' : '未配置'
          "
          :description="
            provider?.key_configured
              ? `当前 key:${provider.key_hint};填入新值会替换它`
              : 'key 只写不读,保存后无法再查看'
          "
        />
        <KunSwitch
          v-if="provider?.key_configured"
          v-model="clearKey"
          label="清除 key"
          description="保存后删除已存的 key,此供应商的路由将无法调用"
        />
        <p v-if="!kekConfigured" class="text-warning-600 text-xs">
          ai 服务未设置 KUN_LLM_KEK,暂时无法保存 key
        </p>
      </div>

      <div class="border-default-200 space-y-2 rounded-lg border p-4">
        <div class="flex items-center justify-between">
          <span class="text-default-500 text-sm">额外请求头</span>
          <KunButton
            size="sm"
            variant="light"
            @click="headers.push({ k: '', v: '' })"
          >
            <KunIcon name="lucide:plus" class="mr-1 size-4" />
            添加
          </KunButton>
        </div>
        <div v-for="(h, i) in headers" :key="i" class="flex items-center gap-2">
          <KunInput
            v-model="h.k"
            placeholder="Header 名"
            aria-label="Header 名"
            class="flex-1"
          />
          <KunInput
            v-model="h.v"
            placeholder="值"
            aria-label="Header 值"
            class="flex-1"
          />
          <KunButton
            size="sm"
            variant="light"
            color="danger"
            is-icon-only
            aria-label="删除此请求头"
            @click="headers.splice(i, 1)"
          >
            <KunIcon name="lucide:x" class="size-4" />
          </KunButton>
        </div>
        <p v-if="!headers.length" class="text-default-400 text-xs">无</p>
      </div>

      <KunSwitch v-model="enabled" label="启用" />

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
