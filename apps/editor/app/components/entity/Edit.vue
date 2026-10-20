<script setup lang="ts">
import type { EditFamily } from '~/constants/editEntities'

const props = defineProps<{ family: EditFamily }>()

const route = useRoute()
const entityId = computed(() => String(route.params.id))
const { data: summary } = await useEntitySummary(props.family, entityId)
</script>

<template>
  <EditEntityEditor
    :family="props.family"
    :entity-id="entityId"
    :entity-name="summary?.display_name || summary?.title"
  >
    <template #header>
      <EntityHeader
        :family="props.family"
        :entity-id="entityId"
        :summary="summary ?? null"
        mode="edit"
      />
    </template>
  </EditEntityEditor>
</template>
