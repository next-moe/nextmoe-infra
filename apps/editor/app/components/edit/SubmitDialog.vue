<script setup lang="ts">
import type { CatalogProposal } from '#shared/types/catalog'

const NOTE_MAX = 2000

const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ submitted: [proposal: CatalogProposal] }>()

const { editor } = useEditorContext()

const note = ref('')
const conflicts = ref<string[]>([])
const checking = ref(false)
const submitting = ref(false)
const error = ref('')

watch(open, async (visible) => {
  if (!visible) {
    return
  }
  error.value = ''
  checking.value = true
  conflicts.value = await editor.findConflicts()
  checking.value = false
})

const submit = async () => {
  submitting.value = true
  error.value = ''
  const result = await editor.submit(note.value.trim())
  submitting.value = false
  if (result.ok) {
    note.value = ''
    open.value = false
    emit('submitted', result.proposal)
  } else {
    error.value = result.message
  }
}

const reload = async () => {
  const ok = await useKunAlert({
    title: '重新载入最新内容',
    message: '这会放弃你当前的全部修改，并载入条目的最新内容。',
    type: 'warning',
    confirmText: '重新载入',
    cancelText: '取消'
  })
  if (ok) {
    open.value = false
    await editor.load()
    editor.discardDraft()
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    title="检查并提交"
    :description="
      editor.willAutomerge.value
        ? '你的修改会在提交后立即生效，并记入修订历史。'
        : '你的修改会作为提案进入审核队列，审核通过后生效。'
    "
    size="lg"
  >
    <div class="space-y-4">
      <KunLoading
        v-if="checking"
        spinner
        description="正在检查条目是否有新的改动…"
      />
      <KunInfo
        v-else-if="conflicts.length"
        color="danger"
        variant="flat"
        icon="lucide:triangle-alert"
        title="条目在你编辑期间被修改过"
        :description="`以下字段已有更新：${conflicts.map((k) => fieldLabel(k)).join('、')}。继续提交会用你的版本覆盖这些字段的当前内容。`"
      />
      <div v-if="conflicts.length" class="flex justify-end">
        <KunButton size="sm" variant="light" color="danger" @click="reload">
          放弃修改并载入最新内容
        </KunButton>
      </div>

      <div class="space-y-2">
        <EditFieldDiff
          v-for="key in editor.changedKeys.value"
          :key="key"
          :field-key="key"
          :conflict="conflicts.includes(key)"
        />
      </div>

      <KunTextarea
        v-model="note"
        label="修改说明"
        placeholder="简单说明修改原因和资料出处，方便审核者判断"
        :max-height="'12rem'"
        :description="`${note.length} / ${NOTE_MAX}`"
        :error="note.length > NOTE_MAX ? `最多 ${NOTE_MAX} 字` : ''"
      />

      <KunInfo
        v-if="error"
        color="danger"
        variant="flat"
        icon="lucide:circle-x"
        title="提交失败"
        :description="error"
      />

      <div class="flex justify-end gap-2">
        <KunButton variant="light" color="default" @click="open = false">
          继续编辑
        </KunButton>
        <KunButton
          color="primary"
          :loading="submitting"
          :disabled="checking || note.length > NOTE_MAX"
          @click="submit"
        >
          {{ editor.willAutomerge.value ? '提交并生效' : '提交审核' }}
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
