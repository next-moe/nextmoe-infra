<template>
  <div class="space-y-5 px-3 py-4" aria-busy="true" aria-label="正在载入">
    <div v-for="group in 3" :key="group" class="space-y-2">
      <KunSkeleton variant="text" width="3rem" class-name="text-xs" />
      <KunSkeleton
        v-for="i in group === 1 ? 5 : 3"
        :key="i"
        height="2rem"
        rounded="lg"
      />
    </div>
  </div>
</template>
