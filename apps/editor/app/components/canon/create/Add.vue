<script setup lang="ts">
import type { CanonSchemas } from '#shared/types/canon'
import { CANON_CREATE_TEMPLATES } from '~/constants/canonCreate'

const props = defineProps<{
  templates: string[]
  schemas: CanonSchemas
  context: Record<string, string>
}>()
const emit = defineEmits<{ created: [] }>()

const open = ref('')
const template = computed(() => CANON_CREATE_TEMPLATES[open.value])

const done = () => {
  open.value = ''
  emit('created')
}
</script>

<template>
  <KunCard bordered>
    <div class="space-y-3">
      <div class="flex flex-wrap gap-2">
        <KunButton
          v-for="key in props.templates"
          :key="key"
          size="sm"
          :variant="open === key ? 'flat' : 'bordered'"
          @click="open = open === key ? '' : key"
        >
          {{ CANON_CREATE_TEMPLATES[key]!.label }}
        </KunButton>
      </div>
      <CanonCreateForm
        v-if="template"
        :template="template"
        :schemas="props.schemas"
        :context="props.context"
        @created="done"
      />
    </div>
  </KunCard>
</template>
