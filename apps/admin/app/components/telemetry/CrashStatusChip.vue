<script setup lang="ts">
import { telemetryCrashStatusMeta } from '~/constants/telemetry'

defineProps<{ status: string }>()
</script>

<template>
  <KunChip
    :color="telemetryCrashStatusMeta(status).color"
    variant="flat"
    size="xs"
  >
    {{ telemetryCrashStatusMeta(status).label }}
  </KunChip>
</template>
