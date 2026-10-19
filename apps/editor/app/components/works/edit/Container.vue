<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'
import type { CatalogProposal } from '#shared/types/catalog'
import type { EditorImage } from '~/composables/useEditorContext'
import { WORK_EDIT_CONFIG } from '~/constants/editWork'

const route = useRoute()
const router = useRouter()
const api = useEditorApi()
const refs = useRefNames()

const workId = computed(() => String(route.params.id))
const editor = useEntityEditor(WORK_EDIT_CONFIG, workId)
const upstream = useWorkUpstream(workId)
const images = ref<Record<string, EditorImage>>({})

provideEditorContext({
  editor,
  config: WORK_EDIT_CONFIG,
  entityId: workId,
  upstreamRows: computed(() => upstream.upstream.value?.rows ?? {}),
  rosterIdentity: computed(() => upstream.upstream.value?.rosterIdentity ?? {}),
  images,
  refs
})

const { allowLeave } = useUnsavedGuard(editor.isDirty)

const groupOf = (key: string) => WORK_EDIT_CONFIG.fields[key]?.group ?? 'other'

const fieldsByGroup = computed(() => {
  const present = new Set(editor.fields.value.map((field) => field.key))
  const out: Record<string, string[]> = {}
  for (const field of editor.fields.value) {
    if (
      isSuppressedKey(field.key) &&
      present.has(field.key.slice(0, -'.suppressed'.length))
    ) {
      continue
    }
    const group = groupOf(field.key)
    out[group] = [...(out[group] ?? []), field.key]
  }
  return out
})

const changedByGroup = computed(() => {
  const parents = new Set(
    editor.changedKeys.value.map((key) =>
      isSuppressedKey(key) ? key.slice(0, -'.suppressed'.length) : key
    )
  )
  const out: Record<string, number> = {}
  for (const key of parents) {
    const group = groupOf(key)
    out[group] = (out[group] ?? 0) + 1
  }
  return out
})

const tabs = computed<KunTabItem[]>(() =>
  WORK_EDIT_CONFIG.groups
    .filter((group) => fieldsByGroup.value[group.value]?.length)
    .map((group) => ({
      value: group.value,
      icon: group.icon,
      textValue: changedByGroup.value[group.value]
        ? `${group.label} · ${changedByGroup.value[group.value]}`
        : group.label
    }))
)

const activeGroup = ref(
  typeof route.query.group === 'string' ? route.query.group : 'basic'
)
watch(tabs, (list) => {
  if (list.length && !list.some((tab) => tab.value === activeGroup.value)) {
    activeGroup.value = list[0]!.value
  }
})
watch(activeGroup, (group) => {
  router.replace({
    query: { ...route.query, group: group === 'basic' ? undefined : group }
  })
})

const canEditAnything = computed(() =>
  (editor.capabilities.value?.fields ?? []).some((cap) => cap.can_propose)
)

const reviewOpen = ref(false)
const openReview = () => {
  if (
    editor.isDirty.value &&
    !editor.issues.value.length &&
    !editor.blockedKeys.value.length
  ) {
    reviewOpen.value = true
  }
}

const jump = async (key: string) => {
  activeGroup.value = groupOf(key)
  await nextTick()
  document
    .getElementById(`field-${key}`)
    ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

const onSubmitted = async (proposal: CatalogProposal) => {
  allowLeave()
  editor.acceptSubmitted()
  if (proposal.state === 'merged') {
    useKunMessage('修改已生效', 'success')
    await navigateTo(`/works/${workId.value}`)
  } else {
    useKunMessage('提案已提交，等待审核', 'success')
    await navigateTo(`/me/proposals/${proposal.id}`)
  }
}

const onKeydown = (event: KeyboardEvent) => {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
    event.preventDefault()
    openReview()
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

const prefillFromProposal = async (proposalId: string) => {
  const proposal = await api<CatalogProposal>(
    `/v2/me/proposals/${proposalId}`,
    {
      query: { include: 'patch' }
    }
  ).catch(() => null)
  const patch = (proposal?.effective_patch ?? proposal?.patch) as
    Record<string, unknown> | undefined
  if (proposal && String(proposal.entity_id) === workId.value && patch) {
    editor.overlay(patch)
    useKunMessage(
      `已载入提案 #${proposalId} 的修改，可以继续调整后重新提交`,
      'info'
    )
  }
}

await Promise.all([editor.load(), upstream.load()])
images.value = { ...(upstream.upstream.value?.images ?? {}), ...images.value }
if (
  typeof route.query.from_proposal === 'string' &&
  /^\d+$/.test(route.query.from_proposal)
) {
  await prefillFromProposal(route.query.from_proposal)
}

useHead(() => ({
  title: `编辑 ${upstream.work.value?.display_name ?? `作品 #${workId.value}`} · 未萌编辑台`
}))
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-5">
    <WorksEditHeader :work-id="workId" :work="upstream.work.value" />

    <KunLoading
      v-if="editor.loading.value"
      spinner
      description="正在载入编辑数据…"
    />

    <KunInfo
      v-else-if="editor.loadError.value"
      color="danger"
      variant="flat"
      icon="lucide:circle-x"
      title="无法打开编辑器"
      :description="editor.loadError.value"
    />

    <template v-else>
      <KunInfo
        v-if="editor.storedDraft.value"
        color="primary"
        variant="flat"
        icon="lucide:file-clock"
        title="发现未提交的草稿"
        :description="`保存于 ${formatDateTime(editor.storedDraft.value.savedAt)}${editor.storedDraftIsStale.value ? '。这期间条目已被修改，恢复后请仔细核对。' : '。'}`"
      >
        <div class="mt-2 flex gap-2">
          <KunButton size="sm" color="primary" @click="editor.restoreDraft()"
            >恢复草稿</KunButton
          >
          <KunButton
            size="sm"
            variant="light"
            color="default"
            @click="editor.discardDraft()"
          >
            丢弃
          </KunButton>
        </div>
      </KunInfo>

      <KunInfo
        v-if="!canEditAnything"
        color="warning"
        variant="flat"
        icon="lucide:lock"
        :title="
          editor.capabilities.value?.quota_exhausted
            ? '今天的提案额度已用完'
            : '你目前不能修改这部作品'
        "
        :description="
          editor.capabilities.value?.quota_exhausted
            ? '额度在滚动窗口内逐步恢复，稍后再来。你仍然可以查看全部字段。'
            : '可以查看全部字段；需要修改时请联系审核者。'
        "
      />

      <div class="flex flex-col gap-5 lg:flex-row">
        <nav
          class="lg:sticky lg:top-16 lg:w-48 lg:shrink-0 lg:self-start"
          aria-label="字段分组"
        >
          <KunTab
            v-model="activeGroup"
            :items="tabs"
            variant="light"
            name="work-edit-groups"
            class="hidden lg:block"
            orientation="vertical"
            full-width
          />
          <KunTab
            v-model="activeGroup"
            :items="tabs"
            variant="underlined"
            name="work-edit-groups-mobile"
            class="lg:hidden"
          />
        </nav>

        <div class="min-w-0 flex-1 space-y-4">
          <EditField
            v-for="key in fieldsByGroup[activeGroup] ?? []"
            :key="key"
            :field-key="key"
          />
        </div>
      </div>

      <EditSubmitBar @jump="jump" @review="openReview" />
      <EditSubmitDialog v-model="reviewOpen" @submitted="onSubmitted" />
    </template>
  </div>
</template>
