<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'

const route = useRoute()
const api = useEditorApi()

const workId = computed(() => String(route.params.id))
const work = ref<CatalogWork | null>(null)

work.value = await api<CatalogWork>(`/v2/catalog/works/${workId.value}`, {
  query: { nsfw: true }
}).catch(() => null)
</script>

<template>
  <EditEntityEditor
    family="work"
    :entity-id="workId"
    :entity-name="work?.display_name"
  >
    <template #header>
      <WorksEditHeader :work-id="workId" :work="work" />
    </template>
  </EditEntityEditor>
</template>
