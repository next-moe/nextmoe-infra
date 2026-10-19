<script setup lang="ts">
import type { CatalogRevision, CatalogSchemaField } from '#shared/types/catalog'
import type { DiffContext } from '~/utils/review/context'
import { revertChanges } from '~/utils/review/history'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'
import { useRevisionDiffs } from '~/utils/review/revisionDiff'

const props = defineProps<{
  target: CatalogRevision | null
  latest: CatalogRevision | null
  fields: readonly CatalogSchemaField[]
  context: DiffContext
  prepare: (values: { key: string; value: unknown }[]) => Promise<void>
}>()

const emit = defineEmits<{ done: [] }>()

const open = defineModel<boolean>({ required: true })

const api = useEditorApi()
const diffs = useRevisionDiffs()
const reason = ref('')
const submitting = ref(false)

const state = computed(() =>
  props.target && props.latest
    ? diffs.stateOf(props.latest.id, props.target.id)
    : undefined
)

const changes = computed(() => revertChanges(state.value?.entries ?? []))

const previewEntries = computed(() =>
  changes.value.map((change) => ({
    key: change.key,
    from: change.current,
    to: change.restored
  }))
)

watch(open, async (value) => {
  if (!value || !props.target || !props.latest) {
    return
  }
  reason.value = ''
  const entries = await diffs.load(props.latest.id, props.target.id)
  if (entries) {
    await props.prepare(
      entries.flatMap((entry) => [
        { key: entry.key, value: entry.from },
        { key: entry.key, value: entry.to }
      ])
    )
  }
})

const submit = async () => {
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
        ...(reason.value.trim() ? { reason: reason.value.trim() } : {})
      }
    })
    useKunMessage(`已回滚到第 ${props.target.seq} 版`, 'success')
    open.value = false
    emit('done')
  } catch (error) {
    useKunMessage(problemMessage(error, '回滚失败'), 'error')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="props.target ? `回滚到第 ${props.target.seq} 版` : '回滚'"
    description="回滚会生成一条新的修订，把下面这些字段恢复成那一版的值；历史不会被改写，回滚本身也能再回滚。"
    size="xl"
    role="alertdialog"
  >
    <div class="space-y-4">
      <HistoryDiffPanel
        :entries="previewEntries"
        :fields="props.fields"
        :context="props.context"
        before-label="当前"
        after-label="回滚后"
        :loading="state?.loading"
        :error="state?.error"
        empty-text="当前值和那一版完全一致，不需要回滚"
      />
      <KunTextarea
        v-model="reason"
        placeholder="回滚原因（可选，会记入修订）"
        aria-label="回滚原因"
        max-height="8rem"
      />
      <div class="flex justify-end gap-2">
        <KunButton variant="light" @click="open = false">取消</KunButton>
        <KunButton
          color="warning"
          :loading="submitting"
          :disabled="!changes.length || state?.loading"
          @click="submit"
        >
          确认回滚 {{ changes.length }} 个字段
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
