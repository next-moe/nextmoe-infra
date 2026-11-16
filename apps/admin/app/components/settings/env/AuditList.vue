<script setup lang="ts">
import type { EnvAuditEntry } from '~~/shared/types/settings'
import {
  ENV_AUDIT_ACTION_COLORS,
  ENV_AUDIT_ACTION_LABELS,
  envScopeLabel
} from '~/constants/settings-env'

defineProps<{ entries: EnvAuditEntry[]; hasError: boolean }>()
const emit = defineEmits<{ retry: [] }>()

const formatTime = (iso: string) => new Date(iso).toLocaleString()

const change = (entry: EnvAuditEntry) => {
  if (entry.old_value || entry.new_value) {
    return `${entry.old_value || '—'} → ${entry.new_value || '—'}`
  }
  return `${entry.old_fingerprint || '—'} → ${entry.new_fingerprint || '—'}`
}
</script>

<template>
  <KunCard content-class="space-y-3 p-4">
    <h2 class="text-foreground text-lg font-bold">环境变量与密钥</h2>

    <CommonFetchError v-if="hasError" @retry="emit('retry')" />

    <div v-else class="overflow-x-auto">
      <table class="w-full min-w-[60rem] text-sm">
        <thead class="text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">时间</th>
            <th class="px-3 py-2 text-left font-medium">操作者</th>
            <th class="px-3 py-2 text-left font-medium">动作</th>
            <th class="px-3 py-2 text-left font-medium">作用域</th>
            <th class="px-3 py-2 text-left font-medium">变量</th>
            <th class="px-3 py-2 text-left font-medium">旧 → 新(密钥为指纹)</th>
            <th class="px-3 py-2 text-left font-medium">备注</th>
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
              {{
                entry.actor_name ||
                (entry.actor_user_id ? `#${entry.actor_user_id}` : '导入工具')
              }}
            </td>
            <td class="px-3 py-2">
              <KunChip
                :color="ENV_AUDIT_ACTION_COLORS[entry.action]"
                variant="flat"
                size="xs"
              >
                {{ ENV_AUDIT_ACTION_LABELS[entry.action] || entry.action }}
              </KunChip>
            </td>
            <td class="text-foreground px-3 py-2">
              {{ envScopeLabel(entry.scope) }}
            </td>
            <td class="text-foreground px-3 py-2 font-mono break-all">
              {{ entry.key }}
            </td>
            <td class="text-foreground px-3 py-2 font-mono break-all">
              {{ change(entry) }}
            </td>
            <td class="text-default-400 px-3 py-2 break-all">
              {{ entry.note || '—' }}
            </td>
          </tr>
          <tr v-if="!entries.length">
            <td colspan="7" class="text-default-400 px-3 py-8 text-center">
              暂无变更
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </KunCard>
</template>
