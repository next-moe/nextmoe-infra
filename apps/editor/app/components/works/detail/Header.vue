<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'
import {
  CONTENT_RATING,
  LANGUAGE_LABELS,
  MEDIUM_LABELS,
  RELEASE_STATUS_LABELS,
  SITE_LABELS
} from '~/constants/catalog'

const props = defineProps<{ work: CatalogWork }>()

const rating = computed(() => CONTENT_RATING[props.work.content_rating])
const identityLinks = computed(() =>
  (props.work.links ?? []).filter((link) => link.is_identity)
)
const release = computed(() =>
  props.work.release_date
    ? formatReleaseDate(
        props.work.release_date,
        props.work.release_date_precision
      )
    : labelOf(RELEASE_STATUS_LABELS, props.work.release_status)
)
</script>

<template>
  <section class="flex flex-col gap-5 sm:flex-row">
    <div
      class="bg-default-100 w-32 shrink-0 overflow-hidden rounded-xl sm:w-40"
    >
      <KunImage
        v-if="props.work.cover?.url"
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
        <KunIcon name="lucide:image-off" class="size-8" />
      </div>
    </div>

    <div class="min-w-0 flex-1 space-y-3">
      <div>
        <h1 class="text-2xl font-bold break-words">
          {{ props.work.display_name }}
        </h1>
        <p v-if="props.work.latin" class="text-default-500 mt-0.5">
          {{ props.work.latin }}
        </p>
      </div>

      <div class="flex flex-wrap items-center gap-1.5">
        <KunChip size="sm" variant="flat">{{
          labelOf(MEDIUM_LABELS, props.work.medium)
        }}</KunChip>
        <KunChip v-if="rating" size="sm" variant="flat" :color="rating.color">
          {{ rating.label }}
        </KunChip>
        <KunChip size="sm" variant="flat">{{
          labelOf(LANGUAGE_LABELS, props.work.olang)
        }}</KunChip>
        <KunChip v-if="release" size="sm" variant="flat">{{ release }}</KunChip>
        <KunChip
          v-if="props.work.claim"
          size="sm"
          variant="flat"
          color="secondary"
        >
          收录于 {{ labelOf(SITE_LABELS, props.work.claim.site) }}
        </KunChip>
      </div>

      <div
        class="text-default-500 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm"
      >
        <span class="inline-flex items-center gap-1">
          条目 ID
          <KunCopy :text="String(props.work.id)" size="xs" variant="light" />
        </span>
        <span>更新于 {{ formatDateTime(props.work.updated_at) }}</span>
      </div>

      <div v-if="identityLinks.length" class="flex flex-wrap gap-1.5">
        <a
          v-for="link in identityLinks"
          :key="link.url"
          :href="link.url"
          target="_blank"
          rel="noopener noreferrer"
          class="border-default-200 text-default-600 hover:border-primary-200 hover:text-primary inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs transition-colors"
        >
          {{ link.source }}
          <KunIcon name="lucide:external-link" class="size-3" />
        </a>
      </div>
    </div>
  </section>
</template>
