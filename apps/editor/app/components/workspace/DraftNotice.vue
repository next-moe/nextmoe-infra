<script setup lang="ts">
import type { EntityWorkspace } from '~/composables/useEntityWorkspace'

const props = defineProps<{ workspace: EntityWorkspace }>()

const editor = computed(() => props.workspace.editor)
</script>

<template>
  <KunInfo
    v-if="editor.storedDraft.value"
    color="primary"
    variant="flat"
    icon="lucide:file-clock"
    title="发现未提交的草稿"
    :description="`保存于 ${formatDateTime(editor.storedDraft.value.savedAt)}${editor.storedDraftIsStale.value ? '。这期间条目已被修改，恢复后请仔细核对。' : '。'}`"
  >
    <div class="mt-2 flex gap-2">
      <KunButton size="sm" color="primary" @click="editor.restoreDraft()">
        恢复草稿
      </KunButton>
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
</template>
