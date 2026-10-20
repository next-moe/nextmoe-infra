<script setup lang="ts">
const props = withDefaults(
  defineProps<{ slug?: string; iconOnly?: boolean }>(),
  { slug: '', iconOnly: false }
)

const nav = useWorkspaceNav()

const target = computed(
  () =>
    props.slug ||
    nav.sections.value.find((entry) => entry.key === nav.section.value)
      ?.guide ||
    ''
)
</script>

<template>
  <KunTooltip v-if="target" text="在新标签页查看这一部分的编辑规范" position="bottom">
    <KunButton
      :href="`/guidelines/${target}`"
      target="_blank"
      variant="light"
      size="sm"
      :is-icon-only="props.iconOnly"
      aria-label="编辑规范"
    >
      <KunIcon name="lucide:book-open" class="size-4" />
      <span v-if="!props.iconOnly">规范</span>
    </KunButton>
  </KunTooltip>
</template>
