<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'

const props = defineProps<{ section: WorkspaceSectionDef }>()
</script>

<template>
  <WorkspaceGenericSection :section="props.section" />
</template>
