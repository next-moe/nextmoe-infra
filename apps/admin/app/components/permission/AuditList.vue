<script setup lang="ts">
import type { PermissionAuditEntry } from '~~/shared/types/permission'
import { roleLabel } from '~/constants/roles'
import {
  AUDIT_ACTION_LABELS,
  AUDIT_ACTION_COLORS
} from '~/constants/permission'

defineProps<{ entries: PermissionAuditEntry[]; hasError: boolean }>()
const emit = defineEmits<{ retry: [] }>()

const formatTime = (iso: string) => new Date(iso).toLocaleString()
</script>

<template>
  <KunCard content-class="space-y-3 p-4">
    <h2 class="text-foreground text-lg font-bold">最近变更</h2>

    <CommonFetchError v-if="hasError" @retry="emit('retry')" />

    <div v-else class="overflow-x-auto">
      <table class="w-full min-w-[40rem] text-sm">
        <thead class="text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">时间</th>
            <th class="px-3 py-2 text-left font-medium">操作者</th>
            <th class="px-3 py-2 text-left font-medium">动作</th>
            <th class="px-3 py-2 text-left font-medium">角色</th>
            <th class="px-3 py-2 text-left font-medium">权限键</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="entry in entries"
            :key="entry.id"
            class="border-default-200 border-t align-top"
          >
            <td class="text-default-400 px-3 py-2">
              {{ formatTime(entry.created_at) }}
            </td>
            <td class="text-foreground px-3 py-2">
              {{ entry.actor_name || `#${entry.actor_user_id}` }}
            </td>
            <td class="px-3 py-2">
              <KunChip
                :color="AUDIT_ACTION_COLORS[entry.action]"
                variant="flat"
                size="xs"
              >
                {{ AUDIT_ACTION_LABELS[entry.action] || entry.action }}
              </KunChip>
            </td>
            <td class="text-default-500 px-3 py-2">
              {{ roleLabel(entry.role) }}
            </td>
            <td class="text-foreground px-3 py-2 font-mono break-all">
              {{ entry.permission }}
            </td>
          </tr>
          <tr v-if="!entries.length">
            <td colspan="5" class="text-default-400 px-3 py-8 text-center">
              暂无叠加授权变更
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </KunCard>
</template>
