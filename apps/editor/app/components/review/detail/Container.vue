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
  unresolvedConflicts,
  type FieldDecision,
  type FieldDecisions
} from '~/utils/review/decision'
import { cloneValue } from '#shared/utils/editModel'
import { threeWayMerge, type ThreeWayMerge } from '#shared/utils/threeWay'
import { conflictingFields, type FieldDiffLike } from '~/utils/review/history'
import { basesFromDiff, conflictBaseQuery } from '~/utils/review/conflictBase'
import {
  isStale,
  mergeConflictKeys,
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

const discussionParticipants = computed(() => {
  const p = proposal.value
  if (!p) {
    return []
  }
  return [
    { uid: p.proposer_uid, user: p.proposer, role: '提案人' },
    ...(p.decided_by_uid
      ? [{ uid: p.decided_by_uid, user: p.decided_by, role: '处理人' }]
      : []),
    ...(p.amendments ?? []).map((amendment) => ({
      uid: amendment.amender_uid,
      user: amendment.amender,
      role: '修正者'
    }))
  ]
})

const jumpToDiscussion = () =>
  document
    .getElementById('discussion')
    ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
const etag = ref<string | null>(null)
const schema = ref<CatalogObjectSchema | null>(null)
const current = ref<Record<string, unknown>>({})
const capabilities = ref<CatalogEditCapabilities | null>(null)
const conflicts = ref<Record<string, number>>({})
const bases = ref<Record<string, unknown>>({})
const basesKnown = ref(false)
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

const conflictKeys = computed(() =>
  Object.keys(conflicts.value).filter((key) => key in effective.value)
)

const fields = computed(() => {
  const keys = Object.keys(effective.value)
  const present = (schema.value?.fields ?? []).filter((field) =>
    keys.includes(field.key)
  )
  return [
    ...present.filter((field) => conflictKeys.value.includes(field.key)),
    ...present.filter((field) => !conflictKeys.value.includes(field.key))
  ]
})

const merges = computed<Record<string, ThreeWayMerge | null>>(() =>
  Object.fromEntries(
    fields.value
      .filter((field) => conflictKeys.value.includes(field.key))
      .map((field) => [
        field.key,
        basesKnown.value
          ? threeWayMerge(
              field,
              bases.value[field.key],
              current.value[field.key],
              effective.value[field.key]
            )
          : null
      ])
  )
)

const canReviewField = (key: string) =>
  capabilities.value?.fields.find((field) => field.key === key)?.can_review ??
  false

const isOpen = computed(() => proposal.value?.state === 'open')
const counts = computed(() => decisionCounts(decisions.value))
const unresolved = computed(() =>
  unresolvedConflicts(conflictKeys.value, decisions.value)
)
const autoMergeable = computed(() =>
  unresolved.value.filter((key) => merges.value[key]?.clean)
)
const mergeable = computed(
  () =>
    keysToMerge(effective.value, decisions.value).length > 0 &&
    unresolved.value.length === 0
)
const amending = computed(
  () =>
    counts.value.amend > 0 ||
    counts.value.reject > 0 ||
    conflictKeys.value.length > 0
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
    const found = conflictingFields(
      record.base_revision_seq,
      revisions?.items ?? [],
      Object.keys(patch)
    )
    for (const key of record.conflict_fields ?? []) {
      found[key] ??= 0
    }
    conflicts.value = record.state === 'open' ? found : {}
    const keys = Object.keys(conflicts.value)
    decisions.value = initialDecisions(patch, keys)
    await loadBases(record.base_revision_seq, revisions?.items ?? [], keys)
    await Promise.all([
      names.resolve([record]),
      prepare(
        schemaRes.fields,
        Object.keys(patch).flatMap((key) => [
          { key, value: patch[key] },
          { key, value: current.value[key] },
          { key, value: bases.value[key] }
        ])
      )
    ])
  } catch (error) {
    proposal.value = null
    loadError.value = problemMessage(error, '无法加载这条提案')
  }
}

const loadBases = async (
  baseSeq: number,
  revisions: readonly { id: string; seq: number; changed_fields: string[] }[],
  keys: string[]
) => {
  bases.value = {}
  basesKnown.value = false
  const query = keys.length ? conflictBaseQuery(baseSeq, revisions) : null
  if (!query) {
    return
  }
  const revision = await api<{ diff?: FieldDiffLike[] }>(
    `/v2/catalog/revisions/${query.revisionId}`,
    { query: { include: 'diff', diff_base: query.diffBase } }
  ).catch(() => null)
  if (revision?.diff) {
    bases.value = basesFromDiff(revision.diff, keys, current.value)
    basesKnown.value = true
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
  const fresh = mergeConflictKeys(error)
  if (fresh.length) {
    useKunMessage(
      `合并时发现新的冲突：${fresh.map((key) => fieldLabel(key)).join('、')} 刚被改过，已重新加载并标出`,
      'warn'
    )
    await load()
    return
  }
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
  if (unresolved.value.length) {
    useKunMessage(`还有 ${unresolved.value.length} 处冲突没有处理`, 'warn')
    return
  }
  if (!mergeable.value) {
    useKunMessage('所有字段都被拒绝了，请直接拒绝这条提案', 'warn')
    return
  }
  acting.value = true
  try {
    let validator = etag.value
    const amendment = buildAmendment(
      effective.value,
      decisions.value,
      conflictKeys.value
    )
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

const resolveConflict = (
  key: string,
  verdict: 'auto' | 'override' | 'keep' | 'amend'
) => {
  const merge = merges.value[key]
  if (verdict === 'auto') {
    if (merge?.clean) {
      setDecision(key, { verdict, value: cloneValue(merge.value) })
    }
  } else if (verdict === 'amend') {
    const existing = decisions.value[key]
    setDecision(key, {
      verdict,
      value:
        existing?.verdict === 'amend'
          ? existing.value
          : cloneValue(
              merge?.value ?? current.value[key] ?? effective.value[key]
            )
    })
  } else {
    setDecision(key, { verdict })
  }
}

const autoResolveAll = () => {
  for (const key of autoMergeable.value) {
    resolveConflict(key, 'auto')
  }
}

const verdictOnFocused = (
  verdict: 'accept' | 'reject' | 'amend' | 'override' | 'keep'
) => {
  const field = focusedField.value
  if (!field || !canReviewField(field.key) || !isOpen.value) {
    return
  }
  if (conflictKeys.value.includes(field.key)) {
    const mapped =
      verdict === 'accept' ? 'auto' : verdict === 'reject' ? 'keep' : verdict
    resolveConflict(field.key, mapped)
    return
  }
  if (verdict === 'override' || verdict === 'keep') {
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
    o: () => verdictOnFocused('override'),
    c: () => verdictOnFocused('keep'),
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
          <template v-for="(field, index) in fields" :key="field.key">
            <ReviewDetailConflictCard
              v-if="conflictKeys.includes(field.key)"
              :field="field"
              :base="bases[field.key]"
              :base-known="basesKnown"
              :current="current[field.key]"
              :proposed="effective[field.key]"
              :merge="merges[field.key] ?? null"
              :decision="decisions[field.key] ?? { verdict: 'pending' }"
              :context="context"
              :conflict-seq="conflicts[field.key] || undefined"
              :can-review="canReviewField(field.key)"
              :focused="index === focusIndex"
              :disabled="!isOpen || acting"
              @focus="focusIndex = index"
              @update:decision="setDecision(field.key, $event)"
            />
            <ReviewDetailFieldCard
              v-else
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
          </template>
          <KunNull
            v-if="!fields.length"
            description="这条提案没有可识别的字段改动"
          />
          <DiscussionThread
            anchor-kind="proposal"
            :anchor-id="proposal.id"
            :participants="discussionParticipants"
          />
        </div>

        <aside class="space-y-3 lg:sticky lg:top-16 lg:self-start">
          <KunCard
            v-if="isOpen && reviewable"
            bordered
            padding="md"
            content-class="space-y-3"
          >
            <div
              v-if="conflictKeys.length"
              :class="[
                'rounded-lg border px-3 py-2 text-xs',
                unresolved.length
                  ? 'border-warning-200 bg-warning-50 text-warning-800'
                  : 'border-success-200 bg-success-50 text-success-800'
              ]"
            >
              <p class="flex items-center gap-1.5 font-medium">
                <KunIcon
                  :name="
                    unresolved.length
                      ? 'lucide:triangle-alert'
                      : 'lucide:circle-check'
                  "
                  class="size-3.5"
                />
                {{
                  unresolved.length
                    ? `${unresolved.length} 处冲突待处理`
                    : `${conflictKeys.length} 处冲突已处理`
                }}
                <template v-if="autoMergeable.length">
                  · {{ autoMergeable.length }} 处可自动合并
                </template>
              </p>
              <KunButton
                v-if="autoMergeable.length"
                size="xs"
                variant="flat"
                color="primary"
                class-name="mt-2"
                :disabled="acting"
                @click="autoResolveAll"
              >
                <KunIcon name="lucide:git-merge" class="size-3.5" />
                可自动合并的全部采用自动合并
              </KunButton>
            </div>
            <p
              v-if="fields.length > conflictKeys.length"
              class="text-default-500 text-xs"
            >
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
              {{
                unresolved.length
                  ? `还有 ${unresolved.length} 处冲突待处理`
                  : amending
                    ? '修正并合并'
                    : '合并'
              }}
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
          <KunButton
            full-width
            variant="light"
            size="sm"
            @click="jumpToDiscussion"
          >
            <KunIcon name="lucide:messages-square" class="size-4" />
            跳到讨论
          </KunButton>
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
