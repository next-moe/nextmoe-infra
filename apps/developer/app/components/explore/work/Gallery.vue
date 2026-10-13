<script setup lang="ts">
import { EXPLORE_COVER_KIND_LABEL } from '~/constants/explore'
import type { ExploreImage, ExploreWork } from '~~/shared/types/explore'

const props = defineProps<{ work: ExploreWork; nsfw: boolean }>()
const emit = defineEmits<{ nsfw: [] }>()

const split = <T extends ExploreImage>(list: T[]) => ({
  shown: list.filter((i) => isImageShown(i, props.nsfw)),
  hidden: list.filter((i) => !isImageShown(i, props.nsfw)).length
})

const screenshots = computed(() => split(props.work.screenshots ?? []))
const covers = computed(() => split(props.work.covers ?? []))
const hidden = computed(() => screenshots.value.hidden + covers.value.hidden)

const ratio = (img: ExploreImage) =>
  img.width && img.height ? `${img.width} / ${img.height}` : '3 / 4'
</script>

<template>
  <div class="space-y-8">
    <KunInfo
      v-if="hidden && !nsfw"
      color="default"
      variant="flat"
      icon="lucide:eye-off"
      :title="`${hidden} 张图片含性或暴力内容，默认不显示`"
    >
      <KunButton size="xs" color="primary" variant="flat" @click="emit('nsfw')">
        显示全部图片
      </KunButton>
    </KunInfo>

    <KunLightboxGallery>
      <div class="space-y-8">
        <section v-if="screenshots.shown.length" class="space-y-3">
          <h3 class="text-foreground text-base font-semibold">
            游戏截图
            <span class="text-default-400 ml-1 text-sm font-normal">
              {{ screenshots.shown.length }}
            </span>
          </h3>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
            <KunLightboxGalleryItem
              v-for="(s, i) in screenshots.shown"
              :key="s.url"
              as="div"
              :src="s.url"
              :alt="`${work.display_name} 截图 ${i + 1}`"
              class="group border-default-200 bg-default-100 overflow-hidden rounded-xl border"
            >
              <KunImageNative
                :src="s.url"
                :alt="`截图 ${i + 1}`"
                loading="lazy"
                class-name="aspect-video w-full object-cover transition-transform duration-300 group-hover:scale-[1.03]"
              />
            </KunLightboxGalleryItem>
          </div>
        </section>

        <section v-if="covers.shown.length" class="space-y-3">
          <h3 class="text-foreground text-base font-semibold">
            封面与包装
            <span class="text-default-400 ml-1 text-sm font-normal">
              {{ covers.shown.length }}
            </span>
          </h3>
          <div class="columns-2 gap-3 sm:columns-3 xl:columns-4">
            <KunLightboxGalleryItem
              v-for="c in covers.shown"
              :key="c.url"
              as="div"
              :src="c.url"
              :alt="`${work.display_name} ${EXPLORE_COVER_KIND_LABEL[c.cover_kind ?? ''] ?? '封面'}`"
              class="border-default-200 bg-default-100 relative mb-3 break-inside-avoid overflow-hidden rounded-xl border"
            >
              <div :style="{ aspectRatio: ratio(c) }">
                <KunImageNative
                  :src="c.url"
                  :alt="EXPLORE_COVER_KIND_LABEL[c.cover_kind ?? ''] ?? '封面'"
                  loading="lazy"
                  class-name="h-full w-full object-cover"
                />
              </div>
              <div class="absolute bottom-2 left-2 flex flex-wrap gap-1">
                <KunChip
                  v-if="c.cover_kind"
                  color="default"
                  variant="solid"
                  size="xs"
                >
                  {{ EXPLORE_COVER_KIND_LABEL[c.cover_kind] ?? c.cover_kind }}
                </KunChip>
                <KunChip
                  v-if="c.sexual && c.sexual !== 'safe'"
                  color="danger"
                  variant="solid"
                  size="xs"
                >
                  {{ vocabLabel('sexual', c.sexual) }}
                </KunChip>
              </div>
            </KunLightboxGalleryItem>
          </div>
        </section>
      </div>
    </KunLightboxGallery>
  </div>
</template>
