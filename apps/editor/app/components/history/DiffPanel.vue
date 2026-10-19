<script setup lang="ts">
import type { CatalogSchemaField } from '#shared/types/catalog'
import type { DiffContext } from '~/utils/review/context'
import type { FieldDiffLike } from '~/utils/review/history'

const props = withDefaults(
  defineProps<{
    entries: readonly FieldDiffLike[] | null
    fields: readonly CatalogSchemaField[]
    context: DiffContext
    beforeLabel?: string
    afterLabel?: string
    loading?: boolean
    error?: string
    emptyText?: string
  }>(),
  {
    beforeLabel: '之前',
    afterLabel: '之后',
    loading: false,
    error: '',
    emptyText: '这两个版本之间没有字段差异'
  }
)

const fallbackField = (key: string): CatalogSchemaField => ({
  key,
  field_type: 'text',
  diff_hint: 'inline',
  deprecated: false,
  max_suppressed: 0,
  max_elements: 0,
  vocabulary: '',
  base: 0,
  nullable: true
})

const rows = computed(() =>
  (props.entries ?? []).map((entry) => ({
    ...entry,
    field:
      props.fields.find((field) => field.key === entry.key) ??
      fallbackField(entry.key)
  }))
)
</script>

<template>
  <div class="space-y-3">
    <div v-if="props.loading" class="flex justify-center py-4">
      <KunLoading spinner />
    </div>
    <KunInfo
      v-else-if="props.error"
      color="danger"
      variant="flat"
      :description="props.error"
    />
    <p v-else-if="!rows.length" class="text-default-400 text-sm">
      {{ props.emptyText }}
    </p>
    <section
      v-for="row in rows"
      v-else
      :key="row.key"
      class="border-default-200 rounded-lg border p-3"
    >
      <h4 class="text-foreground mb-2 text-sm font-medium">
        {{ fieldLabel(row.key) }}
        <code class="text-default-400 ml-1 text-xs font-normal">{{
          row.key
        }}</code>
      </h4>
      <DiffField
        :field="row.field"
        :before="row.from"
        :after="row.to"
        :context="props.context"
        :before-label="props.beforeLabel"
        :after-label="props.afterLabel"
      />
    </section>
  </div>
</template>
