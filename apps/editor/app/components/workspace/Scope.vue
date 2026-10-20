<script setup lang="ts">
import type { EntityWorkspace } from '~/composables/useEntityWorkspace'

const props = defineProps<{ workspace: EntityWorkspace }>()

provideEditorContext(props.workspace.context)
</script>

<template>
  <slot />
</template>
