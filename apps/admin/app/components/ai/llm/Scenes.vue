<script setup lang="ts">
import {
  LLM_API,
  LLM_SCENE_CLASS_META,
  LLM_SCENE_REASONING_META,
  LLM_SCENE_STRATEGY_META
} from '~/constants/llm'
import type { LlmModel, LlmScene } from '~~/shared/types/llm'

const props = defineProps<{ scenes: LlmScene[]; models: LlmModel[] }>()
const emit = defineEmits<{ changed: [] }>()

const api = useApi('ai')
const busy = ref('')

const modelKey = (id: number) =>
  props.models.find((m) => m.id === id)?.key ?? `#${id}`

const modalOpen = ref(false)
const editing = ref<LlmScene | null>(null)
const openModal = (s: LlmScene | null) => {
  editing.value = s
  modalOpen.value = true
}

const planOpen = ref(false)
const planning = ref<LlmScene | null>(null)
const openPlan = (s: LlmScene) => {
  planning.value = s
  planOpen.value = true
}

const extraChains = (s: LlmScene) =>
  [
    { label: '超长时', ids: s.context_fallback_model_ids },
    { label: '被过滤时', ids: s.content_fallback_model_ids }
  ].filter((c) => c.ids.length)

const remove = async (s: LlmScene) => {
  const confirmed = await useKunAlert({
    title: `删除场景「${s.key}」?`,
    message: '仍在按这个场景调用的服务会立即失败。',
    type: 'danger',
    confirmText: '删除',
    confirmColor: 'danger'
  })
  if (!confirmed) return
  busy.value = s.key
  try {
    const res = await api.delete(
      `${LLM_API}/scenes/${encodeURIComponent(s.key)}`
    )
    if (res.code !== 0) {
      useKunMessage(res.message || '删除失败', 'error')
      return
    }
    useKunMessage('已删除', 'success')
    emit('changed')
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-3">
      <p class="text-default-500 text-sm">
        调用方按场景请求;主模型的路由全部失败后,依次尝试备选模型;输入超长或被内容过滤时改走对应的链。
      </p>
      <KunButton
        color="primary"
        :disabled="!models.length"
        @click="openModal(null)"
      >
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        新建场景
      </KunButton>
    </div>

    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[64rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">场景</th>
            <th class="px-3 py-2 text-left font-medium">模型链</th>
            <th class="px-3 py-2 text-left font-medium">类别 / 策略</th>
            <th class="px-3 py-2 text-left font-medium">今日花费</th>
            <th class="px-3 py-2 text-right font-medium">温度 / 最大输出</th>
            <th class="px-3 py-2 text-right font-medium">
              超时 首字 / 空闲 / 总 (ms)
            </th>
            <th class="px-3 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="s in scenes"
            :key="s.key"
            class="border-default-200 border-t"
          >
            <td class="px-3 py-2">
              <p class="text-foreground font-mono">{{ s.key }}</p>
              <KunChip
                v-if="!s.ready"
                color="danger"
                variant="flat"
                size="xs"
                class-name="mt-1"
              >
                无可用路由
              </KunChip>
            </td>
            <td class="px-3 py-2">
              <div class="flex flex-wrap items-center gap-1">
                <KunChip color="primary" variant="flat" size="xs">
                  {{ modelKey(s.model_id) }}
                </KunChip>
                <template v-for="id in s.fallback_model_ids" :key="id">
                  <KunIcon
                    name="lucide:arrow-right"
                    class="text-default-400 size-3"
                  />
                  <KunChip color="default" variant="flat" size="xs">
                    {{ modelKey(id) }}
                  </KunChip>
                </template>
              </div>
              <div
                v-for="c in extraChains(s)"
                :key="c.label"
                class="mt-1 flex flex-wrap items-center gap-1"
              >
                <span class="text-default-400 text-xs">{{ c.label }}</span>
                <template v-for="(id, i) in c.ids" :key="id">
                  <KunIcon
                    v-if="i"
                    name="lucide:arrow-right"
                    class="text-default-400 size-3"
                  />
                  <KunChip color="warning" variant="flat" size="xs">
                    {{ modelKey(id) }}
                  </KunChip>
                </template>
              </div>
            </td>
            <td class="px-3 py-2">
              <p>{{ LLM_SCENE_CLASS_META[s.class] ?? s.class }}</p>
              <p class="text-default-500 text-xs">
                {{ LLM_SCENE_STRATEGY_META[s.strategy]?.label ?? s.strategy }}
                · 思考{{
                  LLM_SCENE_REASONING_META[s.reasoning]?.label ?? s.reasoning
                }}
              </p>
            </td>
            <td class="px-3 py-2">
              <AiLlmBudgetUsage
                :spent-nano="s.spent_today_nano"
                :budget-micro="s.daily_budget_micro"
              />
            </td>
            <td class="px-3 py-2 text-right tabular-nums">
              {{ s.temperature ?? '默认' }} /
              {{ s.max_output ? s.max_output.toLocaleString() : '默认' }}
            </td>
            <td class="px-3 py-2 text-right tabular-nums">
              {{ s.ttft_ms.toLocaleString() }} /
              {{ s.idle_ms.toLocaleString() }} /
              {{ s.total_ms.toLocaleString() }}
            </td>
            <td class="px-3 py-2 text-right whitespace-nowrap">
              <KunButton size="sm" variant="flat" @click="openPlan(s)">
                试运行
              </KunButton>
              <KunButton size="sm" variant="light" @click="openModal(s)">
                编辑
              </KunButton>
              <KunButton
                size="sm"
                variant="light"
                color="danger"
                :loading="busy === s.key"
                @click="remove(s)"
              >
                删除
              </KunButton>
            </td>
          </tr>
          <tr v-if="!scenes.length">
            <td colspan="7" class="text-default-400 px-3 py-8 text-center">
              还没有场景
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <AiLlmSceneModal
      v-model="modalOpen"
      :scene="editing"
      :models="models"
      :taken-keys="scenes.map((s) => s.key)"
      @saved="emit('changed')"
    />
    <AiLlmScenePlanModal v-model="planOpen" :scene="planning" />
  </div>
</template>
