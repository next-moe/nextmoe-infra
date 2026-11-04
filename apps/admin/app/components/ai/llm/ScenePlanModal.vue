<script setup lang="ts">
import {
  LLM_API,
  LLM_PLAN_STATE_META,
  LLM_SCENE_STRATEGY_META
} from '~/constants/llm'
import type {
  LlmPlanStep,
  LlmScene,
  LlmScenePlan,
  LlmSceneStrategy
} from '~~/shared/types/llm'

const props = defineProps<{ scene: LlmScene | null }>()
const open = defineModel<boolean>({ required: true })

const api = useApi('ai')

const affinity = ref('')
const plan = ref<LlmScenePlan | null>(null)
const loading = ref(false)
const error = ref('')

const load = async () => {
  if (!props.scene) return
  loading.value = true
  error.value = ''
  try {
    const res = await api.get<LlmScenePlan>(
      `${LLM_API}/scenes/${encodeURIComponent(props.scene.key)}/plan`,
      { affinity: affinity.value.trim() || undefined }
    )
    if (res.code !== 0) {
      error.value = res.message || '试运行失败'
      plan.value = null
      return
    }
    plan.value = res.data
  } finally {
    loading.value = false
  }
}

watch(open, (v) => {
  if (!v) return
  affinity.value = ''
  plan.value = null
  load()
})

const strategyLabel = computed(() => {
  const s = plan.value?.strategy ?? ''
  return LLM_SCENE_STRATEGY_META[s as LlmSceneStrategy]?.label ?? s
})

const stateMeta = (step: LlmPlanStep) => LLM_PLAN_STATE_META[step.state]
</script>

<template>
  <KunModal
    v-model="open"
    :title="scene ? `试运行「${scene.key}」` : '试运行'"
    size="lg"
  >
    <div class="space-y-4">
      <p class="text-default-500 text-sm">
        按当前的健康状态和路由策略,算出一次调用会依次尝试哪些路由;不会真的发请求。
      </p>
      <form class="flex items-end gap-2" @submit.prevent="load">
        <KunInput
          v-model="affinity"
          label="会话 ID（看加权策略会落到哪条路由）"
          placeholder="可选,例如 conv_123"
          class="flex-1"
        />
        <KunButton color="primary" type="submit" :loading="loading">
          计算
        </KunButton>
      </form>

      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
        {{ error }}
      </div>

      <template v-if="plan">
        <p class="text-default-500 text-sm">
          策略:<span class="text-foreground">{{ strategyLabel }}</span> · 共
          {{ plan.steps.length }} 步
        </p>
        <div
          class="border-default-200 divide-default-200 divide-y overflow-hidden rounded-xl border"
        >
          <div
            v-for="step in plan.steps"
            :key="`${step.order}-${step.route_id}`"
            class="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm"
          >
            <span class="text-default-400 w-6 text-xs tabular-nums">
              {{ step.order }}
            </span>
            <KunChip
              :color="step.chain === 'primary' ? 'primary' : 'default'"
              variant="flat"
              size="xs"
            >
              {{ step.chain === 'primary' ? '主' : '备' }}
            </KunChip>
            <span class="text-foreground font-mono">{{ step.model_key }}</span>
            <span class="text-default-500 min-w-0 text-xs break-all">
              #{{ step.route_id }} {{ step.provider }} ·
              <span class="font-mono">{{ step.upstream_id }}</span>
            </span>
            <span class="text-default-400 text-xs tabular-nums">
              优先级 {{ step.priority }} · 权重 {{ step.weight }}
            </span>
            <KunChip
              :color="stateMeta(step).color"
              variant="flat"
              size="xs"
              class-name="ml-auto"
            >
              {{ stateMeta(step).label }}
            </KunChip>
          </div>
          <p
            v-if="!plan.steps.length"
            class="text-default-400 px-3 py-6 text-center text-sm"
          >
            没有可用的路由
          </p>
        </div>

        <div v-if="plan.skipped.length" class="space-y-2">
          <p class="text-default-500 text-sm">
            跳过(冷却中,其它路由都失败时才会被尝试)
          </p>
          <div
            class="border-warning-200 divide-warning-200 divide-y overflow-hidden rounded-xl border"
          >
            <div
              v-for="step in plan.skipped"
              :key="`skip-${step.route_id}-${step.model_key}`"
              class="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm"
            >
              <span class="text-foreground font-mono">
                {{ step.model_key }}
              </span>
              <span class="text-default-500 min-w-0 text-xs break-all">
                #{{ step.route_id }} {{ step.provider }} ·
                <span class="font-mono">{{ step.upstream_id }}</span>
              </span>
              <KunChip
                :color="stateMeta(step).color"
                variant="flat"
                size="xs"
                class-name="ml-auto"
              >
                {{ stateMeta(step).label }}
              </KunChip>
            </div>
          </div>
        </div>
      </template>

      <div class="flex justify-end">
        <KunButton color="default" variant="flat" @click="open = false">
          关闭
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
