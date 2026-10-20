<script setup lang="ts">
import type {
  CatalogEditCapabilities,
  CatalogObjectSchema,
  CatalogProposal,
  CatalogRevisionList
} from '#shared/types/catalog'
import { useDiffContext } from '~/utils/review/context'
import {
  afterDecision,
  buildAmendment,
  decisionCounts,
  initialDecisions,
  keysToMerge,
  neighbourIds,
  type FieldDecision,
  type FieldDecisions
} from '~/utils/review/decision'
import { cloneValue } from '#shared/utils/editModel'
import { conflictingFields } from '~/utils/review/history'
import {
  isStale,
  newIdempotencyKey,
  problemMessage
} from '~/utils/review/problem'

interface SnapshotRecord {
  field_values: Record<string, unknown>
}

const api = useEditorApi()
const route = useRoute()
const names = useEntityNames()
const { isReviewer, settled } = useEditorSession()
const queueIds = useState<string[]>('review-queue-ids', () => [])
const { prepare, context } = useDiffContext()

const id = computed(() => String(route.params.id))

const proposal = ref<CatalogProposal | null>(null)
const etag = ref<string | null>(null)
const schema = ref<CatalogObjectSchema | null>(null)
const current = ref<Record<string, unknown>>({})
const capabilities = ref<CatalogEditCapabilities | null>(null)
const conflicts = ref<Record<string, number>>({})
const decisions = ref<FieldDecisions>({})
const note = ref('')
const loadError = ref('')
const acting = ref(false)
const focusIndex = ref(0)
const declineOpen = ref(false)
const helpOpen = ref(false)

const effective = computed<Record<string, unknown>>(
  () => proposal.value?.effective_patch ?? proposal.value?.patch ?? {}
)

const fields = computed(() => {
  const keys = Object.keys(effective.value)
  return (schema.value?.fields ?? []).filter((field) =>
    keys.includes(field.key)
  )
})

const canReviewField = (key: string) =>
  capabilities.value?.fields.find((field) => field.key === key)?.can_review ??
  false

const isOpen = computed(() => proposal.value?.state === 'open')
const counts = computed(() => decisionCounts(decisions.value))
const mergeable = computed(
  () => keysToMerge(effective.value, decisions.value).length > 0
)
const reviewable = computed(() =>
  fields.value.some((field) => canReviewField(field.key))
)
const neighbours = computed(() => neighbourIds(queueIds.value, id.value))

const load = async () => {
  loadError.value = ''
  try {
    const res = await api.raw<CatalogProposal>(
      `/v2/moderation/proposals/${id.value}`,
      {
        query: { include: 'patch,amendments' }
      }
    )
    const record = res._data!
    proposal.value = record
    etag.value = res.headers.get('etag')
    decisions.value = initialDecisions(
      record.effective_patch ?? record.patch ?? {}
    )
    focusIndex.value = 0
    const family = record.target_object
    const [schemaRes, snapshot, caps, revisions] = await Promise.all([
      api<CatalogObjectSchema>(`/v2/catalog/schemas/${family}`),
      api<SnapshotRecord>(
        `/v2/moderation/snapshots/${family}/${record.entity_id}`
      ).catch(() => null),
      api<CatalogEditCapabilities>(`/v2/me/edit-capabilities/${family}`, {
        query: { entity_id: record.entity_id }
      }).catch(() => null),
      api<CatalogRevisionList>('/v2/catalog/revisions', {
        query: { object: family, entity_id: record.entity_id, limit: 100 }
      }).catch(() => null)
    ])
    schema.value = schemaRes
    current.value = snapshot?.field_values ?? {}
    capabilities.value = caps
    const patch = record.effective_patch ?? record.patch ?? {}
    conflicts.value = conflictingFields(
      record.base_revision_seq,
      revisions?.items ?? [],
      Object.keys(patch)
    )
    await Promise.all([
      names.resolve([record]),
      prepare(
        schemaRes.fields,
        Object.keys(patch).flatMap((key) => [
          { key, value: patch[key] },
          { key, value: current.value[key] }
        ])
      )
    ])
  } catch (error) {
    proposal.value = null
    loadError.value = problemMessage(error, '无法加载这条提案')
  }
}

const setDecision = (key: string, decision: FieldDecision) => {
  decisions.value = { ...decisions.value, [key]: decision }
}

const finish = async (message: string) => {
  useKunMessage(message, 'success')
  const { remaining, target } = afterDecision(queueIds.value, id.value)
  queueIds.value = remaining
  await navigateTo(target ? `/review/${target}` : '/review')
}

const handleFailure = async (error: unknown) => {
  if (isStale(error)) {
    useKunMessage('提案刚被其他人修改过，已重新加载，请再确认一遍', 'warn')
    await load()
    return
  }
  useKunMessage(problemMessage(error), 'error')
}

const merge = async () => {
  if (acting.value || !proposal.value || !isOpen.value) {
    return
  }
  if (!mergeable.value) {
    useKunMessage('所有字段都被拒绝了，请直接拒绝这条提案', 'warn')
    return
  }
  acting.value = true
  try {
    let validator = etag.value
    const amendment = buildAmendment(effective.value, decisions.value)
    if (amendment) {
      const amended = await api.raw<CatalogProposal>(
        `/v2/me/proposals/${id.value}/amendments`,
        {
          method: 'POST',
          headers: {
            'If-Match': validator ?? '',
            'Idempotency-Key': newIdempotencyKey()
          },
          body: { ...amendment, ...(note.value ? { note: note.value } : {}) }
        }
      )
      validator = amended.headers.get('etag')
    }
    const decided = await api<{ to_state: string }>(
      `/v2/moderation/proposals/${id.value}/decisions`,
      {
        method: 'POST',
        headers: {
          'If-Match': validator ?? '',
          'Idempotency-Key': newIdempotencyKey()
        },
        body: { decision: 'merge', ...(note.value ? { note: note.value } : {}) }
      }
    )
    if (decided.to_state !== 'merged') {
      useKunMessage(`提案没有合并，当前状态：${decided.to_state}`, 'warn')
      await load()
      return
    }
    await finish(amendment ? '已修正并合并' : '已合并')
  } catch (error) {
    await handleFailure(error)
  } finally {
    acting.value = false
  }
}

const decline = async (reason: string) => {
  if (acting.value || !proposal.value) {
    return
  }
  acting.value = true
  try {
    await api(`/v2/moderation/proposals/${id.value}/decisions`, {
      method: 'POST',
      headers: {
        'If-Match': etag.value ?? '',
        'Idempotency-Key': newIdempotencyKey()
      },
      body: { decision: 'decline', note: reason }
    })
    declineOpen.value = false
    await finish('已拒绝这条提案')
  } catch (error) {
    await handleFailure(error)
  } finally {
    acting.value = false
  }
}

const focusedField = computed(() => fields.value[focusIndex.value])

const verdictOnFocused = (verdict: 'accept' | 'reject' | 'amend') => {
  const field = focusedField.value
  if (!field || !canReviewField(field.key) || !isOpen.value) {
    return
  }
  const proposed = effective.value[field.key]
  setDecision(
    field.key,
    verdict === 'amend'
      ? { verdict, value: cloneValue(proposed ?? null) }
      : { verdict }
  )
}

const isTyping = (event: KeyboardEvent) => {
  const target = event.target as HTMLElement | null
  return (
    !!target &&
    (target.isContentEditable ||
      ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
  )
}

const onKey = (event: KeyboardEvent) => {
  if (
    event.metaKey ||
    event.ctrlKey ||
    event.altKey ||
    isTyping(event) ||
    declineOpen.value
  ) {
    return
  }
  const actions: Record<string, () => void> = {
    j: () =>
      (focusIndex.value = Math.min(
        focusIndex.value + 1,
        fields.value.length - 1
      )),
    k: () => (focusIndex.value = Math.max(focusIndex.value - 1, 0)),
    a: () => verdictOnFocused('accept'),
    r: () => verdictOnFocused('reject'),
    e: () => verdictOnFocused('amend'),
    m: () => reviewable.value && merge(),
    d: () => isOpen.value && (declineOpen.value = true),
    ']': () =>
      neighbours.value.next && navigateTo(`/review/${neighbours.value.next}`),
    '[': () =>
      neighbours.value.previous &&
      navigateTo(`/review/${neighbours.value.previous}`),
    '?': () => (helpOpen.value = !helpOpen.value)
  }
  const action = actions[event.key]
  if (action) {
    event.preventDefault()
    action()
  }
}

onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

watch(id, load)

useHead(() => ({ title: `审核提案 #${id.value} · 未萌编辑台` }))

watch(
  isReviewer,
  (reviewer) => {
    if (reviewer && !proposal.value) {
      load()
    }
  },
  { immediate: true }
)
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-5">
    <div class="flex flex-wrap items-center gap-2">
      <NuxtLink
        to="/review"
        class="text-default-500 hover:text-foreground inline-flex items-center gap-1 text-sm"
      >
        <KunIcon name="lucide:chevron-left" class="size-4" />
        待审队列
      </NuxtLink>
      <div class="ml-auto flex items-center gap-1">
        <KunButton
          size="sm"
          variant="light"
          :disabled="!neighbours.previous"
          @click="navigateTo(`/review/${neighbours.previous}`)"
        >
          上一条 <kbd class="text-default-400 ml-1 text-[10px]">[</kbd>
        </KunButton>
        <KunButton
          size="sm"
          variant="light"
          :disabled="!neighbours.next"
          @click="navigateTo(`/review/${neighbours.next}`)"
        >
          下一条 <kbd class="text-default-400 ml-1 text-[10px]">]</kbd>
        </KunButton>
        <KunButton
          size="sm"
          variant="light"
          is-icon-only
          aria-label="快捷键"
          @click="helpOpen = !helpOpen"
        >
          <KunIcon name="lucide:keyboard" class="size-4" />
        </KunButton>
      </div>
    </div>

    <ReviewDetailShortcuts v-if="helpOpen" />

    <KunInfo
      v-if="settled && !isReviewer"
      color="warning"
      variant="flat"
      title="需要审核权限"
      description="只有审核者可以处理待审提案。"
    />
    <KunInfo
      v-else-if="loadError"
      color="danger"
      variant="flat"
      title="无法加载这条提案"
      :description="loadError"
    />

    <template v-else-if="proposal">
      <ReviewDetailHeader
        :proposal="proposal"
        :name="names.nameOf(proposal)"
        :conflict-count="Object.keys(conflicts).length"
      />

      <div class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <div class="space-y-3">
          <KunInfo
            v-if="!isOpen"
            color="info"
            variant="flat"
            title="这条提案已经处理过"
            description="它不在待审状态，下面只展示提议的内容。"
          />
          <KunInfo
            v-else-if="!reviewable"
            color="warning"
            variant="flat"
            title="你无权审核这条提案涉及的字段"
            description="按当前编辑策略，这些字段需要其他审核权限。"
          />
          <ReviewDetailFieldCard
            v-for="(field, index) in fields"
            :key="field.key"
            :field="field"
            :current="current[field.key]"
            :proposed="effective[field.key]"
            :decision="decisions[field.key] ?? { verdict: 'accept' }"
            :context="context"
            :conflict-seq="conflicts[field.key]"
            :can-review="canReviewField(field.key)"
            :focused="index === focusIndex"
            :disabled="!isOpen || acting"
            @focus="focusIndex = index"
            @update:decision="setDecision(field.key, $event)"
          />
          <KunNull
            v-if="!fields.length"
            description="这条提案没有可识别的字段改动"
          />
        </div>

        <aside class="space-y-3 lg:sticky lg:top-16 lg:self-start">
          <KunCard
            v-if="isOpen && reviewable"
            bordered
            padding="md"
            content-class="space-y-3"
          >
            <p class="text-default-500 text-xs">
              采纳 {{ counts.accept }} · 修正 {{ counts.amend }} · 拒绝
              {{ counts.reject }}
            </p>
            <KunTextarea
              v-model="note"
              placeholder="审核备注（可选；拒绝时必填）"
              aria-label="审核备注"
              max-height="160px"
            />
            <KunButton
              full-width
              color="primary"
              :loading="acting"
              :disabled="!mergeable"
              @click="merge"
            >
              {{ counts.amend || counts.reject ? '修正并合并' : '合并' }}
              <kbd class="ml-1 text-[10px] opacity-70">m</kbd>
            </KunButton>
            <KunButton
              full-width
              variant="bordered"
              color="danger"
              :disabled="acting"
              @click="declineOpen = true"
            >
              拒绝整条提案 <kbd class="ml-1 text-[10px] opacity-70">d</kbd>
            </KunButton>
          </KunCard>
          <ReviewDetailSidebar :proposal="proposal" />
        </aside>
      </div>

      <ReviewDetailDeclineModal
        v-model="declineOpen"
        :initial-reason="note"
        :loading="acting"
        @confirm="decline"
      />
    </template>

    <div v-else class="space-y-5" aria-busy="true" aria-label="正在加载提案">
      <div class="space-y-2">
        <KunSkeleton variant="text" class-name="text-xl" width="18rem" />
        <KunSkeleton variant="text" class-name="text-sm" width="12rem" />
      </div>
      <div class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <div class="space-y-3">
          <div
            v-for="index in 3"
            :key="index"
            class="border-default-200 bg-content1 space-y-3 rounded-xl border p-4"
          >
            <KunSkeleton
              variant="text"
              class-name="font-semibold"
              width="7rem"
            />
            <div class="grid gap-3 sm:grid-cols-2">
              <KunSkeleton height="4.5rem" rounded="lg" />
              <KunSkeleton height="4.5rem" rounded="lg" />
            </div>
          </div>
        </div>
        <div
          class="border-default-200 bg-content1 space-y-3 self-start rounded-xl border p-4"
        >
          <KunSkeleton variant="text" class-name="text-xs" width="60%" />
          <KunSkeleton height="5rem" rounded="lg" />
          <KunSkeleton height="2.5rem" rounded="lg" />
        </div>
      </div>
    </div>
  </div>
</template>
