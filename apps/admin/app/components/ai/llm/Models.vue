<script setup lang="ts">
import {
  LLM_API,
  LLM_MODEL_CAPABILITIES,
  LLM_MODEL_KIND_META
} from '~/constants/llm'
import type { LlmModel } from '~~/shared/types/llm'

defineProps<{ models: LlmModel[] }>()
const emit = defineEmits<{ changed: [] }>()

const api = useApi('ai')
const busy = ref(0)

const modalOpen = ref(false)
const editing = ref<LlmModel | null>(null)
const openModal = (m: LlmModel | null) => {
  editing.value = m
  modalOpen.value = true
}

const toggle = async (m: LlmModel, enabled: boolean) => {
  busy.value = m.id
  try {
    const res = await api.patch(`${LLM_API}/models/${m.id}`, { enabled })
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

const remove = async (m: LlmModel) => {
  const confirmed = await useKunAlert({
    title: `删除模型「${m.key}」?`,
    message: '仍有路由或场景引用该模型时会被拒绝。',
    type: 'danger',
    confirmText: '删除',
    confirmColor: 'danger'
  })
  if (!confirmed) return
  busy.value = m.id
  try {
    const res = await api.delete(`${LLM_API}/models/${m.id}`)
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
        模型 key 是平台内的稳定名,场景与访问密钥都按它引用。
      </p>
      <KunButton color="primary" @click="openModal(null)">
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        新建模型
      </KunButton>
    </div>

    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[48rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">Key</th>
            <th class="px-3 py-2 text-left font-medium">类型</th>
            <th class="px-3 py-2 text-right font-medium">上下文 / 最大输出</th>
            <th class="px-3 py-2 text-left font-medium">能力</th>
            <th class="px-3 py-2 text-left font-medium">启用</th>
            <th class="px-3 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="m in models"
            :key="m.id"
            class="border-default-200 border-t"
          >
            <td class="text-foreground px-3 py-2 font-mono">{{ m.key }}</td>
            <td class="px-3 py-2">
              <KunChip
                :color="LLM_MODEL_KIND_META[m.kind]?.color ?? 'default'"
                variant="flat"
                size="xs"
              >
                {{ LLM_MODEL_KIND_META[m.kind]?.label ?? m.kind }}
              </KunChip>
            </td>
            <td class="px-3 py-2 text-right tabular-nums">
              {{ m.context_tokens.toLocaleString() }} /
              {{ m.max_output.toLocaleString() }}
            </td>
            <td class="px-3 py-2">
              <div class="flex flex-wrap gap-1">
                <template v-for="c in LLM_MODEL_CAPABILITIES" :key="c.key">
                  <KunChip
                    v-if="m[c.key]"
                    color="secondary"
                    variant="flat"
                    size="xs"
                  >
                    {{ c.label }}
                  </KunChip>
                </template>
              </div>
            </td>
            <td class="px-3 py-2">
              <KunSwitch
                :model-value="m.enabled"
                :disabled="busy === m.id"
                @update:model-value="toggle(m, $event)"
              />
            </td>
            <td class="px-3 py-2 text-right whitespace-nowrap">
              <KunButton size="sm" variant="light" @click="openModal(m)">
                编辑
              </KunButton>
              <KunButton
                size="sm"
                variant="light"
                color="danger"
                :loading="busy === m.id"
                @click="remove(m)"
              >
                删除
              </KunButton>
            </td>
          </tr>
          <tr v-if="!models.length">
            <td colspan="6" class="text-default-400 px-3 py-8 text-center">
              还没有模型
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <AiLlmModelModal
      v-model="modalOpen"
      :item="editing"
      @saved="emit('changed')"
    />
  </div>
</template>
