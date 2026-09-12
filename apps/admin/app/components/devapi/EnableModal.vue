<script setup lang="ts">
import {
  DEV_TIER_OPTIONS,
  type DevTier,
  devTierLimitHint,
} from '~/constants/devapi'

const props = defineProps<{ candidates: OAuthClient[] }>()
const emit = defineEmits<{ enabled: [] }>()

const open = defineModel<boolean>('open', { required: true })
const api = useApi()

const clientId = ref<string>('')
const tier = ref<DevTier>('free')
const error = ref('')
const isLoading = ref(false)

const clientOptions = computed(() =>
  props.candidates.map((c) => ({ value: c.id, label: `${c.name}（${c.id}）` }))
)

watch(open, (v) => {
  if (!v) return
  clientId.value = ''
  tier.value = 'free'
  error.value = ''
})

const handleSubmit = async () => {
  error.value = ''
  if (!clientId.value) {
    error.value = '请选择要启用的客户端'
    return
  }
  isLoading.value = true
  try {
    const body: Record<string, unknown> = { dev_enabled: true, dev_tier: tier.value }
    const res = await api.patch(`/admin/devapi/apps/${clientId.value}`, body)
    if (res.code === 0) {
      useKunMessage('应用已启用进开发者平台', 'success')
      emit('enabled')
    } else {
      error.value = res.message || '启用失败'
    }
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" size="md" aria-label="启用新应用">
    <div class="space-y-4">
      <h2 class="text-xl font-bold text-foreground">启用新应用</h2>
      <p class="text-sm text-default-500">
        从既有 OAuth 客户端中选择一个，将其接入 NextMoe 开放 API 平台。
      </p>

      <KunSelect
        v-if="candidates.length"
        v-model="clientId"
        label="OAuth 客户端"
        placeholder="请选择客户端"
        :options="clientOptions"
      />
      <div
        v-else
        class="rounded-lg bg-default-50 p-4 text-center text-sm text-default-400"
      >
        没有可启用的客户端 — 所有客户端均已接入，或尚未在「OAuth 客户端」中创建。
      </div>

      <KunSelect
        v-model="tier"
        label="初始 Tier"
        :options="DEV_TIER_OPTIONS"
        :description="`默认限额：${devTierLimitHint(tier)}`"
      />

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
        {{ error }}
      </div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton
          color="primary"
          :disabled="isLoading || !candidates.length"
          @click="handleSubmit"
        >
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          启用
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
