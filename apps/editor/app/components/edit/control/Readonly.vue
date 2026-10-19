<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const { value } = useEditorField(() => props.fieldKey)

const preview = computed(() => JSON.stringify(value.value ?? null, null, 2))
</script>

<template>
  <div class="space-y-2">
    <p class="text-default-500 text-xs">
      编辑台暂时还没有适合这一项的编辑控件，下面是它当前的值。
    </p>
    <pre
      class="bg-default-50 text-default-600 max-h-64 overflow-auto rounded-lg p-3 text-xs"
      >{{ preview }}</pre>
  </div>
</template>
