<script setup lang="ts">
import type { KunSelectOption } from '@kungal/ui-vue'
import type {
  CatalogMergeRequest,
  CatalogMergeRequestList
} from '#shared/types/catalog'
import {
  newIdempotencyKey,
  problemMessage,
  problemStatus
} from '~/utils/review/problem'

useHead({ title: '合并请求审核 · 未萌编辑台' })

type Decision = 'approve' | 'reject' | 'execute'

const api = useEditorApi()

const state = ref('open')
const items = ref<CatalogMergeRequest[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(false)
const forbidden = ref(false)
const now = ref(Date.now())
const deciding = ref<string | null>(null)
const rejectTarget = ref<CatalogMergeRequest | null>(null)
const rejectReason = ref('')
const rejectOpen = ref(false)

const stateOptions: KunSelectOption<string>[] = [
  { value: 'open', label: '待审核' },
  { value: 'approved', label: '已批准（冷却中 / 待执行）' },
  { value: 'executed', label: '已合并' },
  { value: 'rejected', label: '已驳回' }
]

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CatalogMergeRequestList>(
      '/v2/moderation/merge-requests',
      {
        query: { limit: 30, state: state.value, ...(cursor ? { cursor } : {}) }
      }
    )
    forbidden.value = false
    items.value = cursor ? [...items.value, ...list.items] : list.items
    nextCursor.value = list.next_cursor ?? null
  } catch (error) {
    forbidden.value = problemStatus(error) === 403
    if (!cursor) {
      items.value = []
    }
    nextCursor.value = null
  } finally {
    loading.value = false
    now.value = Date.now()
  }
}

const decide = async (
  request: CatalogMergeRequest,
  decision: Decision,
  reason?: string
) => {
  deciding.value = `${request.id}:${decision}`
  try {
    await api(`/v2/moderation/merge-requests/${request.id}/decisions`, {
      method: 'POST',
      headers: { 'Idempotency-Key': newIdempotencyKey() },
      body: { decision, ...(reason ? { reason } : {}) }
    })
    useKunMessage(
      decision === 'approve'
        ? '已批准，冷却期结束后执行'
        : decision === 'execute'
          ? '合并已执行'
          : '已驳回',
      'success'
    )
    await fetchPage(null)
  } catch (error) {
    useKunMessage(problemMessage(error, '操作失败'), 'error')
  } finally {
    deciding.value = null
  }
}

const askReject = (request: CatalogMergeRequest) => {
  rejectTarget.value = request
  rejectReason.value = ''
  rejectOpen.value = true
}

const confirmReject = async () => {
  if (!rejectTarget.value || !rejectReason.value.trim()) {
    return
  }
  await decide(rejectTarget.value, 'reject', rejectReason.value.trim())
  rejectOpen.value = false
}

let ticker: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  ticker = setInterval(() => {
    now.value = Date.now()
  }, 30_000)
})
onBeforeUnmount(() => clearInterval(ticker))

watch(state, () => fetchPage(null))
await fetchPage(null)
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-5">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"
    >
      <KunHeader
        name="合并请求审核"
        description="批准后进入冷却期，期满后可以手动执行，系统也会每小时自动执行到期的请求。"
      />
      <div class="flex items-center gap-2">
        <KunButton href="/review" variant="light" size="sm">
          <KunIcon name="lucide:inbox" class="size-4" />
          编辑提案
        </KunButton>
        <KunSelect
          v-model="state"
          :options="stateOptions"
          aria-label="状态"
          class="w-56"
        />
      </div>
    </div>

    <KunInfo
      v-if="forbidden"
      color="warning"
      variant="flat"
      title="需要合并审核权限"
      description="合并请求只能由版主、管理员审核。"
    />

    <KunCard v-else bordered padding="md">
      <ul v-if="items.length" class="divide-default-200 divide-y">
        <MergeRow
          v-for="request in items"
          :key="request.id"
          v-slot="{ timing }"
          :request="request"
          :now="now"
        >
          <div class="flex justify-end gap-2">
            <template v-if="request.state === 'open'">
              <KunButton
                size="xs"
                variant="light"
                color="danger"
                @click="askReject(request)"
              >
                驳回
              </KunButton>
              <KunButton
                size="xs"
                color="primary"
                :loading="deciding === `${request.id}:approve`"
                @click="decide(request, 'approve')"
              >
                批准
              </KunButton>
            </template>
            <template v-else-if="request.state === 'approved'">
              <KunButton
                size="xs"
                variant="light"
                color="danger"
                @click="askReject(request)"
              >
                驳回
              </KunButton>
              <KunButton
                size="xs"
                color="warning"
                :disabled="!timing.canExecute"
                :loading="deciding === `${request.id}:execute`"
                @click="decide(request, 'execute')"
              >
                {{
                  timing.canExecute
                    ? '立即执行'
                    : `冷却中（${formatWait(timing.waitMs)}）`
                }}
              </KunButton>
            </template>
          </div>
        </MergeRow>
      </ul>
      <KunNull v-else-if="!loading" description="没有这个状态的合并请求" />
      <div class="flex justify-center pt-3">
        <KunLoading v-if="loading" spinner />
        <KunButton
          v-else-if="nextCursor"
          variant="bordered"
          @click="fetchPage(nextCursor)"
        >
          加载更多
        </KunButton>
      </div>
    </KunCard>

    <KunModal
      v-model="rejectOpen"
      title="驳回合并请求"
      description="驳回理由会展示给提交者。"
      size="md"
    >
      <div class="space-y-4">
        <KunTextarea
          v-model="rejectReason"
          placeholder="驳回理由（必填）"
          aria-label="驳回理由"
          max-height="8rem"
        />
        <div class="flex justify-end gap-2">
          <KunButton variant="light" @click="rejectOpen = false"
            >取消</KunButton
          >
          <KunButton
            color="danger"
            :disabled="!rejectReason.trim()"
            :loading="!!deciding"
            @click="confirmReject"
          >
            驳回
          </KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
