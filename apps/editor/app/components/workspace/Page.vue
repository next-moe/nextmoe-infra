<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WorkspaceSectionDef } from '~/constants/workspace'

const props = withDefaults(
  defineProps<{
    section: WorkspaceSectionDef
    description?: string
    count?: number | null
    wide?: boolean
  }>(),
  { description: '', count: null, wide: false }
)

const nav = useWorkspaceNav()
</script>

<template>
  <div class="h-full overflow-y-auto">
    <div
      :class="
        cn(
          'mx-auto space-y-5 px-4 py-5 md:px-8 md:py-6',
          props.wide ? 'max-w-7xl' : 'max-w-5xl'
        )
      "
    >
      <WorkspaceSectionHeader
        :title="props.section.label"
        :description="props.description"
        :count="props.count"
        :changes="nav.changesBySection.value[props.section.key] ?? 0"
      >
        <slot name="actions" />
      </WorkspaceSectionHeader>
      <slot />
    </div>
  </div>
</template>
