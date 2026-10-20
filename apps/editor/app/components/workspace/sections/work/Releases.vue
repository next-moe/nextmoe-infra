<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'

const props = defineProps<{ section: WorkspaceSectionDef }>()
</script>

<template>
  <WorkspacePage :section="props.section">
    <WorkspaceEmpty
      icon="lucide:package"
      title="发行编辑即将在这里开放"
      description="目前可以在发行列表中单独编辑。"
    />
  </WorkspacePage>
</template>
