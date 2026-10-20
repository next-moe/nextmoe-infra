<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'

defineProps<{ section: WorkspaceSectionDef }>()
</script>

<template>
  <EditMediaGallery kind="cover" />
</template>
