<script setup lang="ts">
import { LLM_ACCESS_KEY_HINT, LLM_API } from '~/constants/llm'
import type { LlmAccessKey, LlmModel, LlmScene } from '~~/shared/types/llm'

defineProps<{
  accessKeys: LlmAccessKey[]
  models: LlmModel[]
  scenes: LlmScene[]
}>()
const emit = defineEmits<{ changed: [] }>()

const api = useApi('ai')
const busy = ref(0)

const modalOpen = ref(false)
const editing = ref<LlmAccessKey | null>(null)
const openModal = (k: LlmAccessKey | null) => {
  editing.value = k
  modalOpen.value = true
}

const secret = ref('')
const secretOpen = ref(false)
const onCreated = (s: string) => {
  secret.value = s
  secretOpen.value = true
  emit('changed')
}

const toggle = async (k: LlmAccessKey, enabled: boolean) => {
  busy.value = k.id
  try {
    const res = await api.patch(`${LLM_API}/access-keys/${k.id}`, { enabled })
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

const remove = async (k: LlmAccessKey) => {
  const confirmed = await useKunAlert({
    title: `删除访问密钥「${k.name}」?`,
    message: '使用该密钥的调用方会立即收到 401。',
    type: 'danger',
    confirmText: '删除',
    confirmColor: 'danger'
  })
  if (!confirmed) return
  busy.value = k.id
  try {
    const res = await api.delete(`${LLM_API}/access-keys/${k.id}`)
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
      <p class="text-default-500 text-sm">{{ LLM_ACCESS_KEY_HINT }}</p>
      <KunButton color="primary" @click="openModal(null)">
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        新建密钥
      </KunButton>
    </div>

    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[48rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">名称</th>
            <th class="px-3 py-2 text-left font-medium">前缀</th>
            <th class="px-3 py-2 text-left font-medium">允许的模型 / 场景</th>
            <th class="px-3 py-2 text-left font-medium">启用</th>
            <th class="px-3 py-2 text-left font-medium">最近使用</th>
            <th class="px-3 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="k in accessKeys"
            :key="k.id"
            class="border-default-200 border-t"
          >
            <td class="text-foreground px-3 py-2 font-medium">{{ k.name }}</td>
            <td class="text-default-500 px-3 py-2 font-mono text-xs">
              {{ k.prefix }}…
            </td>
            <td class="px-3 py-2">
              <div v-if="k.allowed_models.length" class="flex flex-wrap gap-1">
                <KunChip
                  v-for="m in k.allowed_models"
                  :key="m"
                  color="info"
                  variant="flat"
                  size="xs"
                >
                  {{ m }}
                </KunChip>
              </div>
              <span v-else class="text-default-400">全部</span>
            </td>
            <td class="px-3 py-2">
              <KunSwitch
                :model-value="k.enabled"
                :disabled="busy === k.id"
                @update:model-value="toggle(k, $event)"
              />
            </td>
            <td class="text-default-500 px-3 py-2 text-xs">
              {{ k.last_used_at ? llmTime(k.last_used_at) : '从未使用' }}
            </td>
            <td class="px-3 py-2 text-right whitespace-nowrap">
              <KunButton size="sm" variant="light" @click="openModal(k)">
                编辑
              </KunButton>
              <KunButton
                size="sm"
                variant="light"
                color="danger"
                :loading="busy === k.id"
                @click="remove(k)"
              >
                删除
              </KunButton>
            </td>
          </tr>
          <tr v-if="!accessKeys.length">
            <td colspan="6" class="text-default-400 px-3 py-8 text-center">
              还没有访问密钥
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <AiLlmAccessKeyModal
      v-model="modalOpen"
      :access-key="editing"
      :models="models"
      :scenes="scenes"
      @saved="emit('changed')"
      @created="onCreated"
    />
    <AiLlmSecretModal v-model="secretOpen" :secret="secret" />
  </div>
</template>
