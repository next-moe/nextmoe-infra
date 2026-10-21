<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { GuidelineHeading } from '#shared/types/guidelines'

const props = defineProps<{ headings: GuidelineHeading[] }>()

const active = ref('')
let observer: IntersectionObserver | null = null

const observe = () => {
  observer?.disconnect()
  observer = new IntersectionObserver(
    (entries) => {
      const visible = entries
        .filter((entry) => entry.isIntersecting)
        .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
      if (visible[0]) {
        active.value = visible[0].target.id
      }
    },
    { rootMargin: '-64px 0px -70% 0px' }
  )
  for (const heading of props.headings) {
    const element = document.getElementById(heading.id)
    if (element) {
      observer.observe(element)
    }
  }
}

onMounted(() => nextTick(observe))
watch(
  () => props.headings,
  () => nextTick(observe)
)
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <nav v-if="props.headings.length" aria-label="本页目录" class="text-sm">
    <p class="text-default-400 mb-2 text-xs font-medium">本页内容</p>
    <ul class="border-default-200 space-y-1 border-l">
      <li v-for="heading in props.headings" :key="heading.id">
        <a
          :href="`#${heading.id}`"
          :class="
            cn(
              '-ml-px block border-l py-1 leading-5 transition-colors',
              heading.depth === 3 ? 'pl-6' : 'pl-3',
              active === heading.id
                ? 'border-primary-500 text-primary-600'
                : 'text-default-500 hover:text-foreground border-transparent'
            )
          "
        >
          {{ heading.text }}
        </a>
      </li>
    </ul>
  </nav>
</template>
