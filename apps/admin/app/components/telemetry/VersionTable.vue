<script setup lang="ts">
import { formatTelemetryRate, formatTelemetryMs } from '~/constants/telemetry'
import type { TelemetryDailyMetric } from '~~/shared/types/telemetry'

const props = defineProps<{
  rows: TelemetryDailyMetric[]
  minSessions: number
  crashThreshold: number
  anrThreshold: number
}>()

type VersionRow = {
  version: string
  sessions: number
  crashed: number
  anr: number
  unhandled: number
  ttidP50: number | null
  ttidP90: number | null
  jankOver: number
  jankTotal: number
  firstDay: string
  lastDay: string
}

const weightedMs = (
  list: TelemetryDailyMetric[],
  pick: (r: TelemetryDailyMetric) => number | null
) => {
  let num = 0
  let den = 0
  for (const r of list) {
    const v = pick(r)
    if (v == null || r.startup_count <= 0) continue
    num += v * r.startup_count
    den += r.startup_count
  }
  return den === 0 ? null : num / den
}

const versions = computed(() => {
  const map = new Map<string, TelemetryDailyMetric[]>()
  for (const r of props.rows) {
    const list = map.get(r.service_version) ?? []
    list.push(r)
    map.set(r.service_version, list)
  }
  const out: VersionRow[] = []
  for (const [version, list] of map) {
    const days = list.map((r) => r.day).sort()
    out.push({
      version,
      sessions: list.reduce((s, r) => s + r.sessions, 0),
      crashed: list.reduce((s, r) => s + r.crashed_sessions, 0),
      anr: list.reduce((s, r) => s + r.anr_sessions, 0),
      unhandled: list.reduce((s, r) => s + r.unhandled_sessions, 0),
      ttidP50: weightedMs(list, (r) => r.ttid_p50_ms),
      ttidP90: weightedMs(list, (r) => r.ttid_p90_ms),
      jankOver: list.reduce((s, r) => s + r.jank_frames_over, 0),
      jankTotal: list.reduce((s, r) => s + r.jank_frames_total, 0),
      firstDay: days[0] ?? '',
      lastDay: days[days.length - 1] ?? ''
    })
  }
  out.sort((a, b) => b.sessions - a.sessions)
  return out
})

const crashRate = (row: VersionRow) =>
  row.sessions === 0 ? null : row.crashed / row.sessions
const anrRate = (row: VersionRow) =>
  row.sessions === 0 ? null : row.anr / row.sessions
const unhandledRate = (row: VersionRow) =>
  row.sessions === 0 ? null : row.unhandled / row.sessions
const jankRatio = (row: VersionRow) =>
  row.jankTotal === 0 ? null : row.jankOver / row.jankTotal

const sampleShort = (row: VersionRow) => row.sessions < props.minSessions

const overCrash = (row: VersionRow) => {
  const r = crashRate(row)
  return r != null && !sampleShort(row) && r > props.crashThreshold
}
const overAnr = (row: VersionRow) => {
  const r = anrRate(row)
  return r != null && !sampleShort(row) && r > props.anrThreshold
}

const fmtInt = (n: number) => n.toLocaleString('zh-CN')
const pctLabel = (n: number) => `${(n * 100).toFixed(2)}%`
</script>

<template>
  <div class="space-y-2">
    <h2 class="text-foreground text-lg font-semibold">版本</h2>
    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[64rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">版本</th>
            <th class="px-3 py-2 text-right font-medium">会话数</th>
            <th class="px-3 py-2 text-right font-medium">崩溃率</th>
            <th class="px-3 py-2 text-right font-medium">ANR 率</th>
            <th class="px-3 py-2 text-right font-medium">未处理率</th>
            <th class="px-3 py-2 text-right font-medium">TTID P50</th>
            <th class="px-3 py-2 text-right font-medium">TTID P90</th>
            <th class="px-3 py-2 text-right font-medium">卡顿帧占比</th>
            <th class="px-3 py-2 text-left font-medium">首次出现</th>
            <th class="px-3 py-2 text-left font-medium">最近出现</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in versions"
            :key="row.version"
            class="border-default-200 border-t"
          >
            <td class="text-foreground px-3 py-2 font-mono">
              {{ row.version }}
            </td>
            <td class="px-3 py-2 text-right tabular-nums">
              {{ fmtInt(row.sessions) }}
            </td>
            <td class="px-3 py-2 text-right">
              <KunTooltip
                v-if="overCrash(row)"
                :text="`阈值 ${pctLabel(crashThreshold)}`"
                position="top"
              >
                <span class="text-danger-600 tabular-nums">
                  {{ formatTelemetryRate(crashRate(row), row.sessions) }}
                </span>
              </KunTooltip>
              <span
                v-else-if="sampleShort(row)"
                class="text-default-400 tabular-nums"
              >
                {{ formatTelemetryRate(crashRate(row), row.sessions) }}
                <span class="ml-1 text-xs">样本不足</span>
              </span>
              <span v-else class="tabular-nums">
                {{ formatTelemetryRate(crashRate(row), row.sessions) }}
              </span>
            </td>
            <td class="px-3 py-2 text-right">
              <KunTooltip
                v-if="overAnr(row)"
                :text="`阈值 ${pctLabel(anrThreshold)}`"
                position="top"
              >
                <span class="text-danger-600 tabular-nums">
                  {{ formatTelemetryRate(anrRate(row), row.sessions) }}
                </span>
              </KunTooltip>
              <span
                v-else-if="sampleShort(row)"
                class="text-default-400 tabular-nums"
              >
                {{ formatTelemetryRate(anrRate(row), row.sessions) }}
                <span class="ml-1 text-xs">样本不足</span>
              </span>
              <span v-else class="tabular-nums">
                {{ formatTelemetryRate(anrRate(row), row.sessions) }}
              </span>
            </td>
            <td
              class="px-3 py-2 text-right tabular-nums"
              :class="sampleShort(row) ? 'text-default-400' : ''"
            >
              {{ formatTelemetryRate(unhandledRate(row), row.sessions) }}
              <span v-if="sampleShort(row)" class="ml-1 text-xs">样本不足</span>
            </td>
            <td class="px-3 py-2 text-right tabular-nums">
              {{ formatTelemetryMs(row.ttidP50) }}
            </td>
            <td class="px-3 py-2 text-right tabular-nums">
              {{ formatTelemetryMs(row.ttidP90) }}
            </td>
            <td class="px-3 py-2 text-right tabular-nums">
              {{
                row.jankTotal === 0
                  ? '—'
                  : formatTelemetryRate(jankRatio(row), 1)
              }}
            </td>
            <td class="text-default-500 px-3 py-2 tabular-nums">
              {{ row.firstDay }}
            </td>
            <td class="text-default-500 px-3 py-2 tabular-nums">
              {{ row.lastDay }}
            </td>
          </tr>
          <tr v-if="!versions.length">
            <td colspan="10" class="text-default-400 px-3 py-10 text-center">
              窗口内没有版本数据
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
