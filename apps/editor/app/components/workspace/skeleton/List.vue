<script setup lang="ts">
const props = withDefaults(defineProps<{ rows?: number; avatar?: boolean }>(), {
  rows: 8,
  avatar: false
})
</script>

<template>
  <ul class="space-y-1 p-2" aria-busy="true" aria-label="正在载入">
    <li
      v-for="i in props.rows"
      :key="i"
      class="flex items-center gap-3 px-2 py-2"
    >
      <KunSkeleton
        v-if="props.avatar"
        variant="circle"
        width="2rem"
        height="2rem"
      />
      <div class="min-w-0 flex-1">
        <KunSkeleton
          variant="text"
          :width="`${50 + ((i * 17) % 40)}%`"
          class-name="text-sm"
        />
        <KunSkeleton variant="text" width="30%" class-name="text-xs" />
      </div>
    </li>
  </ul>
</template>
