<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    title: string
    total?: number | null
    placeholder?: string
    addLabel?: string
    canAdd?: boolean
    adding?: boolean
    filterable?: boolean
  }>(),
  {
    total: null,
    placeholder: '筛选',
    addLabel: '添加',
    canAdd: true,
    adding: false,
    filterable: true
  }
)

const query = defineModel<string>('query', { default: '' })
const emit = defineEmits<{ add: [] }>()
</script>

<template>
  <div class="space-y-2">
    <div class="flex items-center gap-2">
      <h2 class="flex min-w-0 flex-1 items-baseline gap-1.5 font-semibold">
        <span class="truncate">{{ props.title }}</span>
        <span
          v-if="props.total !== null"
          class="text-default-400 text-xs font-normal tabular-nums"
        >
          {{ props.total }}
        </span>
      </h2>
      <slot name="actions" />
      <WorkspaceGuideLink icon-only />
      <WorkspaceAddToggle
        v-if="props.canAdd"
        :model-value="props.adding"
        :label="props.addLabel"
        @update:model-value="emit('add')"
      />
    </div>
    <KunInput
      v-if="props.filterable"
      v-model="query"
      :placeholder="props.placeholder"
      :aria-label="props.placeholder"
      size="sm"
      is-clearable
    >
      <template #prefix>
        <KunIcon name="lucide:search" class="text-default-400 size-4" />
      </template>
    </KunInput>
  </div>
</template>
