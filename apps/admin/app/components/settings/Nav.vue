<script setup lang="ts">
import { SETTINGS_NAV_TABS } from '~/constants/settings-env'

defineProps<{ title: string; description: string }>()

const route = useRoute()
const tab = computed({
  get: () => route.path.replace(/\/$/, '') || '/settings',
  set: (path: string) => navigateTo(path)
})
</script>

<template>
  <div class="space-y-4">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between"
    >
      <div>
        <h1 class="text-foreground text-2xl font-bold">{{ title }}</h1>
        <p class="text-default-500 mt-1">{{ description }}</p>
      </div>
      <slot name="actions" />
    </div>
    <KunTab v-model="tab" :items="SETTINGS_NAV_TABS" variant="underlined" />
  </div>
</template>
