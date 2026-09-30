<script setup lang="ts">
import { telemetryKindMeta } from '~/constants/telemetry'

defineProps<{ kind: string }>()
</script>

<template>
  <KunChip :color="telemetryKindMeta(kind).color" variant="flat" size="xs">
    {{ telemetryKindMeta(kind).label }}
  </KunChip>
</template>
