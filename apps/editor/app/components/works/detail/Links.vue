<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'

const props = defineProps<{ work: CatalogWork }>()

const links = computed(() => props.work.links ?? [])
</script>

<template>
  <WorksDetailSection
    v-if="links.length"
    title="外部链接"
    :count="links.length"
  >
    <ul class="divide-default-200 divide-y text-sm">
      <li
        v-for="link in links"
        :key="link.url"
        class="flex items-center gap-3 py-2"
      >
        <KunChip
          size="xs"
          :variant="link.is_identity ? 'solid' : 'flat'"
          :color="link.is_identity ? 'primary' : 'default'"
        >
          {{ link.source }}
        </KunChip>
        <a
          :href="link.url"
          target="_blank"
          rel="noopener noreferrer"
          class="hover:text-primary min-w-0 flex-1 truncate"
        >
          {{ link.url }}
        </a>
        <span v-if="link.is_identity" class="text-default-400 shrink-0 text-xs"
          >身份锚点</span
        >
      </li>
    </ul>
  </WorksDetailSection>
  <KunNull v-else description="暂无外部链接" />
</template>
