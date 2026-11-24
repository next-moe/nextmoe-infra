<script setup lang="ts">
import type {
  CanonEditChange,
  CanonRecord,
  CanonSchemaEntity,
  CanonSchemas
} from '#shared/types/canon'
import { CANON_EDITABLE_ENTITIES, CANON_ENTITY_LABELS } from '~/constants/canon'
import { changeFrom, draftOf } from '~/utils/canon/record'

const props = defineProps<{
  record: CanonRecord
  schemas: CanonSchemas
  /** The field that says which parent this row belongs to; not shown. */
  parentField?: string
  removable?: boolean
  busy?: boolean
}>()
const emit = defineEmits<{ save: [CanonEditChange] }>()

const schema = computed<CanonSchemaEntity | undefined>(() =>
  props.schemas.entities.find((e) => e.name === props.record.entity)
)
const properties = computed(() =>
  (schema.value?.properties ?? []).filter((p) => p.key !== props.parentField)
)
const draft = ref(draftOf(props.record))
watch(
  () => props.record,
  (r) => {
    draft.value = draftOf(r)
  }
)
const change = computed(() => changeFrom(props.record, properties.value, draft.value))
const terms = (vocab?: string) =>
  props.schemas.vocabularies.find((v) => v.name === vocab)?.terms ?? []
const opens = computed(() =>
  (CANON_EDITABLE_ENTITIES as readonly string[]).includes(props.record.entity)
)

const remove = async () => {
  if (await useKunAlert({ title: '移除这一条？', message: '可以在修订记录里撤销。' })) {
    emit('save', {
      entity: props.record.entity,
      id: props.record.id,
      expect_version: props.record.version,
      remove: true
    })
  }
}
</script>

<template>
  <div class="space-y-3 px-4 py-3">
    <div class="flex items-center gap-2">
      <span class="text-foreground font-medium">
        {{ CANON_ENTITY_LABELS[props.record.entity] ?? props.record.entity }}
      </span>
      <NuxtLink
        v-if="opens"
        :to="`/canon/${props.record.entity}/${props.record.id}`"
        class="text-primary-600 font-mono text-xs"
      >
        #{{ props.record.id }}
      </NuxtLink>
      <span v-else class="text-default-400 font-mono text-xs">
        #{{ props.record.id }}
      </span>
      <span class="text-default-400 text-xs">第 {{ props.record.version }} 版</span>
      <KunChip v-if="props.record.held" color="warning" size="sm" variant="flat">
        已冻结采集写入
      </KunChip>
      <div class="flex-1" />
      <KunButton
        v-if="props.removable"
        size="sm"
        variant="light"
        color="danger"
        :disabled="props.busy"
        @click="remove"
      >
        移除
      </KunButton>
      <KunButton
        size="sm"
        color="primary"
        :disabled="!change"
        :loading="props.busy"
        @click="change && emit('save', change)"
      >
        保存
      </KunButton>
    </div>
    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <CanonRecordField
        v-for="p in properties"
        :key="p.key"
        v-model="draft[p.key]!"
        :property="p"
        :state="props.record.state[p.key]"
        :terms="terms(p.vocab)"
      />
    </div>
  </div>
</template>
