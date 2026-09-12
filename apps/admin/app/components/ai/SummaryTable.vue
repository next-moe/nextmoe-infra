<script setup lang="ts">
import {
  AI_STATUS_KEYS,
  AI_STATUS_META,
  AI_DEGRADED_CHANNEL_LABEL
} from '~/constants/ai'
import type { AiSummaryRow } from '~~/shared/types/ai'

const props = defineProps<{
  rows: AiSummaryRow[]
  loading: boolean
}>()

const fmtInt = (n?: number) => (n ?? 0).toLocaleString()

const statusChips = (row: AiSummaryRow) =>
  AI_STATUS_KEYS.map((key) => ({
    key,
    count: (row[key] as number) ?? 0,
    ...AI_STATUS_META[key]
  })).filter((s) => s.count > 0)
</script>

<template>
  <div class="space-y-2">
    <h2 class="text-foreground text-lg font-semibold">站点 × 路由 × 渠道</h2>
    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[56rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">站点</th>
            <th class="px-3 py-2 text-left font-medium">路由</th>
            <th class="px-3 py-2 text-left font-medium">渠道</th>
            <th class="px-3 py-2 text-right font-medium">调用</th>
            <th class="px-3 py-2 text-right font-medium">prompt</th>
            <th class="px-3 py-2 text-right font-medium">completion</th>
            <th class="px-3 py-2 text-right font-medium">成本 (µ)</th>
            <th class="px-3 py-2 text-left font-medium">状态分布</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(row, i) in props.rows"
            :key="`${row.site}-${row.route}-${row.channel}-${i}`"
            class="border-default-200 border-t align-top"
          >
            <td class="px-3 py-2">{{ row.site }}</td>
            <td class="px-3 py-2">
              <KunChip color="info" variant="flat" size="xs">{{ row.route }}</KunChip>
            </td>
            <td class="px-3 py-2">
              <span v-if="row.channel" class="font-mono">{{ row.channel }}</span>
              <span v-else class="text-default-400">{{ AI_DEGRADED_CHANNEL_LABEL }}</span>
            </td>
            <td class="px-3 py-2 text-right tabular-nums">{{ fmtInt(row.calls) }}</td>
            <td class="px-3 py-2 text-right tabular-nums">{{ fmtInt(row.prompt_tokens) }}</td>
            <td class="px-3 py-2 text-right tabular-nums">{{ fmtInt(row.completion_tokens) }}</td>
            <td class="px-3 py-2 text-right tabular-nums">{{ fmtInt(row.cost_micro) }}</td>
            <td class="px-3 py-2">
              <div class="flex flex-wrap gap-1">
                <KunChip
                  v-for="s in statusChips(row)"
                  :key="s.key"
                  :color="s.color"
                  variant="flat"
                  size="xs"
                >
                  {{ s.label }} {{ fmtInt(s.count) }}
                </KunChip>
              </div>
            </td>
          </tr>
          <tr v-if="!props.rows.length">
            <td colspan="8" class="text-default-400 px-3 py-10 text-center">
              {{ props.loading ? '加载中…' : '窗口内没有用量记录' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
