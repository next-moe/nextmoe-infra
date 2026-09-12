<script setup lang="ts">
import {
  CATALOG_LINK_KIND,
  CATALOG_SOURCE_LABELS,
  catalogExternalUrl
} from '~/constants/catalog'

const props = defineProps<{
  sourceId: number
  externalId: string
  linkKind: number
  entityType?: number
  shared?: boolean
}>()

const label = computed(
  () => CATALOG_SOURCE_LABELS[props.sourceId] ?? `源${props.sourceId}`
)

const url = computed(() =>
  catalogExternalUrl(props.sourceId, props.externalId, props.entityType)
)

const isProbable = computed(() => props.linkKind === CATALOG_LINK_KIND.probable)

const hint = computed(() => {
  const kind = isProbable.value ? '推测链接（未确认）' : '精确链接'
  return props.shared ? `${kind} · 双方共有的身份锚点` : kind
})
</script>

<template>
  <KunChip
    :color="shared ? 'success' : 'default'"
    :variant="isProbable ? 'bordered' : 'flat'"
    size="xs"
    :class="shared ? 'ring-success-400 ring-1' : ''"
    :title="hint"
  >
    <template #start>
      <KunIcon
        v-if="shared"
        name="lucide:anchor"
        class="text-success-600 mr-1 size-3"
      />
    </template>
    <span :class="shared ? 'text-success-700' : 'text-default-500'">
      {{ label }}
    </span>
    <KunLink
      v-if="url"
      :href="url"
      target="_blank"
      underline="hover"
      color="primary"
      class-name="ml-1 font-mono"
    >
      {{ externalId }}
    </KunLink>
    <span v-else class="text-foreground ml-1 font-mono">{{ externalId }}</span>
    <template #end>
      <KunIcon
        v-if="isProbable"
        name="lucide:circle-help"
        class="text-default-400 ml-1 size-3"
      />
    </template>
  </KunChip>
</template>
