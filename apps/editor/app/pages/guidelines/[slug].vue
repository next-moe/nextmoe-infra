<script setup lang="ts">
import { GUIDELINE_DOCS } from '~/generated/guidelines'

definePageMeta({
  title: '编辑规范',
  layout: 'docs',
  validate: (route) =>
    GUIDELINE_DOCS.some((doc) => doc.slug === String(route.params.slug))
})
</script>

<template>
  <GuidelinesDoc />
</template>
