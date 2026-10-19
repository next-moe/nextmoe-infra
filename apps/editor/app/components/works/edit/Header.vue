<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'
import { CONTENT_RATING, MEDIUM_LABELS } from '~/constants/catalog'

const props = defineProps<{ workId: string; work: CatalogWork | null }>()

const rating = computed(() =>
  props.work ? CONTENT_RATING[props.work.content_rating] : undefined
)
</script>

<template>
  <header class="flex items-center gap-4">
    <div
      class="bg-default-100 w-16 shrink-0 overflow-hidden rounded-lg sm:w-20"
    >
      <KunImage
        v-if="props.work?.cover?.url"
        :src="props.work.cover.url"
        :thumbhash="props.work.cover.thumbhash ?? undefined"
        :alt="props.work.display_name"
        aspect-ratio="3 / 4"
        object-fit="cover"
        loading="eager"
      />
      <div
        v-else
        class="text-default-300 flex aspect-[3/4] items-center justify-center"
      >
        <KunIcon name="lucide:image-off" class="size-6" />
      </div>
    </div>
    <div class="min-w-0 flex-1">
      <p class="text-default-500 flex items-center gap-1 text-xs">
        <KunIcon name="lucide:pencil" class="size-3.5" />
        编辑作品
      </p>
      <h1 class="truncate text-xl font-bold sm:text-2xl">
        {{ props.work?.display_name ?? `作品 #${props.workId}` }}
      </h1>
      <div
        class="text-default-500 mt-1 flex flex-wrap items-center gap-1.5 text-xs"
      >
        <KunChip v-if="props.work" size="xs" variant="flat">
          {{ labelOf(MEDIUM_LABELS, props.work.medium) }}
        </KunChip>
        <KunChip v-if="rating" size="xs" variant="flat" :color="rating.color">
          {{ rating.label }}
        </KunChip>
        <span class="inline-flex items-center gap-1">
          条目 ID
          <KunCopy :text="props.workId" size="xs" variant="light" />
        </span>
      </div>
    </div>
    <KunButton :href="`/works/${props.workId}`" variant="bordered" size="sm">
      <KunIcon name="lucide:eye" class="size-4" />
      查看资料
    </KunButton>
  </header>
</template>
