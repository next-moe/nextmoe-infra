<script setup lang="ts">
useKunDisableSeo('编辑者申请')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <EditorApplicationsContainer />
</template>
