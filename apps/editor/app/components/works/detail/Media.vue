<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'

const props = defineProps<{ work: CatalogWork }>()

const covers = computed(() => props.work.covers ?? [])
const screenshots = computed(() => props.work.screenshots ?? [])

const lightboxOpen = ref(false)
const lightboxIndex = ref(0)
const lightboxImages = computed(() =>
  [...covers.value, ...screenshots.value].map((image) => ({
    src: image.url,
    alt: ''
  }))
)

const openLightbox = (index: number) => {
  lightboxIndex.value = index
  lightboxOpen.value = true
}
</script>

<template>
  <div class="space-y-4">
    <WorksDetailSection title="封面" :count="covers.length">
      <div
        v-if="covers.length"
        class="grid grid-cols-3 gap-3 sm:grid-cols-4 lg:grid-cols-6"
      >
        <button
          v-for="(cover, index) in covers"
          :key="cover.id"
          type="button"
          class="group text-left"
          @click="openLightbox(index)"
        >
          <div class="bg-default-100 overflow-hidden rounded-lg">
            <KunImage
              :src="cover.url"
              :thumbhash="cover.thumbhash ?? undefined"
              alt=""
              aspect-ratio="3 / 4"
              object-fit="cover"
            />
          </div>
          <p class="text-default-400 mt-1 truncate text-xs">
            {{ cover.source }}
            <template v-if="cover.portrait_pinned"> · 已钉选</template>
          </p>
        </button>
      </div>
      <KunNull v-else description="暂无封面" :is-show-sticker="false" />
    </WorksDetailSection>

    <WorksDetailSection title="截图" :count="screenshots.length">
      <div
        v-if="screenshots.length"
        class="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-4"
      >
        <button
          v-for="(shot, index) in screenshots"
          :key="shot.hash"
          type="button"
          class="bg-default-100 overflow-hidden rounded-lg"
          @click="openLightbox(covers.length + index)"
        >
          <KunImage
            :src="shot.url"
            :thumbhash="shot.thumbhash ?? undefined"
            :alt="shot.caption ?? ''"
            aspect-ratio="16 / 9"
            object-fit="cover"
          />
        </button>
      </div>
      <KunNull v-else description="暂无截图" :is-show-sticker="false" />
    </WorksDetailSection>

    <KunLightbox
      v-model:is-open="lightboxOpen"
      :images="lightboxImages"
      :initial-index="lightboxIndex"
    />
  </div>
</template>
