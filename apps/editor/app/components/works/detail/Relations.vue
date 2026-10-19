<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'

const props = defineProps<{ work: CatalogWork }>()

const relations = computed(() => props.work.relations ?? [])
</script>

<template>
  <WorksDetailSection
    v-if="relations.length"
    title="关联作品"
    :count="relations.length"
  >
    <ul class="grid gap-3 sm:grid-cols-2">
      <li
        v-for="relation in relations"
        :key="`${relation.relation_type}-${relation.work.id}`"
      >
        <NuxtLink
          :to="`/works/${relation.work.id}`"
          class="border-default-200 hover:border-primary-200 flex items-center gap-3 rounded-lg border p-2 transition-colors"
        >
          <div class="bg-default-100 w-12 shrink-0 overflow-hidden rounded-md">
            <KunImage
              v-if="relation.work.cover?.url"
              :src="relation.work.cover.url"
              alt=""
              aspect-ratio="3 / 4"
              object-fit="cover"
            />
            <div v-else class="aspect-[3/4]" />
          </div>
          <div class="min-w-0">
            <p class="text-primary text-xs">{{ relation.phrase }}</p>
            <p class="truncate text-sm font-medium">
              {{ relation.work.display_name }}
            </p>
          </div>
        </NuxtLink>
      </li>
    </ul>
  </WorksDetailSection>
  <KunNull v-else description="没有关联作品" />
</template>
