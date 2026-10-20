<script setup lang="ts">
import { CREATABLE_FAMILIES } from '~/constants/creation'
import { ENTITY_META } from '~/constants/editEntities'

const entries = [
  ENTITY_META.work,
  ...CREATABLE_FAMILIES.map((f) => ENTITY_META[f])
]
</script>

<template>
  <KunPopover position="bottom-end" aria-label="新建条目">
    <template #trigger>
      <button
        type="button"
        class="hover:bg-default-100 text-default-600 flex size-8 items-center justify-center rounded-lg transition-colors"
        aria-label="新建条目"
      >
        <KunIcon name="lucide:plus" class="size-5" />
      </button>
    </template>

    <div class="w-48 p-2">
      <p class="text-default-400 px-2 pb-1 text-xs">新建条目</p>
      <NuxtLink
        v-for="meta in entries"
        :key="meta.family"
        :to="`/new/${meta.family}`"
        class="text-default-600 hover:bg-default-100 hover:text-foreground flex items-center gap-3 rounded-lg px-2 py-2 text-sm transition-colors"
      >
        <KunIcon :name="meta.icon" class="size-4" />
        {{ meta.label }}
      </NuxtLink>
    </div>
  </KunPopover>
</template>
