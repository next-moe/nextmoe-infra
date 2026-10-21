<script setup lang="ts">
import type {
  CatalogDeletionRequest,
  CatalogDeletionRequestList
} from '#shared/types/catalog'
import { DELETION_REASON_LABELS } from '~/constants/deletion'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'

const props = defineProps<{
  workId: string
  reason: string
  deletedAt: string
}>()
const emit = defineEmits<{ restored: [] }>()

const api = useEditorApi()
const { isReviewer } = useEditorSession()

const request = ref<CatalogDeletionRequest | null>(null)
const restoreOpen = ref(false)
const restoreNote = ref('')
const restoring = ref(false)

const loadRequest = async () => {
  if (!isReviewer.value) {
    return
  }
  try {
    const list = await api<CatalogDeletionRequestList>(
      '/v2/moderation/deletion-requests',
      { query: { state: 'executed', target_id: props.workId, limit: 1 } }
    )
    request.value = list.items[0] ?? null
  } catch {
    request.value = null
  }
}
watch(isReviewer, loadRequest, { immediate: true })

const restore = async () => {
  if (!request.value) {
    return
  }
  restoring.value = true
  try {
    await api(
      `/v2/moderation/deletion-requests/${request.value.id}/decisions`,
      {
        method: 'POST',
        headers: { 'Idempotency-Key': newIdempotencyKey() },
        body: {
          decision: 'restore',
          ...(restoreNote.value.trim()
            ? { reason: restoreNote.value.trim() }
            : {})
        }
      }
    )
    useKunMessage('已恢复，作品和用户数据都回来了', 'success')
    restoreOpen.value = false
    emit('restored')
  } catch (error) {
    useKunMessage(problemMessage(error, '恢复失败'), 'error')
  } finally {
    restoring.value = false
  }
}
</script>

<template>
  <KunCard bordered padding="lg">
    <div class="flex flex-col gap-4 sm:flex-row sm:items-start">
      <div
        class="bg-danger-50 text-danger-600 flex size-12 shrink-0 items-center justify-center rounded-full"
      >
        <KunIcon name="lucide:trash-2" class="size-6" />
      </div>
      <div class="min-w-0 flex-1 space-y-2">
        <h2 class="text-lg font-semibold">这部作品已被删除</h2>
        <p class="text-default-600 text-sm">
          原因：{{ DELETION_REASON_LABELS[props.reason] ?? props.reason }}
          <template v-if="props.deletedAt">
            · {{ formatDateTime(props.deletedAt) }}
          </template>
        </p>
        <p class="text-default-500 text-sm leading-relaxed">
          它的资料源对应保留着，导入不会把它重新建出来；评分、收藏等用户数据保留但不显示。审核者可以用原来的
          ID 恢复它，数据会一并回来。
        </p>
        <div v-if="request" class="flex flex-wrap gap-2 pt-1">
          <KunButton color="success" variant="flat" @click="restoreOpen = true">
            <KunIcon name="lucide:archive-restore" class="size-4" />
            恢复作品
          </KunButton>
          <KunButton
            :href="`/review/deletions?state=executed&id=${request.id}`"
            variant="light"
          >
            查看删除请求 #{{ request.id }}
          </KunButton>
        </div>
      </div>
    </div>

    <KunModal
      v-model="restoreOpen"
      title="恢复作品"
      description="作品会以原来的 id 回来，评分、收藏等用户数据一并恢复。"
      size="md"
    >
      <div class="space-y-4">
        <KunTextarea
          v-model="restoreNote"
          placeholder="恢复说明（可选）"
          aria-label="恢复说明"
          max-height="160px"
        />
        <div class="flex justify-end gap-2">
          <KunButton variant="light" @click="restoreOpen = false"
            >取消</KunButton
          >
          <KunButton color="success" :loading="restoring" @click="restore">
            恢复
          </KunButton>
        </div>
      </div>
    </KunModal>
  </KunCard>
</template>
