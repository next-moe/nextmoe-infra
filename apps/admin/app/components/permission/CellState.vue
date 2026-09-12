<script setup lang="ts">
import type { PermissionCell } from '~~/shared/types/permission'

defineProps<{ cell: PermissionCell }>()
</script>

<template>
  <span class="inline-flex items-center gap-1">
    <KunChip
      v-if="cell.source === 'deny'"
      color="danger"
      variant="flat"
      size="xs"
    >
      <span class="line-through">已撤销</span>
    </KunChip>

    <template v-else-if="cell.granted">
      <KunIcon
        name="lucide:check"
        class="size-4"
        :class="cell.source === 'grant' ? 'text-primary' : 'text-success'"
      />
      <span
        v-if="cell.source === 'grant'"
        class="bg-primary size-1.5 rounded-full"
      />
    </template>

    <span v-else class="text-default-300">—</span>
  </span>
</template>
