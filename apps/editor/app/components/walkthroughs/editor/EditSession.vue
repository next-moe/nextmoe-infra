<script setup lang="ts">
import type { CatalogProposal } from '#shared/types/catalog'
import type { WtChart, WtLang, WtPin } from '#shared/types/walkthrough'
import type { WalkthroughDocumentSource } from '~/composables/useWalkthroughDocument'
import { WT_FIELD } from '~/constants/walkthrough'

const route = useRoute()
const api = useEditorApi()
const { load: loadSession } = useEditorSession()
const { capture } = useReturnLink()

capture()
callOnce('editor-session', loadSession)

const entityId = String(route.params.id)
const session = createWorkspaceSession({ family: 'walkthrough', id: entityId })
provideWorkspaceSession(session)
onBeforeUnmount(session.dispose)

const workspace = session.root.workspace
const editor = workspace.editor
provideEditorContext(workspace.context)

const value = <T,>(key: string, fallback: T) =>
  (editor.draft.value[key] ?? fallback) as T

const canPropose = (key: string) =>
  Boolean(editor.capabilityByKey.value.get(key)?.can_propose)

const source: WalkthroughDocumentSource = {
  chart: () => editor.draft.value[WT_FIELD.chart],
  setChart: (chart: WtChart) => editor.setValue(WT_FIELD.chart, chart),
  lang: () => value<WtLang>(WT_FIELD.lang, 'ja'),
  setLang: (lang: WtLang) => editor.setValue(WT_FIELD.lang, lang),
  hidden: () => value<boolean>(WT_FIELD.hidden, false),
  setHidden: (hidden: boolean) => editor.setValue(WT_FIELD.hidden, hidden),
  pins: () => value<WtPin[]>(WT_FIELD.pins, []),
  setPins: (pins: WtPin[]) => editor.setValue(WT_FIELD.pins, pins),
  workId: () => {
    const id = editor.draft.value[WT_FIELD.work]
    return id === undefined || id === null ? null : String(id)
  },
  setWorkId: (id: string) => editor.setValue(WT_FIELD.work, Number(id)),
  readonly: () => !canPropose(WT_FIELD.chart)
}

const workId = computed(() => source.workId())

const changesOpen = ref(false)
const submitOpen = ref(false)

const openSubmit = () => {
  if (session.canSubmit.value) {
    submitOpen.value = true
  } else if (session.isDirty.value) {
    changesOpen.value = true
  }
}

useUnsavedGuard(session.isDirty)

const prefillFromProposal = async (proposalId: string) => {
  const proposal = await api<CatalogProposal>(
    `/v2/me/proposals/${proposalId}`,
    { query: { include: 'patch' } }
  ).catch(() => null)
  const patch = (proposal?.effective_patch ?? proposal?.patch) as
    Record<string, unknown> | undefined
  if (
    proposal &&
    proposal.target_object === 'walkthrough' &&
    String(proposal.entity_id) === entityId &&
    patch
  ) {
    editor.overlay(patch)
    useKunMessage(
      `已载入提案 #${proposalId} 的修改，可以继续调整后重新提交`,
      'info'
    )
  }
}

workspace.load().then(() => {
  const fromProposal = route.query.from_proposal
  if (typeof fromProposal === 'string' && /^\d+$/.test(fromProposal)) {
    prefillFromProposal(fromProposal)
  }
})

const submitHint = computed(() =>
  session.blockedCount.value
    ? `${session.blockedCount.value} 项修改你没有权限提交`
    : session.willAutomerge.value
      ? '提交后直接生效（Ctrl/⌘ + S）'
      : '提交后进入审核（Ctrl/⌘ + S）'
)

useHead({ title: `编辑攻略 #${entityId} · 未萌编辑台` })
</script>

<template>
  <div
    class="bg-background text-foreground flex h-dvh flex-col overflow-hidden"
  >
    <div v-if="editor.loading.value" class="flex flex-1 flex-col">
      <div class="border-default-200 bg-content1 h-12 shrink-0 border-b" />
      <div class="flex min-h-0 flex-1 gap-4 p-4">
        <KunSkeleton width="16rem" height="100%" />
        <div class="flex-1 space-y-3">
          <KunSkeleton v-for="n in 6" :key="n" height="4.5rem" />
        </div>
        <KunSkeleton width="32%" height="100%" />
      </div>
    </div>

    <div v-else-if="editor.loadError.value" class="p-6">
      <KunInfo
        :color="editor.loadStatus.value === 404 ? 'default' : 'danger'"
        variant="flat"
        :icon="
          editor.loadStatus.value === 404
            ? 'lucide:construction'
            : 'lucide:circle-x'
        "
        title="无法打开攻略编辑器"
        :description="editor.loadError.value"
      />
    </div>

    <WalkthroughsEditorWorkbench
      v-else
      :source="source"
      :entity-id="entityId"
      :work-id="workId"
      :change-count="session.changeCount.value"
      :can-submit="session.canSubmit.value"
      :submit-hint="submitHint"
      @submit="openSubmit"
      @changes="changesOpen = true"
      @discard="session.discardAll()"
    >
      <template #notices>
        <WorkspaceDraftNotice :workspace="workspace" />
        <div
          v-if="session.submissions.value.length"
          class="bg-success-50 text-success-800 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg px-3 py-2 text-sm"
        >
          <KunIcon name="lucide:circle-check" class="size-4 shrink-0" />
          <span>本次已提交：</span>
          <NuxtLink
            v-for="entry in session.submissions.value"
            :key="entry.proposal.id"
            :to="`/me/proposals/${entry.proposal.id}`"
            target="_blank"
            class="hover:underline"
          >
            提案 #{{ entry.proposal.id }}（{{
              entry.proposal.state === 'merged' ? '已生效' : '待审核'
            }}）
          </NuxtLink>
        </div>
      </template>
    </WalkthroughsEditorWorkbench>

    <WorkspaceChangesModal v-model="changesOpen" />
    <WorkspaceSubmitDialog v-model="submitOpen" />
  </div>
</template>
