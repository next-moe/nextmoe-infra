<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const { value, disabled, config } = useEditorField(() => props.fieldKey)

const checked = computed({
  get: () => value.value === true,
  set: (next: boolean) => {
    value.value = next
  }
})
</script>

<template>
  <KunSwitch
    v-model="checked"
    :label="checked ? '是' : '否'"
    :disabled="disabled"
    :aria-label="config?.label ?? props.fieldKey"
  />
</template>
