<script setup lang="ts">
import {
  DEV_TIER_OPTIONS,
  type DevTier,
  devTierLimitHint,
} from '~/constants/devapi'
import type { DevApp } from '~~/shared/types/devapi'

const props = defineProps<{ app: DevApp | null }>()
const emit = defineEmits<{ updated: [] }>()

const open = defineModel<boolean>('open', { required: true })
const api = useApi()

const tier = ref<DevTier>('free')
const ratePerMin = ref<number | null>(null)
const quotaDaily = ref<number | null>(null)
const ownerUserId = ref<number | null>(null)
const settlementEligible = ref(false)
const error = ref('')
const isLoading = ref(false)

watch(open, (v) => {
  if (!v || !props.app) return
  tier.value = (props.app.dev_tier as DevTier) ?? 'free'
  ratePerMin.value = props.app.dev_rate_per_min
  quotaDaily.value = props.app.dev_quota_daily
  ownerUserId.value = props.app.owner_user_id ?? null
  settlementEligible.value = props.app.store_settlement_eligible
  error.value = ''
})

const handleSubmit = async () => {
  const app = props.app
  if (!app) return
  error.value = ''
  isLoading.value = true
  try {
    const body: Record<string, unknown> = {
      dev_tier: tier.value,
      dev_rate_per_min: ratePerMin.value ?? 0,
      dev_quota_daily: quotaDaily.value ?? 0,
      owner_user_id: ownerUserId.value ?? 0,
      store_settlement_eligible: settlementEligible.value,
    }
    const res = await api.patch(`/admin/devapi/apps/${app.client_id}`, body)
    if (res.code === 0) {
      useKunMessage('配置已更新', 'success')
      emit('updated')
    } else {
      error.value = res.message || '更新失败'
    }
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" size="md" aria-label="应用配置">
    <div class="space-y-4">
      <h2 class="text-xl font-bold text-foreground">应用配置</h2>

      <div class="rounded-lg bg-default-50 p-3">
        <p class="text-xs text-default-400">{{ app?.name }}</p>
        <p class="mt-1 truncate font-mono text-sm text-foreground">{{ app?.client_id }}</p>
      </div>

      <KunSelect
        v-model="tier"
        label="Tier（等级）"
        :options="DEV_TIER_OPTIONS"
        :description="`默认限额：${devTierLimitHint(tier)}`"
      />

      <KunNumberInput
        v-model="ratePerMin"
        label="限流覆盖（次/分）"
        :min="0"
        description="0 = 使用 tier 默认值"
      />

      <KunNumberInput
        v-model="quotaDaily"
        label="日配额覆盖（次/日）"
        :min="0"
        description="0 = 使用 tier 默认值"
      />

      <KunNumberInput
        v-model="ownerUserId"
        label="归属用户 ID（owner）"
        :min="0"
        description="第三方开发者应用的所有者用户 ID；0 = 不指定"
      />

      <div class="rounded-lg border border-default-200 p-3">
        <KunSwitch
          v-model="settlementEligible"
          label="DLsite 结算名单（分成资格）"
          description="铸造商店短链是自助的，人人可用；但每月的优惠券池是固定的一份，按去重后的点击占比切分——名单里每多一个参与者，其余人分到的就更少。这个开关决定谁真正参与分成，不是谁可以铸链。"
        />
      </div>

      <div class="rounded-lg border border-default-200 p-3">
        <div class="flex items-center gap-2 text-sm text-default-500">
          <KunIcon name="lucide:info" class="size-4 text-default-400" />
          NSFW 内容已全量公开，无需授权
        </div>
        <p class="mt-1 text-xs text-default-400">
          — 任意密钥均可请求 nsfw=true；不传该参数时仍默认只返回 SFW 内容。
        </p>
      </div>

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
        {{ error }}
      </div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :disabled="isLoading" @click="handleSubmit">
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          保存
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
