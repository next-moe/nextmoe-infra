<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'

const props = defineProps<{ section: WorkspaceSectionDef }>()
</script>

<template>
  <WorkspacePage :section="props.section">
    <WorkspaceFormSection :fields="props.section.fields" />
  </WorkspacePage>
</template>
