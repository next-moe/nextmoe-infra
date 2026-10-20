<script setup lang="ts">
import type { KunSelectOption } from '@kungal/ui-vue'
import type { CatalogRevision, CatalogSchemaField } from '#shared/types/catalog'
import type { DiffContext } from '~/utils/review/context'
import { revertChanges } from '~/utils/review/history'
import {
  conflictFieldKeys,
  newIdempotencyKey,
  problemCode,
  problemMessage
} from '~/utils/review/problem'
import {
  BASELINE_DIFF_BASE,
  useRevisionDiffs
} from '~/utils/review/revisionDiff'

type RevertMode = 'restore' | 'undo'

const props = withDefaults(
  defineProps<{
    target: CatalogRevision | null
    latest: CatalogRevision | null
    fields: readonly CatalogSchemaField[]
    context: DiffContext
    prepare: (values: { key: string; value: unknown }[]) => Promise<void>
    baseline?: boolean
  }>(),
  { baseline: false }
)

const emit = defineEmits<{ done: [] }>()

const open = defineModel<boolean>({ required: true })

const api = useEditorApi()
const diffs = useRevisionDiffs()
const reason = ref('')
const mode = ref<RevertMode>('restore')
const conflicts = ref<string[]>([])
const submitting = ref(false)

const modeOptions: KunSelectOption<RevertMode>[] = [
  { value: 'restore', label: '恢复到这一版' },
  { value: 'undo', label: '只撤销这一版的改动' }
]

const restoreBase = computed(() =>
  props.baseline ? BASELINE_DIFF_BASE : (props.target?.id ?? null)
)

const state = computed(() => {
  if (!props.target || !props.latest) {
    return undefined
  }
  return mode.value === 'undo' && !props.baseline
    ? diffs.stateOf(props.target.id)
    : diffs.stateOf(props.latest.id, restoreBase.value)
})

const changes = computed(() => revertChanges(state.value?.entries ?? []))

const previewEntries = computed(() =>
  changes.value.map((change) => ({
    key: change.key,
    from: change.current,
    to: change.restored
  }))
)

const title = computed(() => {
  if (props.baseline) {
    return '回滚到导入原始状态'
  }
  return props.target ? `回滚第 ${props.target.seq} 版` : '回滚'
})

const loadPreview = async () => {
  if (!props.target || !props.latest) {
    return
  }
  const entries =
    mode.value === 'undo' && !props.baseline
      ? await diffs.load(props.target.id)
      : await diffs.load(props.latest.id, restoreBase.value)
  if (entries) {
    await props.prepare(
      entries.flatMap((entry) => [
        { key: entry.key, value: entry.from },
        { key: entry.key, value: entry.to }
      ])
    )
  }
}

watch(open, (value) => {
  if (!value) {
    return
  }
  reason.value = ''
  conflicts.value = []
  mode.value = 'restore'
  loadPreview()
})
watch(mode, () => {
  conflicts.value = []
  if (open.value) {
    loadPreview()
  }
})

const submit = async (force = false) => {
  if (!props.target || submitting.value) {
    return
  }
  submitting.value = true
  try {
    await api('/v2/moderation/reverts', {
      method: 'POST',
      headers: { 'Idempotency-Key': newIdempotencyKey() },
      body: {
        revision_id: props.target.id,
        mode: props.baseline ? 'restore' : mode.value,
        ...(props.baseline ? { baseline: true } : {}),
        ...(force ? { force: true } : {}),
        ...(reason.value.trim() ? { reason: reason.value.trim() } : {})
      }
    })
    useKunMessage(
      props.baseline
        ? '已回滚到导入原始状态'
        : mode.value === 'undo'
          ? `已撤销第 ${props.target.seq} 版的改动`
          : `已恢复到第 ${props.target.seq} 版`,
      'success'
    )
    open.value = false
    emit('done')
  } catch (error) {
    if (problemCode(error) === 'REVERT_CONFLICT') {
      conflicts.value = conflictFieldKeys(error)
      return
    }
    useKunMessage(problemMessage(error, '回滚失败'), 'error')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="title"
    description="回滚会生成一条新的修订，历史不会被改写，回滚本身也能再回滚。"
    size="xl"
    role="alertdialog"
  >
    <div class="space-y-4">
      <div v-if="!props.baseline" class="space-y-1">
        <KunSelect
          v-model="mode"
          :options="modeOptions"
          aria-label="回滚方式"
          class="max-w-xs"
        />
        <p class="text-default-500 text-xs">
          {{
            mode === 'restore'
              ? '把所有字段恢复成这一版之后的值，之后其他人的修改会一并被覆盖。'
              : '只把这一版改动过的字段恢复成它改动之前的值，其他字段保持现状。'
          }}
        </p>
      </div>
      <p v-else class="text-default-500 text-sm">
        把这个条目恢复成第一次人工编辑之前、资料源导入时的状态。
      </p>

      <HistoryDiffPanel
        :entries="previewEntries"
        :fields="props.fields"
        :context="props.context"
        :before-label="
          mode === 'undo' && !props.baseline ? '这一版改动后' : '当前'
        "
        :after-label="mode === 'undo' && !props.baseline ? '撤销后' : '回滚后'"
        :loading="state?.loading"
        :error="state?.error"
        empty-text="没有需要恢复的字段"
      />

      <KunInfo
        v-if="conflicts.length"
        color="warning"
        variant="flat"
        title="之后的修订也改过这些字段"
        :description="`${conflicts.map((key) => fieldLabel(key)).join('、')} 在这一版之后又被修改过。强制撤销会覆盖那些后来的修改。`"
      />

      <KunTextarea
        v-model="reason"
        placeholder="回滚原因（可选，会记入修订）"
        aria-label="回滚原因"
        max-height="128px"
      />
      <div class="flex justify-end gap-2">
        <KunButton variant="light" @click="open = false">取消</KunButton>
        <KunButton
          v-if="conflicts.length"
          color="danger"
          :loading="submitting"
          @click="submit(true)"
        >
          强制撤销
        </KunButton>
        <KunButton
          v-else
          color="warning"
          :loading="submitting"
          :disabled="!changes.length || state?.loading"
          @click="submit()"
        >
          确认回滚 {{ changes.length }} 个字段
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
