<script setup lang="ts">
const props = defineProps<{ fields: string[]; count: number }>()

const { editor } = useEditorContext()

const revert = async () => {
  const ok = await useKunAlert({
    title: '撤销这一页的修改',
    message: `这一页上的 ${props.count} 项修改会恢复为当前资料库里的内容。`,
    type: 'warning',
    confirmText: '撤销',
    cancelText: '保留'
  })
  if (ok) {
    for (const key of props.fields) {
      editor.revertField(key)
    }
  }
}
</script>

<template>
  <KunButton
    v-if="props.count"
    size="sm"
    variant="light"
    color="default"
    @click="revert"
  >
    <KunIcon name="lucide:undo-2" class="size-4" />
    撤销本页修改
  </KunButton>
</template>
