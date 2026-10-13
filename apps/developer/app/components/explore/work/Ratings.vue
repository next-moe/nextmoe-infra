<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { EXPLORE_RATING_ORDER, EXPLORE_RATING_SCALE } from '~/constants/explore'
import type { ExploreRating } from '~~/shared/types/explore'

const props = defineProps<{ ratings: ExploreRating[] }>()

const cards = computed(() =>
  [...props.ratings]
    .sort(
      (a, b) =>
        EXPLORE_RATING_ORDER.indexOf(a.source) -
        EXPLORE_RATING_ORDER.indexOf(b.source)
    )
    .map((r) => {
      const scale = EXPLORE_RATING_SCALE[r.source] ?? 10
      const counts = new Map(
        (r.distribution ?? []).map((b) => [b.score, b.count])
      )
      const domain =
        scale === 100
          ? [...counts.keys()].sort((a, b) => a - b)
          : Array.from({ length: scale }, (_, i) => i + 1)
      const bars = domain.map((score) => ({
        score,
        count: counts.get(score) ?? 0
      }))
      const peak = Math.max(1, ...bars.map((b) => b.count))
      return {
        source: r.source,
        score: scale === 100 ? Math.round(r.score) : r.score.toFixed(2),
        scale,
        votes: r.vote_count ?? 0,
        rank: r.rank ?? null,
        bars: bars.map((b) => ({ ...b, height: (b.count / peak) * 100 }))
      }
    })
)
</script>

<template>
  <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
    <div
      v-for="c in cards"
      :key="c.source"
      class="border-default-200 bg-content1 flex gap-4 rounded-xl border p-4"
    >
      <div class="w-24 shrink-0">
        <p class="text-default-500 text-xs font-medium">
          {{ vocabLabel('sources', c.source) }}
        </p>
        <p class="mt-1 flex items-baseline gap-1">
          <span class="text-foreground text-3xl font-bold tabular-nums">
            {{ c.score }}
          </span>
          <span class="text-default-400 text-xs">/{{ c.scale }}</span>
        </p>
        <p class="text-default-400 mt-1 text-xs tabular-nums">
          {{ c.votes.toLocaleString() }} 人
        </p>
        <p v-if="c.rank" class="text-default-400 text-xs tabular-nums">
          排名 #{{ c.rank.toLocaleString() }}
        </p>
      </div>

      <div
        v-if="c.bars.length"
        class="flex min-w-0 flex-1 flex-col justify-end"
      >
        <div class="flex h-16 items-end gap-0.5">
          <div
            v-for="b in c.bars"
            :key="b.score"
            :title="`${b.score} 分：${b.count.toLocaleString()} 票`"
            class="flex h-full flex-1 items-end"
          >
            <div
              :class="
                cn(
                  'w-full rounded-t-sm',
                  b.count ? 'bg-primary' : 'bg-default-200'
                )
              "
              :style="{ height: b.count ? `${Math.max(b.height, 4)}%` : '2px' }"
            />
          </div>
        </div>
        <div class="mt-1 flex gap-0.5">
          <span
            v-for="b in c.bars"
            :key="b.score"
            class="text-default-400 flex-1 text-center text-[10px] tabular-nums"
          >
            {{ b.score }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>
