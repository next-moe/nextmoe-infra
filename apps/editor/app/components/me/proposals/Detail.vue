<script setup lang="ts">
import type { CatalogProposal } from '#shared/types/catalog'
import { PROPOSAL_STATE, SITE_LABELS } from '~/constants/catalog'
import { ENTITY_META, isEditFamily } from '~/constants/editEntities'

const route = useRoute()
const api = useEditorApi()
const names = useEntityNames()

const proposalId = computed(() => String(route.params.id))
const proposal = ref<CatalogProposal | null>(null)
const etag = ref('')
const loadError = ref('')
const withdrawing = ref(false)

const family = computed(() => {
  const target = proposal.value?.target_object ?? ''
  return isEditFamily(target) ? target : null
})

const state = computed(() =>
  proposal.value
    ? (PROPOSAL_STATE[proposal.value.state] ?? {
        label: proposal.value.state,
        color: 'default' as const
      })
    : null
)
const patch = computed(
  () =>
    (proposal.value?.effective_patch ?? proposal.value?.patch ?? {}) as Record<
      string,
      unknown
    >
)
const patchKeys = computed(() => Object.keys(patch.value))
const editable = computed(() => family.value !== null)

const load = async () => {
  loadError.value = ''
  try {
    const res = await api.raw<CatalogProposal>(
      `/v2/me/proposals/${proposalId.value}`,
      {
        query: { include: 'patch,amendments' }
      }
    )
    proposal.value = res._data ?? null
    etag.value = res.headers.get('etag') ?? ''
  } catch (error) {
    loadError.value = editProblemMessage(parseEditProblem(error))
    return
  }
  if (proposal.value) {
    await names.resolve([proposal.value])
  }
}

const withdraw = async () => {
  const ok = await useKunAlert({
    title: '撤回提案',
    message: '撤回后这条提案不会再被审核，修改不会生效。',
    type: 'warning',
    confirmText: '撤回',
    cancelText: '取消'
  })
  if (!ok) {
    return
  }
  withdrawing.value = true
  try {
    await api(`/v2/me/proposals/${proposalId.value}`, {
      method: 'PATCH',
      body: { state: 'withdrawn' },
      headers: { 'If-Match': etag.value }
    })
    useKunMessage('提案已撤回', 'success')
    await load()
  } catch (error) {
    useKunMessage(editProblemMessage(parseEditProblem(error)), 'error')
  } finally {
    withdrawing.value = false
  }
}

useHead(() => ({ title: `提案 #${proposalId.value} · 未萌编辑台` }))

await load()
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-5">
    <NuxtLink
      to="/me/proposals"
      class="text-default-500 hover:text-foreground inline-flex items-center gap-1 text-sm"
    >
      <KunIcon name="lucide:chevron-left" class="size-4" />
      我的提案
    </NuxtLink>

    <KunInfo
      v-if="loadError"
      color="danger"
      variant="flat"
      title="无法加载这条提案"
      :description="loadError"
    />

    <template v-else-if="proposal">
      <KunCard bordered padding="md">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0 space-y-1">
            <div class="flex flex-wrap items-center gap-2">
              <KunChip
                v-if="state"
                size="sm"
                variant="flat"
                :color="state.color"
              >
                {{ state.label }}
              </KunChip>
              <h1 class="text-lg font-semibold">提案 #{{ proposal.id }}</h1>
            </div>
            <CommonEntityLink
              :family="proposal.target_object"
              :id="proposal.entity_id"
              :name="names.nameOf(proposal)"
            />
            <p class="text-default-400 text-xs">
              {{ labelOf(SITE_LABELS, proposal.site) }} · 提交于
              {{ formatDateTime(proposal.created_at) }}
              <template v-if="proposal.decided_at">
                · 处理于 {{ formatDateTime(proposal.decided_at) }}
              </template>
              <template v-if="proposal.decided_by">
                · 处理人
                <HistoryUser :user="proposal.decided_by" />
              </template>
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <KunButton
              v-if="editable"
              :href="`${ENTITY_META[family!].path}/${proposal.entity_id}/edit?from_proposal=${proposal.id}`"
              variant="bordered"
              size="sm"
            >
              <KunIcon name="lucide:copy-plus" class="size-4" />
              基于此提案重新编辑
            </KunButton>
            <KunButton
              v-if="proposal.state === 'open'"
              color="danger"
              variant="flat"
              size="sm"
              :loading="withdrawing"
              @click="withdraw"
            >
              <KunIcon name="lucide:undo-2" class="size-4" />
              撤回
            </KunButton>
          </div>
        </div>
        <p
          v-if="proposal.note"
          class="bg-default-50 text-default-600 mt-3 rounded-lg p-3 text-sm whitespace-pre-line"
        >
          {{ proposal.note }}
        </p>
        <p
          v-if="proposal.amendments?.length"
          class="text-default-500 mt-3 text-xs"
        >
          审核者对这条提案做过
          {{ proposal.amendments.length }} 次修正，下方展示的是修正后的内容。
        </p>
      </KunCard>

      <KunCard bordered padding="md">
        <h2 class="mb-3 font-semibold">提议的修改</h2>
        <MeProposalsChanges
          v-if="family"
          :key="`${family}:${proposal.entity_id}:${proposal.state}`"
          :family="family"
          :entity-id="String(proposal.entity_id)"
          :patch="patch"
          :state="proposal.state"
        />
        <ul v-else class="text-default-600 space-y-1 text-sm">
          <li v-for="key in patchKeys" :key="key">{{ fieldLabel(key) }}</li>
        </ul>
      </KunCard>
    </template>
  </div>
</template>
