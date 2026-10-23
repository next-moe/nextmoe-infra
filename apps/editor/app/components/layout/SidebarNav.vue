<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { EDITOR_NAV, currentNavPath } from '~/constants/navigation'

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

const current = computed(() =>
  currentNavPath(
    route.path,
    groups.value.flatMap((group) => group.items)
  )
)
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
        <li
          v-for="item in group.items"
          :key="item.to"
          :class="props.collapsed && 'flex justify-center'"
        >
          <KunTooltip
            v-if="props.collapsed"
            :text="item.label"
            position="right"
          >
            <KunButton
              :href="item.to"
              :variant="current === item.to ? 'flat' : 'light'"
              :color="current === item.to ? 'primary' : 'default'"
              :aria-label="item.label"
              :aria-current="current === item.to ? 'page' : undefined"
              is-icon-only
            >
              <KunIcon :name="item.icon" class="size-4" />
            </KunButton>
          </KunTooltip>
          <KunNavItem
            v-else
            :label="item.label"
            :icon="item.icon"
            :href="item.to"
            :current="current === item.to"
            :class-name="
              cn('h-9 text-sm', current !== item.to && 'text-default-600')
            "
          />
        </li>
      </ul>
    </div>
  </nav>
</template>
