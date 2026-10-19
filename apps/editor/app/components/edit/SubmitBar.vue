<script setup lang="ts">
const emit = defineEmits<{ jump: [key: string]; review: [] }>()

const { editor, config } = useEditorContext()

const labelOf = (key: string) => {
  const parent = isSuppressedKey(key)
    ? key.slice(0, -'.suppressed'.length)
    : key
  return config.fields[parent]?.label ?? fieldLabel(parent)
}

const changedChips = computed(() => {
  const seen = new Set<string>()
  return editor.changedKeys.value
    .map((key) =>
      isSuppressedKey(key) ? key.slice(0, -'.suppressed'.length) : key
    )
    .filter((key) => (seen.has(key) ? false : (seen.add(key), true)))
    .map((key) => ({ key, label: labelOf(key) }))
})

const issueCount = computed(() => editor.issues.value.length)
const blockedCount = computed(() => editor.blockedKeys.value.length)
const canReview = computed(
  () =>
    editor.isDirty.value && issueCount.value === 0 && blockedCount.value === 0
)

const quotaText = computed(() => {
  const quota = editor.quota.value
  if (!quota) {
    return ''
  }
  const parts: string[] = []
  if (quota.proposals.remaining !== null) {
    parts.push(`今日还可提交 ${quota.proposals.remaining} 次`)
  }
  if (quota.quota_tier === 'editor' && quota.direct_edits.remaining !== null) {
    parts.push(`免审额度 ${quota.direct_edits.remaining}`)
  }
  return parts.join(' · ')
})

const confirmDiscard = async () => {
  const ok = await useKunAlert({
    title: '放弃全部修改',
    message: '这会清空当前页面上所有未提交的修改。',
    type: 'warning',
    confirmText: '放弃',
    cancelText: '保留'
  })
  if (ok) {
    editor.resetAll()
  }
}
</script>

<template>
  <div
    class="border-default-200 bg-content1 sticky bottom-3 z-20 rounded-xl border px-4 py-3 shadow-lg"
  >
    <div class="flex flex-col gap-2 lg:flex-row lg:items-center">
      <div class="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
        <template v-if="changedChips.length">
          <span class="text-default-500 mr-1 text-sm">
            已修改 {{ changedChips.length }} 项
          </span>
          <button
            v-for="chip in changedChips"
            :key="chip.key"
            type="button"
            class="bg-primary-50 text-primary-700 hover:bg-primary-100 rounded-md px-2 py-0.5 text-xs"
            @click="emit('jump', chip.key)"
          >
            {{ chip.label }}
          </button>
        </template>
        <span v-else class="text-default-400 text-sm">还没有修改</span>
        <KunChip v-if="issueCount" size="xs" variant="flat" color="danger">
          {{ issueCount }} 处需要修正
        </KunChip>
        <KunChip v-if="blockedCount" size="xs" variant="flat" color="danger">
          {{ blockedCount }} 项你无权修改
        </KunChip>
      </div>

      <div class="flex flex-wrap items-center justify-end gap-2">
        <span v-if="quotaText" class="text-default-400 text-xs">{{
          quotaText
        }}</span>
        <KunChip
          v-if="editor.isDirty.value"
          size="sm"
          variant="flat"
          :color="editor.willAutomerge.value ? 'success' : 'warning'"
        >
          <span class="inline-flex items-center gap-1">
            <KunIcon
              :name="
                editor.willAutomerge.value ? 'lucide:zap' : 'lucide:hourglass'
              "
              class="size-3.5"
            />
            {{
              editor.willAutomerge.value ? '提交后直接生效' : '提交后进入审核'
            }}
          </span>
        </KunChip>
        <KunButton
          variant="light"
          color="default"
          :disabled="!editor.isDirty.value"
          @click="confirmDiscard"
        >
          放弃修改
        </KunButton>
        <KunButton
          color="primary"
          :disabled="!canReview"
          @click="emit('review')"
        >
          <KunIcon name="lucide:send" class="size-4" />
          检查并提交
        </KunButton>
      </div>
    </div>
  </div>
</template>
