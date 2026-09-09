<script setup lang="ts">
import type { SettingsAuditEntry, SettingKind } from '~~/shared/types/settings'
import {
  AUDIT_ACTION_LABELS,
  AUDIT_ACTION_COLORS,
  formatSettingValue
} from '~/constants/settings'

defineProps<{
  entries: SettingsAuditEntry[]
  hasError: boolean
  kinds: Record<string, SettingKind>
  siteNames: Record<string, string>
}>()
const emit = defineEmits<{ retry: [] }>()

const formatTime = (iso: string) => new Date(iso).toLocaleString()

const kindOf = (kinds: Record<string, SettingKind>, key: string): SettingKind =>
  kinds[key] ?? 'string'

const scopeLabel = (
  entry: SettingsAuditEntry,
  names: Record<string, string>
) =>
  entry.scope_kind === 'site'
    ? names[entry.scope_id] || `站点 #${entry.scope_id}`
    : '平台'
</script>

<template>
  <KunCard content-class="space-y-3 p-4">
    <h2 class="text-foreground text-lg font-bold">最近变更</h2>

    <CommonFetchError v-if="hasError" @retry="emit('retry')" />

    <div v-else class="overflow-x-auto">
      <table class="w-full min-w-[60rem] text-sm">
        <thead class="text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">时间</th>
            <th class="px-3 py-2 text-left font-medium">操作者</th>
            <th class="px-3 py-2 text-left font-medium">动作</th>
            <th class="px-3 py-2 text-left font-medium">作用域</th>
            <th class="px-3 py-2 text-left font-medium">键</th>
            <th class="px-3 py-2 text-left font-medium">旧值 → 新值</th>
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
            <td class="text-foreground px-3 py-2">
              {{ scopeLabel(entry, siteNames) }}
            </td>
            <td class="text-foreground px-3 py-2 font-mono break-all">
              {{ entry.key }}
            </td>
            <td class="text-foreground px-3 py-2 font-mono break-all">
              {{
                formatSettingValue(kindOf(kinds, entry.key), entry.old_value)
              }}
              →
              {{
                formatSettingValue(kindOf(kinds, entry.key), entry.new_value)
              }}
            </td>
            <td class="text-default-400 px-3 py-2 break-all">
              {{ entry.note || '—' }}
            </td>
          </tr>
          <tr v-if="!entries.length">
            <td colspan="7" class="text-default-400 px-3 py-8 text-center">
              暂无配置变更
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </KunCard>
</template>
