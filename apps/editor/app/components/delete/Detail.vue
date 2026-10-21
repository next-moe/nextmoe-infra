<script setup lang="ts">
import type {
  CatalogDeletionPreview,
  CatalogDeletionRequest
} from '#shared/types/catalog'
import {
  DELETION_REASON_LABELS,
  DELETION_STATE_COLORS,
  DELETION_STATE_LABELS
} from '~/constants/deletion'
import { mergeTiming } from '~/utils/creation'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'

type Decision = 'approve' | 'reject' | 'execute' | 'restore'

const props = withDefaults(
  defineProps<{
    request: CatalogDeletionRequest
    mode: 'review' | 'mine'
    now: number
    narrow?: boolean
  }>(),
  { narrow: false }
)
const emit = defineEmits<{ changed: [request: CatalogDeletionRequest] }>()

const api = useEditorApi()
const preview = ref<CatalogDeletionPreview | null>(null)
const previewLoading = ref(false)
const busy = ref<Decision | 'withdraw' | null>(null)
const noteOpen = ref<'reject' | 'restore' | null>(null)
const noteText = ref('')

const timing = computed(() =>
  mergeTiming(props.request.state, props.request.execute_after, props.now)
)
const closed = computed(() =>
  ['rejected', 'withdrawn', 'restored'].includes(props.request.state)
)
const reviewActions = computed(
  () =>
    props.mode === 'review' &&
    ['open', 'approved', 'executed'].includes(props.request.state)
)
const name = computed(
  () => props.request.target_name || `作品 #${props.request.target_id}`
)

const loadPreview = async () => {
  previewLoading.value = true
  preview.value = null
  try {
    preview.value = await api<CatalogDeletionPreview>(
      '/v2/me/deletion-preview',
      { query: { object: 'work', target_id: props.request.target_id } }
    )
  } catch {
    preview.value = null
  } finally {
    previewLoading.value = false
  }
}
watch(() => props.request.id, loadPreview, { immediate: true })

const SUCCESS: Record<Decision, string> = {
  approve: '已批准，冷静期结束后执行',
  reject: '已驳回',
  execute: '已删除',
  restore: '已恢复，作品和用户数据都回来了'
}

const decide = async (decision: Decision, reason = '') => {
  busy.value = decision
  try {
    const updated = await api<CatalogDeletionRequest>(
      `/v2/moderation/deletion-requests/${props.request.id}/decisions`,
      {
        method: 'POST',
        headers: { 'Idempotency-Key': newIdempotencyKey() },
        body: { decision, ...(reason ? { reason } : {}) }
      }
    )
    useKunMessage(
      updated.state === 'rejected' && decision === 'execute'
        ? '作品已被合并或删除，这条请求随之关闭'
        : SUCCESS[decision],
      'success'
    )
    noteOpen.value = null
    emit('changed', updated)
    loadPreview()
  } catch (error) {
    useKunMessage(problemMessage(error, '操作失败'), 'error')
  } finally {
    busy.value = null
  }
}

const withdraw = async () => {
  busy.value = 'withdraw'
  try {
    const updated = await api<CatalogDeletionRequest>(
      `/v2/me/deletion-requests/${props.request.id}`,
      {
        method: 'PATCH',
        headers: { 'Idempotency-Key': newIdempotencyKey() },
        body: { state: 'withdrawn' }
      }
    )
    useKunMessage('已撤回', 'success')
    emit('changed', updated)
  } catch (error) {
    useKunMessage(problemMessage(error, '撤回失败'), 'error')
  } finally {
    busy.value = null
  }
}

const askNote = (kind: 'reject' | 'restore') => {
  noteText.value = ''
  noteOpen.value = kind
}

const discussionParticipants = computed(() => {
  const r = props.request
  return [
    { uid: r.proposed_by_uid, user: r.proposed_by, role: '发起人' },
    ...(r.decided_by_uid
      ? [{ uid: r.decided_by_uid, user: r.decided_by, role: '审核人' }]
      : []),
    ...(r.restored_by_uid
      ? [{ uid: r.restored_by_uid, user: r.restored_by, role: '恢复人' }]
      : [])
  ]
})
</script>

<template>
  <article class="space-y-5">
    <header class="space-y-2">
      <div class="flex flex-wrap items-center gap-2">
        <KunChip
          size="sm"
          variant="flat"
          :color="DELETION_STATE_COLORS[props.request.state] ?? 'default'"
        >
          {{
            DELETION_STATE_LABELS[props.request.state] ?? props.request.state
          }}
        </KunChip>
        <span class="text-default-500 text-xs">
          删除请求 #{{ props.request.id }}
        </span>
      </div>
      <h2 class="text-lg leading-snug font-semibold">
        <NuxtLink
          :to="`/works/${props.request.target_id}`"
          target="_blank"
          class="hover:text-primary"
        >
          {{ name }}
        </NuxtLink>
      </h2>
      <p class="text-default-500 flex items-center gap-1 text-xs">
        作品 ID <KunCopy :text="props.request.target_id" />
      </p>
    </header>

    <DeleteTimeline
      :request="props.request"
      :now="props.now"
      :orientation="props.narrow ? 'vertical' : 'horizontal'"
    />

    <section class="space-y-2">
      <h3 class="text-default-700 text-sm font-medium">
        {{
          DELETION_REASON_LABELS[props.request.reason] ?? props.request.reason
        }}
      </h3>
      <p
        class="bg-default-50 text-default-700 rounded-lg p-3 text-sm whitespace-pre-line"
      >
        {{ props.request.note }}
      </p>
      <KunInfo
        v-if="props.request.decision_note"
        :color="props.request.state === 'rejected' ? 'danger' : 'default'"
        variant="flat"
        :title="props.request.decided_by ? '审核意见' : '系统说明'"
        :description="props.request.decision_note"
      />
      <KunInfo
        v-if="props.request.restore_note"
        color="success"
        variant="flat"
        title="恢复说明"
        :description="props.request.restore_note"
      />
    </section>

    <section v-if="!closed" class="space-y-2">
      <h3 class="text-default-700 text-sm font-medium">
        {{
          props.request.state === 'executed'
            ? '删除带来的影响'
            : '删除会影响什么'
        }}
      </h3>
      <DeleteImpact :preview="preview" :loading="previewLoading" />
    </section>

    <footer
      v-if="reviewActions"
      class="border-default-200 flex flex-wrap justify-end gap-2 border-t pt-4"
    >
      <template v-if="props.request.state === 'open'">
        <KunButton variant="light" color="danger" @click="askNote('reject')">
          驳回
        </KunButton>
        <KunButton
          color="primary"
          :loading="busy === 'approve'"
          @click="decide('approve')"
        >
          批准并开始冷静期
        </KunButton>
      </template>
      <template v-else-if="props.request.state === 'approved'">
        <KunButton variant="light" color="danger" @click="askNote('reject')">
          驳回
        </KunButton>
        <KunButton
          color="danger"
          :disabled="!timing.canExecute"
          :loading="busy === 'execute'"
          @click="decide('execute')"
        >
          {{
            timing.canExecute
              ? '立即删除'
              : `冷静期中（还剩 ${formatWait(timing.waitMs)}）`
          }}
        </KunButton>
      </template>
      <KunButton
        v-else-if="props.request.state === 'executed'"
        color="success"
        variant="flat"
        @click="askNote('restore')"
      >
        <KunIcon name="lucide:archive-restore" class="size-4" />
        恢复作品
      </KunButton>
    </footer>
    <footer
      v-else-if="props.mode === 'mine' && props.request.state === 'open'"
      class="border-default-200 flex justify-end border-t pt-4"
    >
      <KunButton
        variant="light"
        :loading="busy === 'withdraw'"
        @click="withdraw"
      >
        撤回请求
      </KunButton>
    </footer>

    <DiscussionThread
      anchor-kind="deletion_request"
      :anchor-id="props.request.id"
      :participants="discussionParticipants"
    />

    <KunModal
      :model-value="noteOpen !== null"
      :title="noteOpen === 'reject' ? '驳回删除请求' : '恢复作品'"
      :description="
        noteOpen === 'reject'
          ? '驳回理由会展示给提交者。'
          : '作品会以原来的 id 回来，评分、收藏等用户数据一并恢复。'
      "
      size="md"
      @update:model-value="(open) => !open && (noteOpen = null)"
    >
      <div class="space-y-4">
        <KunTextarea
          v-model="noteText"
          :placeholder="
            noteOpen === 'reject' ? '驳回理由（必填）' : '恢复说明（可选）'
          "
          :aria-label="noteOpen === 'reject' ? '驳回理由' : '恢复说明'"
          max-height="160px"
        />
        <div class="flex justify-end gap-2">
          <KunButton variant="light" @click="noteOpen = null">取消</KunButton>
          <KunButton
            v-if="noteOpen === 'reject'"
            color="danger"
            :disabled="!noteText.trim()"
            :loading="busy === 'reject'"
            @click="decide('reject', noteText.trim())"
          >
            驳回
          </KunButton>
          <KunButton
            v-else
            color="success"
            :loading="busy === 'restore'"
            @click="decide('restore', noteText.trim())"
          >
            恢复
          </KunButton>
        </div>
      </div>
    </KunModal>
  </article>
</template>
