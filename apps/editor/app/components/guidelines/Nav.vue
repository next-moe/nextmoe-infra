<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { GUIDELINE_CATEGORIES } from '~/constants/guidelines'
import { GUIDELINE_DOCS } from '~/generated/guidelines'

const route = useRoute()

const groups = computed(() =>
  GUIDELINE_CATEGORIES.map((category) => ({
    ...category,
    docs: GUIDELINE_DOCS.filter((doc) => doc.category === category.key)
  })).filter((group) => group.docs.length)
)

const isCurrent = (slug: string | null) =>
  slug === null
    ? route.path === '/guidelines'
    : route.path === `/guidelines/${slug}`
</script>

<template>
  <nav class="h-full overflow-y-auto px-3 py-4" aria-label="编辑规范目录">
    <NuxtLink
      to="/guidelines"
      :class="
        cn(
          'mb-4 flex h-9 items-center gap-2 rounded-lg px-2.5 text-sm transition-colors',
          isCurrent(null)
            ? 'bg-primary-50 text-primary-700 font-medium'
            : 'text-default-600 hover:bg-default-100 hover:text-foreground'
        )
      "
    >
      <KunIcon name="lucide:book-open" class="size-4" />
      规范总览
    </NuxtLink>
    <div v-for="group in groups" :key="group.key" class="mb-4">
      <p class="text-default-400 px-2.5 pb-1 text-xs font-medium">
        {{ group.label }}
      </p>
      <ul class="space-y-0.5">
        <li v-for="doc in group.docs" :key="doc.slug">
          <NuxtLink
            :to="`/guidelines/${doc.slug}`"
            :aria-current="isCurrent(doc.slug) ? 'page' : undefined"
            :class="
              cn(
                'flex min-h-9 items-center gap-2 rounded-lg px-2.5 py-1.5 text-sm transition-colors',
                isCurrent(doc.slug)
                  ? 'bg-primary-50 text-primary-700 font-medium'
                  : 'text-default-600 hover:bg-default-100 hover:text-foreground'
              )
            "
          >
            <KunIcon :name="doc.icon" class="size-4 shrink-0" />
            <span class="min-w-0 leading-5">{{ doc.title }}</span>
          </NuxtLink>
        </li>
      </ul>
    </div>
  </nav>
</template>
