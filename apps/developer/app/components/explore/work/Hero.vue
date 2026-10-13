<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { EXPLORE_RATING_ORDER, EXPLORE_RATING_SCALE } from '~/constants/explore'
import type { ExploreEntityTarget, ExploreWork } from '~~/shared/types/explore'

const props = defineProps<{ work: ExploreWork; nsfw: boolean }>()
const emit = defineEmits<{ open: [target: ExploreEntityTarget] }>()

const shown = <T extends { url: string }>(img?: T | null): T | null =>
  img && isImageShown(img, props.nsfw) ? img : null

const banner = computed(
  () =>
    shown(props.work.banner) ??
    (props.work.screenshots ?? []).find((s) => isImageShown(s, props.nsfw)) ??
    null
)

const cover = computed(
  () =>
    shown(props.work.cover) ??
    (props.work.covers ?? []).find((c) => isImageShown(c, props.nsfw)) ??
    null
)

const zhName = computed(() => {
  const zh = zhLocalized(props.work.localized)
  return zh && zh.value !== props.work.display_name ? zh : null
})

const originalTitle = computed(() => {
  const ja = props.work.titles?.find(
    (t) => t.lang === props.work.olang && t.title_kind === 'official'
  )
  return ja && ja.title !== props.work.display_name ? ja.title : null
})

const RATING_COLOR: Record<string, 'default' | 'warning' | 'danger'> = {
  all_ages: 'default',
  sensitive: 'warning',
  r18: 'danger'
}

const makers = computed(() =>
  (props.work.companies ?? []).filter((c) =>
    ['developer', 'brand', 'circle'].includes(c.attribution_role ?? '')
  )
)

const scores = computed(() =>
  [...(props.work.ratings ?? [])]
    .sort(
      (a, b) =>
        EXPLORE_RATING_ORDER.indexOf(a.source) -
        EXPLORE_RATING_ORDER.indexOf(b.source)
    )
    .map((r) => {
      const scale = EXPLORE_RATING_SCALE[r.source] ?? 10
      return {
        source: r.source,
        score: scale === 100 ? Math.round(r.score) : r.score.toFixed(2),
        scale,
        votes: r.vote_count ?? 0,
        rank: r.rank ?? null
      }
    })
)
</script>

<template>
  <section
    class="border-default-200 bg-content1 overflow-hidden rounded-2xl border"
  >
    <div v-if="banner" class="bg-default-100 h-40 sm:h-56 md:h-72">
      <KunImageNative
        :src="banner.url"
        :alt="work.display_name"
        loading="eager"
        class-name="h-full w-full object-cover"
      />
    </div>

    <div
      class="flex flex-col gap-5 p-5 sm:flex-row sm:items-end md:gap-6 md:p-6"
    >
      <div
        :class="
          cn(
            'border-content1 bg-default-100 relative z-10 w-32 shrink-0 overflow-hidden rounded-xl border-4 shadow-lg sm:w-40 md:w-48',
            banner && '-mt-20 sm:-mt-28 md:-mt-36'
          )
        "
      >
        <KunImageNative
          v-if="cover"
          :src="cover.url"
          :alt="work.display_name"
          loading="eager"
          class-name="aspect-[3/4] w-full object-cover"
        />
        <div
          v-else
          class="text-default-300 flex aspect-[3/4] items-center justify-center"
        >
          <KunIcon name="lucide:gamepad-2" class="size-10" />
        </div>
      </div>

      <div class="min-w-0 flex-1 space-y-3">
        <div class="space-y-1">
          <h1
            class="text-foreground text-2xl font-bold tracking-tight break-words md:text-4xl"
          >
            {{ work.display_name ?? `#${work.id}` }}
          </h1>
          <p
            v-if="zhName"
            class="text-default-500 flex flex-wrap items-center gap-2 text-base md:text-lg"
          >
            {{ zhName.value }}
            <KunChip
              v-if="zhName.is_machine"
              color="warning"
              variant="flat"
              size="xs"
            >
              机翻
            </KunChip>
          </p>
          <p v-if="originalTitle" class="text-default-400 text-sm">
            {{ originalTitle }}
          </p>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <KunChip
            v-if="work.content_rating"
            :color="RATING_COLOR[work.content_rating] ?? 'default'"
            variant="flat"
            size="sm"
          >
            {{ vocabLabel('content_rating', work.content_rating) }}
          </KunChip>
          <KunChip
            v-if="work.release_date"
            color="default"
            variant="flat"
            size="sm"
          >
            <span class="inline-flex items-center gap-1">
              <KunIcon name="lucide:calendar" class="size-3.5" />
              {{ work.release_date }}
              <template v-if="work.release_status">
                · {{ vocabLabel('release_status', work.release_status) }}
              </template>
            </span>
          </KunChip>
          <KunChip v-if="work.olang" color="default" variant="flat" size="sm">
            <span class="inline-flex items-center gap-1">
              <KunIcon name="lucide:languages" class="size-3.5" />
              {{ vocabLabel('olang', work.olang) }}
            </span>
          </KunChip>
          <button
            v-for="c in makers"
            :key="c.id"
            type="button"
            @click="emit('open', { kind: 'companies', id: c.id })"
          >
            <KunChip color="primary" variant="flat" size="sm">
              <span class="inline-flex items-center gap-1">
                <KunIcon name="lucide:building" class="size-3.5" />
                {{ c.display_name }}
              </span>
            </KunChip>
          </button>
        </div>
      </div>
    </div>

    <div
      v-if="scores.length"
      class="border-default-200 -mr-px -mb-px flex flex-wrap border-t"
    >
      <div
        v-for="s in scores"
        :key="s.source"
        class="border-default-200 min-w-36 flex-1 border-r border-b px-5 py-3"
      >
        <p class="text-default-400 text-xs">
          {{ vocabLabel('sources', s.source) }}
        </p>
        <p class="mt-0.5 flex items-baseline gap-1">
          <span class="text-foreground text-2xl font-bold tabular-nums">
            {{ s.score }}
          </span>
          <span class="text-default-400 text-xs">/ {{ s.scale }}</span>
        </p>
        <p class="text-default-400 text-xs tabular-nums">
          {{ s.votes.toLocaleString() }} 人评分
          <template v-if="s.rank">
            · 第 {{ s.rank.toLocaleString() }} 名</template
          >
        </p>
      </div>
    </div>
  </section>
</template>
