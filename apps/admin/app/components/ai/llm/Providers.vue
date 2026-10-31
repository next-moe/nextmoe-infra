<script setup lang="ts">
import { LLM_API } from '~/constants/llm'
import type { LlmProvider } from '~~/shared/types/llm'

const props = defineProps<{
  providers: LlmProvider[]
  kekConfigured: boolean
}>()
const emit = defineEmits<{ changed: []; adopted: [] }>()

const api = useApi('ai')
const busy = ref(0)

const modalOpen = ref(false)
const editing = ref<LlmProvider | null>(null)
const openModal = (p: LlmProvider | null) => {
  editing.value = p
  modalOpen.value = true
}

const catalogOpen = ref(false)
const catalogId = ref(0)
const catalogFor = computed(
  () => props.providers.find((p) => p.id === catalogId.value) ?? null
)
const openCatalog = (p: LlmProvider) => {
  catalogId.value = p.id
  catalogOpen.value = true
}

const toggle = async (p: LlmProvider, enabled: boolean) => {
  busy.value = p.id
  try {
    const res = await api.patch(`${LLM_API}/providers/${p.id}`, { enabled })
    if (res.code !== 0) {
      useKunMessage(res.message || '更新失败', 'error')
      return
    }
    useKunMessage(enabled ? '已启用' : '已停用', 'success')
    emit('changed')
  } finally {
    busy.value = 0
  }
}

const remove = async (p: LlmProvider) => {
  const confirmed = await useKunAlert({
    title: `删除供应商「${p.name}」?`,
    message: '仍有路由在使用该供应商时会被拒绝,请先删除相关路由。',
    type: 'danger',
    confirmText: '删除',
    confirmColor: 'danger'
  })
  if (!confirmed) return
  busy.value = p.id
  try {
    const res = await api.delete(`${LLM_API}/providers/${p.id}`)
    if (res.code !== 0) {
      useKunMessage(res.message || '删除失败', 'error')
      return
    }
    useKunMessage('已删除', 'success')
    emit('changed')
  } finally {
    busy.value = 0
  }
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-3">
      <p class="text-default-500 text-sm">
        OpenAI 兼容的上游端点;API key 只写不读,列表只显示末尾几位。
      </p>
      <KunButton color="primary" @click="openModal(null)">
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        新建供应商
      </KunButton>
    </div>

    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[48rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">名称</th>
            <th class="px-3 py-2 text-left font-medium">Base URL</th>
            <th class="px-3 py-2 text-left font-medium">API key</th>
            <th class="px-3 py-2 text-right font-medium">价格倍率</th>
            <th class="px-3 py-2 text-left font-medium">启用</th>
            <th class="px-3 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="p in providers"
            :key="p.id"
            class="border-default-200 border-t"
          >
            <td class="text-foreground px-3 py-2 font-medium">{{ p.name }}</td>
            <td class="text-default-500 px-3 py-2 font-mono text-xs break-all">
              {{ p.base_url }}
            </td>
            <td class="px-3 py-2">
              <KunChip
                v-if="p.key_configured"
                color="success"
                variant="flat"
                size="xs"
              >
                已配置 {{ p.key_hint }}
              </KunChip>
              <KunChip v-else color="warning" variant="flat" size="xs">
                未配置
              </KunChip>
            </td>
            <td class="px-3 py-2 text-right tabular-nums">
              {{ llmPpmToMultiplier(p.price_multiplier_ppm) }}
            </td>
            <td class="px-3 py-2">
              <KunSwitch
                :model-value="p.enabled"
                :disabled="busy === p.id"
                @update:model-value="toggle(p, $event)"
              />
            </td>
            <td class="px-3 py-2 text-right whitespace-nowrap">
              <KunButton size="sm" variant="flat" @click="openCatalog(p)">
                可用模型
              </KunButton>
              <KunButton size="sm" variant="light" @click="openModal(p)">
                编辑
              </KunButton>
              <KunButton
                size="sm"
                variant="light"
                color="danger"
                :loading="busy === p.id"
                @click="remove(p)"
              >
                删除
              </KunButton>
            </td>
          </tr>
          <tr v-if="!providers.length">
            <td colspan="6" class="text-default-400 px-3 py-8 text-center">
              还没有供应商
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <AiLlmProviderModal
      v-model="modalOpen"
      :provider="editing"
      :kek-configured="kekConfigured"
      @saved="emit('changed')"
    />
    <AiLlmCatalog
      v-model="catalogOpen"
      :provider="catalogFor"
      @adopted="emit('adopted')"
      @synced="emit('changed')"
    />
  </div>
</template>
