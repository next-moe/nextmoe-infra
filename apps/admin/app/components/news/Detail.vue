<script setup lang="ts">
import {
  NEWS_ACTIONS,
  NEWS_STATUS_LABELS,
  NEWS_STATUS_COLORS,
  NEWS_SOURCE_LABELS,
  NEWS_LANE_LABELS,
  NEWS_DEGRADED_LABELS,
  TIER0_LABELS
} from '~/constants/news'
import type { NewsAdminItemDetail } from '~~/shared/types/news'

const props = defineProps<{ detail: NewsAdminItemDetail }>()
const emit = defineEmits<{ decided: [] }>()

const api = useApi('catalog')

const reason = ref('')
const submitting = ref('')

const available = computed(() =>
  NEWS_ACTIONS.filter((a) => a.from.includes(props.detail.status))
)

const formatTime = (raw: string) =>
  new Date(raw).toLocaleString('zh-CN', { hour12: false })

const decide = async (action: string, needsReason?: boolean) => {
  const text = reason.value.trim()
  if (needsReason && !text) {
    useKunMessage('拒绝和撤回必须写明理由，它会留在审计记录里', 'warn')
    return
  }
  submitting.value = action
  const res = await api.post(`/admin/news/items/${props.detail.id}/decision`, {
    action,
    reason: text
  })
  submitting.value = ''
  if (res.code === 0) {
    useKunMessage('已记录', 'success')
    emit('decided')
  } else {
    useKunMessage(res.message || '操作失败', 'error')
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-2">
      <KunChip
        :color="NEWS_STATUS_COLORS[detail.status] ?? 'default'"
        variant="flat"
        size="xs"
      >
        {{ NEWS_STATUS_LABELS[detail.status] ?? detail.status }}
      </KunChip>
      <KunChip color="info" variant="flat" size="xs">
        {{ NEWS_LANE_LABELS[detail.lane] ?? detail.lane }}
      </KunChip>
      <span class="text-default-500 text-sm">
        {{ NEWS_SOURCE_LABELS[detail.source_key] ?? detail.source_key }}
      </span>
    </div>

    <h2 class="text-foreground text-xl font-bold">{{ detail.title }}</h2>

    <img
      v-if="detail.banner_url"
      :src="detail.banner_url"
      :alt="detail.title"
      class="max-h-64 w-full rounded-lg object-cover"
    />

    <p class="text-default-500 text-sm whitespace-pre-wrap">
      {{ detail.preview }}
    </p>

    <KunButton
      size="sm"
      variant="flat"
      @click="
        navigateTo(detail.source_url, {
          external: true,
          open: { target: '_blank' }
        })
      "
    >
      <KunIcon name="lucide:external-link" class="mr-1 size-4" />
      在对方站点查看正文
    </KunButton>

    <KunDivider />

    <div class="space-y-2">
      <h3 class="text-foreground font-medium">机器意见</h3>
      <NewsVerdictBadges :item="detail" />
      <div
        v-for="v in detail.verdicts"
        :key="v.id"
        class="text-default-400 flex flex-wrap items-center gap-2 text-xs"
      >
        <span>{{ formatTime(v.created_at) }}</span>
        <KunChip v-if="!v.current" color="default" variant="flat" size="xs">
          判的是旧正文
        </KunChip>
        <span>
          词表 {{ TIER0_LABELS[v.tier0_decision] ?? v.tier0_decision }}
          <template v-if="v.tier0_matched?.length">
            （{{ v.tier0_matched.join('、') }}）
          </template>
        </span>
        <span v-if="v.degraded" class="text-danger">
          降级：{{
            NEWS_DEGRADED_LABELS[v.degraded_reason ?? ''] ?? v.degraded_reason
          }}
        </span>
        <span v-else-if="v.ai_score != null">
          AI {{ v.ai_score.toFixed(4) }}
          <template v-if="v.ai_channel">· {{ v.ai_channel }}</template>
        </span>
      </div>
      <p v-if="!detail.verdicts.length" class="text-default-400 text-xs">
        还没有任何评分记录
      </p>
    </div>

    <div v-if="detail.decisions.length" class="space-y-2">
      <h3 class="text-foreground font-medium">人工裁决</h3>
      <div
        v-for="d in detail.decisions"
        :key="d.id"
        class="text-default-400 text-xs"
      >
        {{ formatTime(d.created_at) }} · uid {{ d.actor_uid }} ·
        {{ NEWS_STATUS_LABELS[d.from_status] }} →
        {{ NEWS_STATUS_LABELS[d.to_status] }}
        <template v-if="d.reason">· {{ d.reason }}</template>
      </div>
    </div>

    <KunDivider />

    <KunTextarea
      v-model="reason"
      placeholder="裁决理由（拒绝与撤回必填，会留在审计记录里）"
      :rows="2"
    />
    <div class="flex flex-wrap justify-end gap-2">
      <KunButton
        v-for="a in available"
        :key="a.action"
        :color="a.color"
        size="sm"
        :is-loading="submitting === a.action"
        :disabled="submitting !== '' && submitting !== a.action"
        @click="decide(a.action, a.needsReason)"
      >
        <KunIcon :name="a.icon" class="mr-1 size-4" />
        {{ a.label }}
      </KunButton>
    </div>
  </div>
</template>
