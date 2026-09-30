<script setup lang="ts">
import { telemetryIssueStatusMeta } from '~/constants/telemetry'

defineProps<{ status: string }>()
</script>

<template>
  <KunChip
    :color="telemetryIssueStatusMeta(status).color"
    variant="flat"
    size="xs"
  >
    {{ telemetryIssueStatusMeta(status).label }}
  </KunChip>
</template>
