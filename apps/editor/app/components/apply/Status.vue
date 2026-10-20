<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { KunUIColor } from '@kungal/ui-core'
import type { EditorApplication } from '#shared/types/application'

const props = defineProps<{
  application: EditorApplication
  roleActive: boolean
}>()

const STATUS: Record<
  EditorApplication['status'],
  { label: string; color: KunUIColor; icon: string; tone: string }
> = {
  pending: {
    label: '审核中',
    color: 'warning',
    icon: 'lucide:clock',
    tone: 'text-warning-600'
  },
  approved: {
    label: '已通过',
    color: 'success',
    icon: 'lucide:badge-check',
    tone: 'text-success-600'
  },
  declined: {
    label: '未通过',
    color: 'danger',
    icon: 'lucide:circle-x',
    tone: 'text-danger-600'
  }
}

const status = computed(() => STATUS[props.application.status])

const now = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  ticker = setInterval(() => {
    now.value = Date.now()
  }, 30_000)
})
onBeforeUnmount(() => clearInterval(ticker))

const waitMs = computed(() =>
  reapplyWaitMs(props.application.reapply_after, now.value)
)
</script>

<template>
  <KunCard bordered padding="md">
    <div class="flex items-start gap-3">
      <KunIcon
        :name="status.icon"
        :class="cn('mt-0.5 size-5 shrink-0', status.tone)"
      />
      <div class="min-w-0 flex-1 space-y-2">
        <div class="flex flex-wrap items-center gap-2">
          <h2 class="font-semibold">我的申请</h2>
          <KunChip size="sm" variant="flat" :color="status.color">
            {{ status.label }}
          </KunChip>
          <span class="text-default-400 text-xs">
            提交于 {{ formatDateTime(props.application.created_at) }}
          </span>
        </div>

        <p
          class="text-default-600 bg-default-50 rounded-lg p-3 text-sm whitespace-pre-line"
        >
          {{ props.application.reason }}
        </p>

        <p
          v-if="props.application.status === 'pending'"
          class="text-default-500 text-sm"
        >
          管理员审核后会更新这里的状态，请耐心等待。
        </p>

        <template v-else-if="props.application.status === 'approved'">
          <p class="text-success-600 text-sm">
            申请已于
            {{ formatDateTime(props.application.reviewed_at) }}
            通过。
          </p>
          <p v-if="!props.roleActive" class="text-default-500 text-sm">
            新权限会在登录凭证刷新时生效，通常不超过 15
            分钟；也可以退出后重新登录立即生效。
          </p>
        </template>

        <template v-else>
          <p class="text-sm">
            <span class="text-danger-600">未通过</span>
            <template v-if="props.application.reviewed_at">
              （{{ formatDateTime(props.application.reviewed_at) }}）
            </template>
            <template v-if="props.application.decline_reason">
              ：{{ props.application.decline_reason }}
            </template>
          </p>
          <p v-if="waitMs > 0" class="text-default-500 text-sm">
            冷却期还剩
            <span class="text-foreground font-medium tabular-nums">{{
              formatWait(waitMs)
            }}</span>
            ，{{ formatDateTime(props.application.reapply_after) }}
            后可以再次申请。
          </p>
          <p v-else class="text-default-500 text-sm">
            冷却期已过，可以在下方重新提交申请。
          </p>
        </template>
      </div>
    </div>
  </KunCard>
</template>
