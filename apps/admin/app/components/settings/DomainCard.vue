<script setup lang="ts">
import type {
  SettingsDomainView,
  SettingsKeyView
} from '~~/shared/types/settings'
import {
  KIND_LABELS,
  SOURCE_LABELS,
  SOURCE_COLORS,
  ENV_FLOOR_NOTE,
  PUBLIC_BADGE,
  PUBLIC_NOTE,
  SITE_SCOPED_BADGE,
  SITE_SCOPED_NOTE,
  formatSettingValue
} from '~/constants/settings'

defineProps<{
  domain: SettingsDomainView
  writable: boolean
  scopeKind: 'platform' | 'site'
}>()
const emit = defineEmits<{
  edit: [row: SettingsKeyView]
  reset: [row: SettingsKeyView]
}>()

const formatTime = (iso: string) => new Date(iso).toLocaleString()

const rangeText = (row: SettingsKeyView): string => {
  if (row.min != null && row.max != null) return `${row.min}–${row.max}`
  if (row.min != null) return `≥ ${row.min}`
  if (row.max != null) return `≤ ${row.max}`
  return ''
}

const ownSource = (scopeKind: 'platform' | 'site') =>
  scopeKind === 'site' ? 'site' : 'db'
</script>

<template>
  <KunCard content-class="space-y-3 p-4">
    <div class="flex flex-wrap items-baseline gap-2">
      <h2 class="text-foreground text-lg font-bold">{{ domain.title_zh }}</h2>
      <span class="text-default-400 font-mono text-sm">{{ domain.name }}</span>
    </div>

    <div class="overflow-x-auto">
      <table class="w-full min-w-[64rem] text-sm">
        <thead class="text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">键</th>
            <th class="px-3 py-2 text-left font-medium">类型</th>
            <th class="px-3 py-2 text-left font-medium">生效值</th>
            <th class="px-3 py-2 text-left font-medium">来源</th>
            <th class="px-3 py-2 text-left font-medium">环境变量</th>
            <th class="px-3 py-2 text-left font-medium">备注/修改人</th>
            <th class="px-3 py-2 text-left font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in domain.keys"
            :key="row.key"
            class="border-default-200 border-t align-top"
          >
            <td class="px-3 py-2">
              <KunTooltip :text="row.desc_en" position="top">
                <span class="text-foreground font-mono break-all">
                  {{ row.key }}
                </span>
              </KunTooltip>
              <p class="text-default-500 mt-0.5">{{ row.desc_zh }}</p>
              <div
                v-if="row.public || row.site_scoped"
                class="mt-0.5 flex flex-wrap items-center gap-1"
              >
                <KunTooltip
                  v-if="row.public"
                  :text="PUBLIC_NOTE"
                  position="top"
                >
                  <KunChip color="info" variant="flat" size="xs">
                    {{ PUBLIC_BADGE }}
                  </KunChip>
                </KunTooltip>
                <KunTooltip
                  v-if="row.site_scoped"
                  :text="SITE_SCOPED_NOTE"
                  position="top"
                >
                  <KunChip color="secondary" variant="flat" size="xs">
                    {{ SITE_SCOPED_BADGE }}
                  </KunChip>
                </KunTooltip>
              </div>
            </td>

            <td class="px-3 py-2">
              <KunChip color="default" variant="flat" size="xs">
                {{ KIND_LABELS[row.kind] }}
              </KunChip>
              <p
                v-if="row.enum?.length"
                class="text-default-400 mt-0.5 break-all"
              >
                {{ row.enum.join(', ') }}
              </p>
              <p v-if="rangeText(row)" class="text-default-400 mt-0.5">
                {{ rangeText(row) }}
              </p>
              <p
                v-if="row.pattern"
                class="text-default-400 mt-0.5 font-mono break-all"
              >
                {{ row.pattern }}
              </p>
            </td>

            <td class="px-3 py-2">
              <span class="text-foreground font-mono break-all">
                {{ formatSettingValue(row.kind, row.effective) }}
              </span>
              <p
                v-if="scopeKind === 'platform' && row.source === 'db'"
                class="text-default-400 mt-0.5"
              >
                默认 {{ formatSettingValue(row.kind, row.default) }}
              </p>
              <p
                v-else-if="scopeKind === 'site' && row.source === 'site'"
                class="text-default-400 mt-0.5"
              >
                平台 {{ formatSettingValue(row.kind, row.inherited) }}
              </p>
            </td>

            <td class="px-3 py-2">
              <div class="flex flex-wrap items-center gap-1">
                <KunChip
                  :color="SOURCE_COLORS[row.source]"
                  variant="flat"
                  size="xs"
                >
                  {{ SOURCE_LABELS[row.source] }}
                </KunChip>
                <KunChip
                  v-if="
                    row.override != null && row.source !== ownSource(scopeKind)
                  "
                  color="danger"
                  variant="flat"
                  size="xs"
                >
                  覆盖值无效
                </KunChip>
              </div>
            </td>

            <td class="px-3 py-2">
              <KunTooltip
                v-if="row.env_var"
                :text="ENV_FLOOR_NOTE"
                position="top"
              >
                <span class="text-foreground font-mono break-all">
                  {{ row.env_var }}
                </span>
              </KunTooltip>
              <span v-else class="text-default-400">—</span>
            </td>

            <td class="px-3 py-2">
              <template v-if="row.override">
                <p class="text-foreground break-all">
                  {{ row.override.note || '—' }}
                </p>
                <p class="text-default-400 mt-0.5">
                  {{
                    row.override.updated_by_name ||
                    `#${row.override.updated_by_user_id}`
                  }}
                  · {{ formatTime(row.override.updated_at) }}
                </p>
              </template>
              <span v-else class="text-default-400">—</span>
            </td>

            <td class="px-3 py-2">
              <div v-if="writable" class="flex flex-wrap gap-1">
                <KunButton
                  size="sm"
                  variant="flat"
                  color="primary"
                  @click="emit('edit', row)"
                >
                  编辑
                </KunButton>
                <KunButton
                  v-if="row.override"
                  size="sm"
                  variant="flat"
                  color="warning"
                  @click="emit('reset', row)"
                >
                  撤销覆盖
                </KunButton>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </KunCard>
</template>
