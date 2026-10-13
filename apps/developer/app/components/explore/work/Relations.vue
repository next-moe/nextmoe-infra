<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { EXPLORE_MEDIUM_ICON } from '~/constants/explore'
import type { ExploreWork } from '~~/shared/types/explore'

const props = defineProps<{ work: ExploreWork; nsfw: boolean }>()

const NuxtLink = resolveComponent('NuxtLink')

const relations = computed(() =>
  (props.work.relations ?? []).map((rel) => {
    const browsable = isBrowsableWork(rel.work)
    const zh = zhLocalized(rel.work.localized)
    return {
      ...rel,
      browsable,
      zhName: zh && zh.value !== rel.work.display_name ? zh.value : null,
      cover:
        rel.work.cover && isImageShown(rel.work.cover, props.nsfw)
          ? rel.work.cover
          : null
    }
  })
)

const offCatalog = computed(
  () => relations.value.filter((r) => !r.browsable).length
)
</script>

<template>
  <div class="space-y-8">
    <section v-if="(work.series ?? []).length" class="space-y-3">
      <h3 class="text-foreground text-base font-semibold">所属系列</h3>
      <div class="flex flex-wrap gap-3">
        <div
          v-for="s in work.series"
          :key="s.id"
          class="border-default-200 bg-content1 flex items-center gap-3 rounded-xl border px-4 py-3"
        >
          <KunIcon name="lucide:library" class="text-secondary size-5" />
          <div>
            <p class="text-foreground text-sm font-medium">
              {{ s.display_name }}
            </p>
            <p v-if="s.member_count" class="text-default-400 text-xs">
              系列共 {{ s.member_count }} 部作品
            </p>
          </div>
        </div>
      </div>
    </section>

    <section v-if="relations.length" class="space-y-3">
      <h3 class="text-foreground text-base font-semibold">关联作品</h3>
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <component
          :is="rel.browsable ? NuxtLink : 'div'"
          v-for="(rel, i) in relations"
          :key="`rel-${i}`"
          :to="rel.browsable ? exploreWorkPath(rel.work.id, nsfw) : undefined"
          :class="
            cn(
              'group border-default-200 bg-content1 flex gap-3 rounded-xl border p-3',
              rel.browsable && 'hover:border-primary transition-colors'
            )
          "
        >
          <div
            class="bg-default-100 text-default-300 flex aspect-[3/4] w-14 shrink-0 items-center justify-center overflow-hidden rounded-lg"
          >
            <KunImageNative
              v-if="rel.cover"
              :src="rel.cover.url"
              :alt="rel.work.display_name"
              loading="lazy"
              class-name="h-full w-full object-cover"
            />
            <KunIcon
              v-else
              :name="
                EXPLORE_MEDIUM_ICON[rel.work.medium ?? ''] ?? 'lucide:file'
              "
              class="size-6"
            />
          </div>

          <div class="min-w-0 flex-1 space-y-1">
            <div class="flex flex-wrap items-center gap-1.5">
              <KunChip color="secondary" variant="flat" size="xs">
                {{
                  rel.phrase ?? vocabLabel('relation_types', rel.relation_type)
                }}
              </KunChip>
              <KunChip
                v-if="!rel.browsable"
                color="default"
                variant="flat"
                size="xs"
              >
                {{ vocabLabel('medium', rel.work.medium) }}
              </KunChip>
              <KunChip
                v-if="rel.work.content_rating === 'r18'"
                color="danger"
                variant="flat"
                size="xs"
              >
                R18
              </KunChip>
            </div>
            <p
              :class="
                cn(
                  'text-foreground truncate text-sm font-medium',
                  rel.browsable && 'group-hover:text-primary'
                )
              "
            >
              {{ rel.work.display_name ?? `#${rel.work.id}` }}
            </p>
            <p v-if="rel.zhName" class="text-default-500 truncate text-xs">
              {{ rel.zhName }}
            </p>
            <p v-if="rel.work.release_date" class="text-default-400 text-xs">
              {{ rel.work.release_date }}
            </p>
          </div>

          <KunIcon
            v-if="rel.browsable"
            name="lucide:arrow-right"
            class="text-default-400 size-4 shrink-0 self-center transition-transform group-hover:translate-x-0.5"
          />
        </component>
      </div>
      <p v-if="offCatalog" class="text-default-400 text-xs">
        标了媒介的 {{ offCatalog }} 部是动画、漫画等非 galgame 作品：开放 API
        只提供 galgame 的作品详情，这里只列出关联关系，不提供跳转。
      </p>
    </section>
  </div>
</template>
