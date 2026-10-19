<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'
import { SPOILER_LABELS } from '~/constants/catalog'

const props = defineProps<{ work: CatalogWork }>()

const groups = computed(() => {
  const tags = props.work.tags ?? []
  return [
    { title: '内容标签', items: tags.filter((tag) => tag.tag_kind !== 'meta') },
    {
      title: '元信息标签',
      items: tags.filter((tag) => tag.tag_kind === 'meta')
    }
  ].filter((group) => group.items.length > 0)
})
</script>

<template>
  <div v-if="groups.length" class="space-y-4">
    <WorksDetailSection
      v-for="group in groups"
      :key="group.title"
      :title="group.title"
      :count="group.items.length"
    >
      <div class="flex flex-wrap gap-1.5">
        <NuxtLink
          v-for="tag in group.items"
          :key="tag.id ?? tag.display_name"
          :to="tag.id ? `/tags/${tag.id}` : undefined"
        >
          <KunChip
            size="sm"
            :variant="tag.spoiler === 'none' ? 'flat' : 'bordered'"
            :color="
              tag.is_sexual
                ? 'danger'
                : tag.spoiler === 'none'
                  ? 'default'
                  : 'warning'
            "
          >
            {{ tag.display_name }}
            <span v-if="tag.spoiler !== 'none'" class="text-xs opacity-70">
              · {{ labelOf(SPOILER_LABELS, tag.spoiler) }}
            </span>
          </KunChip>
        </NuxtLink>
      </div>
    </WorksDetailSection>
  </div>
  <KunNull v-else description="暂无标签" />
</template>
