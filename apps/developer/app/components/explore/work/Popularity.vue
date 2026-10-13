<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import {
  EXPLORE_POPULARITY_LABEL,
  EXPLORE_POPULARITY_TONE
} from '~/constants/explore'
import type { ExplorePopularity } from '~~/shared/types/explore'

const props = defineProps<{ popularity: ExplorePopularity[] }>()

const groups = computed(() => {
  const by = new Map<string, ExplorePopularity[]>()
  for (const p of props.popularity) {
    if (p.value <= 0) continue
    by.set(p.source, [...(by.get(p.source) ?? []), p])
  }
  return [...by.entries()].map(([source, rows]) => {
    const total = rows.reduce((n, r) => n + r.value, 0)
    return {
      source,
      total,
      rows: rows.map((r) => ({
        metric: r.metric,
        label: EXPLORE_POPULARITY_LABEL[r.metric] ?? r.metric,
        value: r.value,
        share: (r.value / total) * 100,
        tone: EXPLORE_POPULARITY_TONE[r.metric] ?? 'bg-default-400'
      }))
    }
  })
})
</script>

<template>
  <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
    <div
      v-for="g in groups"
      :key="g.source"
      :class="
        cn(
          'border-default-200 bg-content1 rounded-xl border p-4',
          g.rows.length > 2 && 'sm:col-span-2'
        )
      "
    >
      <p class="text-default-500 text-xs font-medium">
        {{ vocabLabel('sources', g.source) }}
      </p>

      <template v-if="g.rows.length > 2">
        <div class="mt-3 flex h-2.5 gap-0.5 overflow-hidden rounded-full">
          <div
            v-for="r in g.rows"
            :key="r.metric"
            :class="r.tone"
            :style="{ width: `${r.share}%` }"
          />
        </div>
        <div class="mt-3 flex flex-wrap gap-x-5 gap-y-2">
          <div
            v-for="r in g.rows"
            :key="r.metric"
            class="flex items-center gap-2"
          >
            <span :class="cn('size-2 rounded-full', r.tone)" />
            <span class="text-default-500 text-xs">{{ r.label }}</span>
            <span class="text-foreground text-sm font-semibold tabular-nums">
              {{ r.value.toLocaleString() }}
            </span>
          </div>
        </div>
      </template>

      <div v-else class="mt-1 flex flex-wrap gap-x-6 gap-y-1">
        <p v-for="r in g.rows" :key="r.metric">
          <span class="text-foreground text-2xl font-bold tabular-nums">
            {{ r.value.toLocaleString() }}
          </span>
          <span class="text-default-400 ml-1 text-xs">{{ r.label }}</span>
        </p>
      </div>
    </div>
  </div>
</template>
