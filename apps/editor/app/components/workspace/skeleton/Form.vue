<script setup lang="ts">
const props = withDefaults(defineProps<{ rows?: number }>(), { rows: 5 })
</script>

<template>
  <div class="space-y-4" aria-busy="true" aria-label="正在载入">
    <div class="border-default-200 bg-content1 rounded-xl border px-5">
      <div
        v-for="i in props.rows"
        :key="i"
        class="border-default-100 grid gap-x-6 gap-y-2 border-b py-4 last:border-b-0 sm:grid-cols-[9rem_minmax(0,1fr)]"
      >
        <KunSkeleton variant="text" width="5rem" class-name="text-sm" />
        <KunSkeleton height="2.25rem" rounded="lg" />
      </div>
    </div>
    <div class="border-default-200 bg-content1 space-y-3 rounded-xl border p-5">
      <KunSkeleton variant="text" width="8rem" class-name="text-sm" />
      <KunSkeleton height="6rem" rounded="lg" />
    </div>
  </div>
</template>
