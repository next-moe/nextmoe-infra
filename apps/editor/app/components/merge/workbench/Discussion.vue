<script setup lang="ts">
import type { CatalogMergeRequest } from '#shared/types/catalog'

const props = defineProps<{ request: CatalogMergeRequest }>()

const participants = computed(() => {
  const r = props.request
  return [
    { uid: r.proposed_by_uid, user: r.proposed_by, role: '发起人' },
    ...(r.amended_by_uid
      ? [{ uid: r.amended_by_uid, user: r.amended_by, role: '审核人' }]
      : []),
    ...(r.undone_by_uid
      ? [{ uid: r.undone_by_uid, user: r.undone_by, role: '撤销人' }]
      : [])
  ]
})
</script>

<template>
  <DiscussionThread
    anchor-kind="merge_request"
    :anchor-id="props.request.id"
    :participants="participants"
  />
</template>
