<script setup lang="ts">
import type { EnvServiceValue, EnvVarView } from '~~/shared/types/settings'
import {
  ENV_ISSUE_COLORS,
  ENV_ISSUE_LABELS,
  ENV_ISSUE_NOTES,
  ENV_KIND_LABELS,
  ENV_KIND_NOTES,
  ENV_SOURCE_COLORS,
  ENV_SOURCE_LABELS,
  envScopeLabel
} from '~/constants/settings-env'

defineProps<{ rows: EnvVarView[]; writable: boolean }>()
const emit = defineEmits<{
  edit: [row: EnvVarView]
  reset: [row: EnvVarView, scope: string]
}>()

const formatTime = (iso: string) => new Date(iso).toLocaleString()

const shown = (row: EnvVarView, v: EnvServiceValue) => {
  if (row.secret) {
    return v.set ? `已设置 · ${v.fingerprint}` : '未设置'
  }
  return v.value || '(空)'
}

const uniform = (row: EnvVarView) => {
  const [first] = row.services
  if (!first || row.diverged || row.issues.length) return false
  return row.services.every(
    (v) => v.source === first.source && v.set === first.set
  )
}
</script>

<template>
  <KunCard content-class="p-0">
    <div class="overflow-x-auto">
      <table class="w-full min-w-[64rem] text-sm">
        <thead class="text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">变量</th>
            <th class="px-3 py-2 text-left font-medium">各服务生效值</th>
            <th class="px-3 py-2 text-left font-medium">配置中心存储值</th>
            <th class="px-3 py-2 text-left font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in rows"
            :key="row.key"
            class="border-default-200 border-t align-top"
          >
            <td class="px-3 py-2">
              <span class="text-foreground font-mono break-all">
                {{ row.key }}
              </span>
              <div class="mt-1 flex flex-wrap items-center gap-1">
                <KunChip
                  v-if="row.secret"
                  color="secondary"
                  variant="flat"
                  size="xs"
                >
                  密钥
                </KunChip>
                <KunTooltip
                  v-if="ENV_KIND_LABELS[row.kind]"
                  :text="ENV_KIND_NOTES[row.kind]"
                  position="top"
                >
                  <KunChip color="default" variant="flat" size="xs">
                    {{ ENV_KIND_LABELS[row.kind] }}
                  </KunChip>
                </KunTooltip>
                <KunChip
                  v-if="row.diverged"
                  color="info"
                  variant="flat"
                  size="xs"
                >
                  各服务不同
                </KunChip>
              </div>
              <p
                v-if="
                  row.default &&
                  row.services.some((v) => v.value !== row.default)
                "
                class="text-default-400 mt-1 break-all"
              >
                默认 {{ row.default }}
              </p>
            </td>

            <td class="px-3 py-2">
              <div
                v-if="uniform(row)"
                class="flex flex-wrap items-center gap-2"
              >
                <span class="text-foreground font-mono break-all">
                  {{ shown(row, row.services[0]!) }}
                </span>
                <KunChip
                  :color="ENV_SOURCE_COLORS[row.services[0]!.source]"
                  variant="flat"
                  size="xs"
                >
                  {{ ENV_SOURCE_LABELS[row.services[0]!.source] }}
                </KunChip>
                <span class="text-default-400">
                  {{ row.services.length }} 个服务
                </span>
              </div>
              <ul v-else class="space-y-1">
                <li
                  v-for="v in row.services"
                  :key="v.service"
                  class="flex flex-wrap items-center gap-2"
                >
                  <span class="text-default-500 w-28 shrink-0 font-mono">
                    {{ v.service }}
                  </span>
                  <span class="text-foreground font-mono break-all">
                    {{ shown(row, v) }}
                  </span>
                  <KunChip
                    :color="ENV_SOURCE_COLORS[v.source]"
                    variant="flat"
                    size="xs"
                  >
                    {{ ENV_SOURCE_LABELS[v.source] }}
                  </KunChip>
                  <KunTooltip
                    v-if="v.issue"
                    :text="ENV_ISSUE_NOTES[v.issue]"
                    position="top"
                  >
                    <KunChip
                      :color="ENV_ISSUE_COLORS[v.issue]"
                      variant="flat"
                      size="xs"
                    >
                      {{ ENV_ISSUE_LABELS[v.issue] }}
                    </KunChip>
                  </KunTooltip>
                </li>
              </ul>
            </td>

            <td class="px-3 py-2">
              <ul v-if="row.stored.length" class="space-y-2">
                <li v-for="s in row.stored" :key="s.scope">
                  <div class="flex flex-wrap items-center gap-2">
                    <KunChip color="primary" variant="flat" size="xs">
                      {{ envScopeLabel(s.scope) }}
                    </KunChip>
                    <span class="text-foreground font-mono break-all">
                      {{
                        row.secret ? `${s.hint} · ${s.fingerprint}` : s.value
                      }}
                    </span>
                    <KunButton
                      v-if="writable"
                      size="xs"
                      variant="light"
                      color="warning"
                      @click="emit('reset', row, s.scope)"
                    >
                      撤销
                    </KunButton>
                  </div>
                  <p class="text-default-400 mt-0.5 text-xs">
                    v{{ s.version }} · {{ s.updated_by_name || '导入工具' }} ·
                    {{ formatTime(s.updated_at) }}
                    <template v-if="s.note">· {{ s.note }}</template>
                  </p>
                </li>
              </ul>
              <span v-else class="text-default-400">—</span>
            </td>

            <td class="px-3 py-2">
              <KunButton
                v-if="writable && row.kind === 'storable'"
                size="sm"
                variant="flat"
                color="primary"
                @click="emit('edit', row)"
              >
                写入
              </KunButton>
            </td>
          </tr>
          <tr v-if="!rows.length">
            <td colspan="4" class="text-default-400 px-3 py-8 text-center">
              没有符合条件的变量
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </KunCard>
</template>
