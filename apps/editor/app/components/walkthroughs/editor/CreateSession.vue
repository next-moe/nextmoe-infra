<script setup lang="ts">
import type {
  CatalogEditCapabilities,
  CatalogProposal
} from '#shared/types/catalog'
import type { WtChart, WtLang, WtPin } from '#shared/types/walkthrough'
import type { WalkthroughDocumentSource } from '~/composables/useWalkthroughDocument'
import { WT_FIELD } from '~/constants/walkthrough'
import {
  emptyChart,
  normalizeChart,
  validateChart
} from '~/utils/walkthrough/doc'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'

interface StoredCreation {
  savedAt: string
  chart: WtChart
  lang: WtLang
  pins: WtPin[]
}

const SAVE_DELAY_MS = 800

const route = useRoute()
const api = useEditorApi()
const { load: loadSession } = useEditorSession()
callOnce('editor-session', loadSession)

const workId = String(route.query.work_id)
const storageKey = `nextmoe-editor:walkthrough-new:${workId}`

const blank = emptyChart()
const chart = shallowRef<WtChart>(blank)
const lang = ref<WtLang>('zh-Hans')
const pins = ref<WtPin[]>([])
const releaseId = ref<string | null>(null)
const restoredAt = ref<string | null>(null)

const source: WalkthroughDocumentSource = {
  chart: () => chart.value,
  setChart: (next) => {
    chart.value = next
  },
  lang: () => lang.value,
  setLang: (next) => {
    lang.value = next
  },
  hidden: () => false,
  setHidden: () => undefined,
  pins: () => pins.value,
  setPins: (next) => {
    pins.value = next
  },
  workId: () => workId,
  setWorkId: null,
  releaseId: () => releaseId.value,
  setReleaseId: (next) => {
    releaseId.value = next
  },
  readonly: () => false
}

const isDirty = computed(
  () =>
    pins.value.length > 0 ||
    JSON.stringify(chart.value) !== JSON.stringify(blank)
)

const ready = ref(false)
onMounted(() => {
  try {
    const raw = localStorage.getItem(storageKey)
    const stored = raw ? (JSON.parse(raw) as StoredCreation) : null
    if (stored?.chart) {
      chart.value = normalizeChart(stored.chart)
      lang.value = stored.lang ?? 'zh-Hans'
      pins.value = stored.pins ?? []
      restoredAt.value = stored.savedAt
    }
  } catch {
    restoredAt.value = null
  }
  ready.value = true
})

let saveTimer: ReturnType<typeof setTimeout> | null = null
watch(
  [chart, lang, pins],
  () => {
    if (!ready.value) return
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
      try {
        if (isDirty.value) {
          const stored: StoredCreation = {
            savedAt: new Date().toISOString(),
            chart: chart.value,
            lang: lang.value,
            pins: pins.value
          }
          localStorage.setItem(storageKey, JSON.stringify(stored))
        } else {
          localStorage.removeItem(storageKey)
        }
      } catch {
        return
      }
    }, SAVE_DELAY_MS)
  },
  { deep: true }
)

const discardRestored = () => {
  chart.value = emptyChart()
  pins.value = []
  restoredAt.value = null
  try {
    localStorage.removeItem(storageKey)
  } catch {
    return
  }
}

const { data: capabilities } = useLazyAsyncData('walkthrough-create-caps', () =>
  api<CatalogEditCapabilities>('/v2/me/edit-capabilities/walkthrough').catch(
    () => null
  )
)
const canCreate = computed(
  () => capabilities.value?.creation?.can_create ?? true
)
const willAutomerge = computed(
  () => capabilities.value?.creation?.would_automerge ?? false
)

const issueCount = computed(() => validateChart(chart.value).length)
const submitHint = computed(() =>
  !canCreate.value
    ? '你目前不能新建攻略'
    : willAutomerge.value
      ? '提交后直接生效（Ctrl/⌘ + S）'
      : '提交后进入审核（Ctrl/⌘ + S）'
)

const submitOpen = ref(false)
const note = ref('')
const submitting = ref(false)
let idempotencyKey: string | null = null

const { allowLeave } = useUnsavedGuard(
  isDirty,
  '攻略还没有提交，确定要离开吗？草稿会保留在本机。'
)

const openSubmit = () => {
  if (canCreate.value && !issueCount.value) {
    submitOpen.value = true
  }
}

const submit = async () => {
  if (submitting.value) return
  submitting.value = true
  idempotencyKey ??= newIdempotencyKey()
  try {
    const proposal = await api<CatalogProposal>('/v2/me/creations', {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: {
        object: 'walkthrough',
        params: { work_id: Number(workId) },
        patch: {
          [WT_FIELD.chart]: chart.value,
          [WT_FIELD.lang]: lang.value,
          ...(pins.value.length ? { [WT_FIELD.pins]: pins.value } : {}),
          ...(releaseId.value
            ? { [WT_FIELD.release]: Number(releaseId.value) }
            : {})
        },
        note: note.value.trim()
      }
    })
    idempotencyKey = null
    try {
      localStorage.removeItem(storageKey)
    } catch {
      // the draft only lingers until the next visit overwrites it
    }
    submitOpen.value = false
    allowLeave()
    if (proposal.state === 'merged' && proposal.entity_id !== '0') {
      useKunMessage('攻略已发布，可以继续完善', 'success')
      await navigateTo(`/walkthroughs/${proposal.entity_id}/edit`, {
        replace: true
      })
      return
    }
    useKunMessage('攻略已提交，审核通过后就会出现在作品页上', 'success')
    await navigateTo(`/me/proposals/${proposal.id}`, { replace: true })
  } catch (error) {
    idempotencyKey = null
    useKunMessage(problemMessage(error, '提交失败'), 'error')
  } finally {
    submitting.value = false
  }
}

useHead({ title: '新建攻略 · 未萌编辑台' })
</script>

<template>
  <div
    class="bg-background text-foreground flex h-dvh flex-col overflow-hidden"
  >
    <WalkthroughsEditorWorkbench
      v-if="ready"
      :key="restoredAt ?? 'blank'"
      :source="source"
      :entity-id="null"
      :work-id="workId"
      :change-count="isDirty ? 1 : 0"
      :can-submit="canCreate && isDirty && !issueCount"
      :submit-hint="submitHint"
      @submit="openSubmit"
      @discard="discardRestored"
    >
      <template #notices>
        <KunInfo
          v-if="restoredAt"
          color="info"
          variant="flat"
          icon="lucide:history"
          title="已恢复上次没有提交的草稿"
          :description="`保存于 ${restoredAt.slice(0, 16).replace('T', ' ')}`"
        >
          <KunButton
            size="sm"
            variant="light"
            class-name="mt-1"
            @click="discardRestored"
          >
            丢弃草稿，从空白开始
          </KunButton>
        </KunInfo>
      </template>
    </WalkthroughsEditorWorkbench>

    <KunModal
      v-model="submitOpen"
      title="提交新攻略"
      :description="
        willAutomerge
          ? '你的提交会直接发布到作品页上。'
          : '提交后进入审核队列，审核通过后才会出现在作品页上。'
      "
      :is-dismissable="!submitting"
    >
      <div class="space-y-4">
        <KunTextarea
          v-model="note"
          label="备注"
          placeholder="可选：来源、写作思路，或需要审核者留意的地方"
          :maxlength="2000"
          :rows="3"
        />
        <div class="flex justify-end gap-2">
          <KunButton
            variant="light"
            :disabled="submitting"
            @click="submitOpen = false"
          >
            取消
          </KunButton>
          <KunButton color="primary" :loading="submitting" @click="submit">
            <KunIcon name="lucide:send" class="size-4" />
            提交
          </KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
