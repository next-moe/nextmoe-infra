<script setup lang="ts">
import {
  DEV_APP_REVIEW_COLORS,
  DEV_APP_REVIEW_LABELS,
  DEV_DISABLED_HINT,
  DEV_TIER_COLORS,
  DEV_TIER_LABELS,
  isAppUnderReview
} from '~/constants/dev'
import type { DevApp } from '~~/shared/types/dev'

const props = defineProps<{
  app: DevApp
  manageDisabled: boolean
  resubmitting: boolean
  deleteLabel: string
}>()

const emit = defineEmits<{
  edit: []
  delete: []
  resubmit: []
}>()

const reviewStatus = computed(() => props.app.review_status ?? '')
const isDeclined = computed(() => reviewStatus.value === 'declined')
const reviewChip = computed(() => {
  if (!isAppUnderReview(reviewStatus.value)) return null
  return {
    label: DEV_APP_REVIEW_LABELS[reviewStatus.value],
    color: DEV_APP_REVIEW_COLORS[reviewStatus.value]
  }
})

const limitLabel = (v: number, unit: string) =>
  v > 0 ? `${v.toLocaleString()} ${unit}` : '不限'
</script>

<template>
  <KunCard content-class="justify-start gap-0" class-name="p-6">
    <div class="flex items-start justify-between gap-4">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2">
          <h2 class="text-foreground text-lg font-semibold">
            {{ app.name }}
          </h2>
          <KunChip
            :color="DEV_TIER_COLORS[app.tier] ?? 'default'"
            variant="flat"
            size="sm"
          >
            {{ DEV_TIER_LABELS[app.tier] ?? app.tier }}
          </KunChip>
          <KunChip
            v-if="reviewChip"
            :color="reviewChip.color"
            variant="flat"
            size="sm"
          >
            {{ reviewChip.label }}
          </KunChip>
        </div>
        <p v-if="app.description" class="text-default-500 mt-1 text-sm">
          {{ app.description }}
        </p>
        <div class="mt-2 flex items-center gap-2">
          <p class="text-default-400 truncate font-mono text-sm">
            {{ app.client_id }}
          </p>
          <KunCopy :text="app.client_id" name="复制" size="sm" />
        </div>
      </div>
      <div class="flex shrink-0 gap-1">
        <KunButton
          v-if="isDeclined"
          color="primary"
          size="sm"
          :disabled="resubmitting"
          @click="emit('resubmit')"
        >
          <KunIcon
            v-if="resubmitting"
            name="lucide:loader-circle"
            class="mr-1 size-4 animate-spin"
          />
          <KunIcon v-else name="lucide:send" class="mr-1 size-4" />
          重新提交
        </KunButton>
        <KunButton
          variant="flat"
          size="sm"
          :disabled="manageDisabled"
          @click="emit('edit')"
        >
          <KunIcon name="lucide:pencil" class="mr-1 size-4" />
          编辑
        </KunButton>
        <KunButton
          color="danger"
          variant="flat"
          size="sm"
          :disabled="manageDisabled"
          @click="emit('delete')"
        >
          <KunIcon name="lucide:trash-2" class="mr-1 size-4" />
          {{ deleteLabel }}
        </KunButton>
      </div>
    </div>

    <p
      v-if="isDeclined && app.review_note"
      class="bg-danger-50 text-danger mt-3 rounded-lg p-3 text-sm"
    >
      未通过审核：{{ app.review_note }}
    </p>
    <p
      v-else-if="reviewStatus === 'pending'"
      class="bg-warning-50 text-warning mt-3 rounded-lg p-3 text-sm"
    >
      已提交，等待平台审核。通过后应用即启用，届时可铸造密钥。
    </p>
    <p
      v-else-if="manageDisabled"
      class="bg-default-100 text-default-500 mt-3 rounded-lg p-3 text-sm"
    >
      {{ DEV_DISABLED_HINT }}：应用的编辑与删除暂由平台代为处理。
    </p>

    <div class="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
      <div>
        <p class="text-default-400 text-xs">分层</p>
        <p class="text-foreground mt-0.5 text-sm">
          {{ DEV_TIER_LABELS[app.tier] ?? app.tier }}
        </p>
      </div>
      <div>
        <p class="text-default-400 text-xs">限流</p>
        <p class="text-foreground mt-0.5 text-sm">
          {{ limitLabel(app.rate_per_min, '次/分') }}
        </p>
      </div>
      <div>
        <p class="text-default-400 text-xs">日配额</p>
        <p class="text-foreground mt-0.5 text-sm">
          {{ limitLabel(app.quota_daily, '次/日') }}
        </p>
      </div>
      <div>
        <p class="text-default-400 text-xs">创建时间</p>
        <p class="text-foreground mt-0.5 text-sm">
          {{ formatDate(app.created_at, { isShowYear: true }) }}
        </p>
      </div>
    </div>
  </KunCard>
</template>
