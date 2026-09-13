<script setup lang="ts">
import { QUICK_ACTIONS } from '~/constants/dashboard'
</script>

<template>
  <KunCard content-class="justify-start gap-0" class-name="p-6">
    <h2 class="mb-4 text-lg font-semibold text-foreground">
      快捷操作
    </h2>
    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <NuxtLink
        v-for="action in QUICK_ACTIONS"
        :key="action.to"
        :to="action.to"
        class="flex items-center gap-3 rounded-lg border border-default-200 p-4 transition-colors hover:bg-default-100"
      >
        <KunIcon :name="action.icon" :class="`size-5 ${action.color}`" />
        <span class="text-foreground">{{ action.label }}</span>
      </NuxtLink>
    </div>
  </KunCard>
</template>
