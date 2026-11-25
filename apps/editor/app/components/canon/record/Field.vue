<script setup lang="ts">
import type { CanonFieldState, CanonSchemaProperty } from '#shared/types/canon'
import { CANON_PROPERTY_LABELS } from '~/constants/canon'
import { formatRelative } from '~/utils/format'

const props = defineProps<{
  property: CanonSchemaProperty
  state?: CanonFieldState
  terms?: { key: string; doc?: string }[]
  /** What the cited id is called. */
  cited?: string
  disabled?: boolean
  /** Creating: what is fixed at creation can still be chosen. */
  creating?: boolean
}>()
const model = defineModel<string | boolean>({ required: true })

const label = computed(
  () => CANON_PROPERTY_LABELS[props.property.key] ?? props.property.key
)
const LOOKUP_ENTITIES = ['work', 'person', 'person_alias', 'org']

const refText = computed({
  get: () => String(model.value ?? ''),
  set: (v: string) => {
    model.value = v
  }
})
const readOnly = computed(() => props.disabled || (props.property.immutable && !props.creating))
const options = computed(() =>
  (props.terms ?? []).map((t) => ({ value: t.key, label: t.key }))
)
const origin = computed(() => {
  const s = props.state
  if (!s) {
    return ''
  }
  const who = s.by === 'human' ? '人工' : s.by === 'agent' ? s.key : '系统'
  const how = [s.tier, s.source].filter(Boolean).join(' · ')
  return [who, how, s.reviewed_by && `审核 ${s.reviewed_by}`, s.at && formatRelative(s.at)]
    .filter(Boolean)
    .join(' · ')
})
</script>

<template>
  <div class="space-y-1">
    <div class="flex items-center gap-1">
      <span class="text-default-500 text-sm">{{ label }}</span>
      <KunTooltip :text="`${props.property.doc}${props.property.clauses.length ? '（' + props.property.clauses.join('、') + '）' : ''}`">
        <KunIcon name="lucide:info" class="text-default-300 size-3.5" />
      </KunTooltip>
    </div>
    <KunSwitch
      v-if="props.property.shape === 'bool'"
      :model-value="model === true"
      :disabled="readOnly"
      @update:model-value="(v: boolean) => (model = v)"
    />
    <KunSelect
      v-else-if="props.property.shape === 'vocab'"
      :model-value="String(model)"
      :options="options"
      :disabled="readOnly"
      :aria-label="label"
      size="sm"
      @update:model-value="(v: string | string[] | null) => (model = String(v ?? ''))"
    />
    <CanonRefInput
      v-else-if="props.property.shape === 'ref' && LOOKUP_ENTITIES.includes(props.property.ref ?? '')"
      v-model="refText"
      :entity="props.property.ref!"
      :label="label"
      :disabled="readOnly"
    />
    <KunInput
      v-else
      :model-value="String(model)"
      :disabled="readOnly"
      :aria-label="label"
      size="sm"
      @update:model-value="(v: string | number) => (model = String(v))"
    />
    <p v-if="props.cited" class="text-foreground text-sm">{{ props.cited }}</p>
    <p v-if="origin" class="text-default-400 text-xs">{{ origin }}</p>
    <p
      v-for="e in props.state?.evidence ?? []"
      :key="e"
      class="text-default-400 text-xs break-all"
    >
      「{{ e }}」
    </p>
  </div>
</template>
