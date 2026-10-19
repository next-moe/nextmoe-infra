<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { EDITOR_NAV } from '~/constants/navigation'

const props = withDefaults(defineProps<{ collapsed?: boolean }>(), {
  collapsed: false
})

const { isReviewer } = useEditorSession()
const route = useRoute()

const groups = computed(() =>
  EDITOR_NAV.map((group) => ({
    ...group,
    items: group.items.filter((item) => !item.reviewerOnly || isReviewer.value)
  })).filter((group) => group.items.length > 0)
)

const isActive = (to: string) =>
  to === '/'
    ? route.path === '/'
    : route.path === to || route.path.startsWith(`${to}/`)
</script>

<template>
  <nav class="space-y-4 overflow-y-auto px-2 py-3" aria-label="编辑台导航">
    <div v-for="(group, index) in groups" :key="group.label ?? index">
      <p
        v-if="group.label && !props.collapsed"
        class="text-default-400 px-3 pb-1 text-xs font-medium"
      >
        {{ group.label }}
      </p>
      <KunDivider v-else-if="group.label" class-name="mx-2 mb-2 w-auto" />
      <ul class="space-y-0.5">
        <li v-for="item in group.items" :key="item.to">
          <NuxtLink
            :to="item.to"
            :title="props.collapsed ? item.label : undefined"
            :class="
              cn(
                'flex h-9 items-center gap-3 rounded-lg px-3 text-sm transition-colors',
                isActive(item.to)
                  ? 'bg-primary-50 text-primary font-medium'
                  : 'text-default-600 hover:bg-default-100 hover:text-foreground',
                props.collapsed && 'justify-center px-0'
              )
            "
          >
            <KunIcon :name="item.icon" class="size-4 shrink-0" />
            <template v-if="!props.collapsed">
              <span class="min-w-0 flex-1 truncate">{{ item.label }}</span>
              <span v-if="!item.ready" class="text-default-300 text-xs"
                >筹备中</span
              >
            </template>
          </NuxtLink>
        </li>
      </ul>
    </div>
  </nav>
</template>
