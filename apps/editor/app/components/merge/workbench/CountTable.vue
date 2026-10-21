<script setup lang="ts">
import type { CatalogMergePreviewCount } from '#shared/types/catalog'
import { MERGE_COUNT_LABELS, MERGE_TABLE_LABELS } from '~/constants/merge'
import { countAfter } from '~/utils/merge/plan'

const props = defineProps<{ counts: CatalogMergePreviewCount[] }>()

const label = (key: string) => {
  if (MERGE_COUNT_LABELS[key]) {
    return MERGE_COUNT_LABELS[key]
  }
  const [table, column] = key.split('.')
  return `${MERGE_TABLE_LABELS[table!] ?? table}（${column}）`
}
</script>

<template>
  <div class="border-default-200 bg-content1 overflow-x-auto rounded-xl border">
    <table class="w-full min-w-[32rem] text-sm">
      <thead>
        <tr class="border-default-200 text-default-500 border-b text-left">
          <th class="px-4 py-2 font-medium">类别</th>
          <th class="px-4 py-2 text-right font-medium">被并入</th>
          <th class="px-4 py-2 text-right font-medium">保留</th>
          <th class="px-4 py-2 text-right font-medium">两边都有</th>
          <th class="text-primary-700 px-4 py-2 text-right font-medium">
            合并后
          </th>
        </tr>
      </thead>
      <tbody class="divide-default-100 divide-y">
        <tr v-for="count in props.counts" :key="count.key">
          <td class="px-4 py-2">{{ label(count.key) }}</td>
          <td class="px-4 py-2 text-right tabular-nums">
            {{ count.on_source }}
          </td>
          <td class="px-4 py-2 text-right tabular-nums">
            {{ count.on_target }}
          </td>
          <td class="text-default-500 px-4 py-2 text-right tabular-nums">
            {{ count.already_on_target || '—' }}
          </td>
          <td class="px-4 py-2 text-right font-medium tabular-nums">
            {{ countAfter(count) }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
