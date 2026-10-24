<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'
import { CONTENT_RATING, MEDIUM_LABELS } from '~/constants/catalog'

const props = defineProps<{ work: CatalogWork }>()

const preferences = useEditorPreferences()

const rating = computed(() => CONTENT_RATING[props.work.content_rating])
const name = computed(
  () => preferences.nameOf(props.work) || `#${props.work.id}`
)
</script>

<template>
  <NuxtLink
    :to="`/works/${props.work.id}`"
    class="group border-default-200 bg-content1 hover:border-primary-200 flex flex-col overflow-hidden rounded-xl border transition-colors"
  >
    <div class="bg-default-100 relative aspect-[3/4]">
      <KunImage
        v-if="props.work.cover?.url"
        :src="props.work.cover.url"
        :thumbhash="props.work.cover.thumbhash ?? undefined"
        alt=""
        aspect-ratio="3 / 4"
        object-fit="cover"
        class-name="size-full"
      />
      <div
        v-else
        class="text-default-300 flex size-full items-center justify-center"
      >
        <KunIcon name="lucide:image-off" class="size-8" />
      </div>
      <KunChip
        v-if="rating && props.work.content_rating !== 'all_ages'"
        size="xs"
        variant="solid"
        :color="rating.color"
        class-name="absolute top-2 left-2"
      >
        {{ rating.label }}
      </KunChip>
    </div>
    <div class="space-y-1 p-3">
      <p
        class="group-hover:text-primary line-clamp-2 text-sm font-medium transition-colors"
      >
        {{ name }}
      </p>
      <p class="text-default-400 text-xs">
        {{ labelOf(MEDIUM_LABELS, props.work.medium) }}
        <template v-if="props.work.release_date">
          ·
          {{
            formatReleaseDate(
              props.work.release_date,
              props.work.release_date_precision
            )
          }}
        </template>
      </p>
    </div>
  </NuxtLink>
</template>
