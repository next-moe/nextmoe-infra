<script setup lang="ts">
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-foreground text-2xl font-bold">T&S 审核队列</h1>
    <TrustSubNav />
    <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
      <TrustSubjectKinds />
      <TrustReasons />
    </div>
  </div>
</template>
