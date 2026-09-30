<script setup lang="ts">
import type { TelemetryIssue } from '~~/shared/types/telemetry'

defineProps<{
  items: TelemetryIssue[]
  loading: boolean
}>()

const fmtInt = (n: number) => n.toLocaleString('zh-CN')
const openIssue = (id: number) => navigateTo(`/telemetry/issues/${id}`)
</script>

<template>
  <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
    <table class="w-full min-w-[64rem] text-sm">
      <thead class="bg-content2 text-default-500">
        <tr>
          <th class="px-3 py-2 text-left font-medium">类型</th>
          <th class="px-3 py-2 text-left font-medium">标题</th>
          <th class="px-3 py-2 text-left font-medium">Culprit</th>
          <th class="px-3 py-2 text-right font-medium">事件数</th>
          <th class="px-3 py-2 text-right font-medium">会话数</th>
          <th class="px-3 py-2 text-left font-medium">首次版本</th>
          <th class="px-3 py-2 text-left font-medium">最近版本</th>
          <th class="px-3 py-2 text-left font-medium">最近出现</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="row in items"
          :key="row.id"
          class="border-default-200 hover:bg-default-50 cursor-pointer border-t"
          tabindex="0"
          @click="openIssue(row.id)"
          @keydown.enter="openIssue(row.id)"
        >
          <td class="px-3 py-2">
            <div class="flex flex-wrap items-center gap-1">
              <TelemetryKindChip :kind="row.kind" />
              <KunChip
                v-if="row.regressed"
                color="danger"
                variant="flat"
                size="xs"
              >
                回归
              </KunChip>
            </div>
          </td>
          <td class="px-3 py-2">
            <NuxtLink
              :to="`/telemetry/issues/${row.id}`"
              class="text-foreground line-clamp-2 hover:underline"
              :title="row.title"
              @click.stop
            >
              {{ row.title }}
            </NuxtLink>
          </td>
          <td
            class="text-default-500 max-w-[12rem] truncate px-3 py-2 font-mono text-xs"
            :title="row.culprit"
          >
            {{ row.culprit }}
          </td>
          <td class="px-3 py-2 text-right tabular-nums">
            {{ fmtInt(row.events) }}
          </td>
          <td class="px-3 py-2 text-right tabular-nums">
            {{ fmtInt(row.sessions) }}
          </td>
          <td class="px-3 py-2 font-mono text-xs">{{ row.first_version }}</td>
          <td class="px-3 py-2 font-mono text-xs">{{ row.last_version }}</td>
          <td class="text-default-500 px-3 py-2 tabular-nums">
            {{ row.last_seen_day }}
          </td>
        </tr>
        <tr v-if="!items.length">
          <td colspan="8" class="text-default-400 px-3 py-10 text-center">
            {{ loading ? '加载中…' : '窗口内没有匹配的问题' }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
