<script setup lang="ts">
import type { CanonProblemCandidate, CanonSchemas } from '#shared/types/canon'
import { CANON_ENTITY_LABELS, CANON_PROPERTY_LABELS } from '~/constants/canon'
import type { CanonCreateTemplate } from '~/constants/canonCreate'
import {
  changesFor,
  draftsFor,
  judgedRef,
  missingFields,
  resolvedChanges
} from '~/utils/canon/create'
import { canonCandidates, canonMessage } from '~/utils/canon/record'

const props = defineProps<{
  template: CanonCreateTemplate
  schemas: CanonSchemas
  context: Record<string, string>
}>()
const emit = defineEmits<{ created: [ids: Record<string, string>] }>()

const api = useEditorApi()
const drafts = ref(draftsFor(props.template))
const candidates = ref<CanonProblemCandidate[]>([])
const busy = ref(false)

watch(
  () => props.template.key,
  () => {
    drafts.value = draftsFor(props.template)
    candidates.value = []
  }
)

const missing = computed(() => missingFields(props.template, drafts.value))
const property = (entity: string, key: string) =>
  props.schemas.entities
    .find((e) => e.name === entity)
    ?.properties.find((p) => p.key === key)
const terms = (vocab?: string) =>
  props.schemas.vocabularies.find((v) => v.name === vocab)?.terms ?? []

const origin = (c: CanonProblemCandidate) =>
  c.token.startsWith('legacy:') ? '旧库' : 'canon'

const submit = async (resolution?: { adopt: string }) => {
  busy.value = true
  try {
    let changes = changesFor(
      props.template,
      drafts.value,
      props.schemas.entities,
      props.context
    )
    if (resolution) {
      changes = resolvedChanges(
        changes,
        judgedRef(props.template),
        candidates.value,
        resolution.adopt
      )
    }
    const result = await api<{ ids: Record<string, string> }>('/v3/catalog/commits', {
      method: 'POST',
      body: { note: `编辑台${props.template.label}`, changes }
    })
    useKunMessage('已保存', 'success')
    drafts.value = draftsFor(props.template)
    candidates.value = []
    emit('created', result.ids)
  } catch (error) {
    const found = canonCandidates(error)
    if (found.length) {
      candidates.value = [
        ...candidates.value,
        ...found.filter((f) => !candidates.value.some((c) => c.token === f.token))
      ]
    } else {
      useKunMessage(canonMessage(error), 'error')
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="space-y-4" @submit.prevent="submit()">
    <div v-for="part in props.template.parts.filter((p) => p.fields.length)" :key="part.ref" class="space-y-2">
      <p class="text-foreground text-sm font-medium">
        {{ CANON_ENTITY_LABELS[part.entity] ?? part.entity }}
      </p>
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <template v-for="key in part.fields" :key="key">
          <CanonRecordField
            v-if="property(part.entity, key)"
            v-model="drafts[part.ref]![key]!"
            :property="property(part.entity, key)!"
            :terms="terms(property(part.entity, key)!.vocab)"
            creating
          />
        </template>
      </div>
    </div>

    <div v-if="candidates.length" class="border-warning-300 space-y-2 rounded-lg border px-3 py-2">
      <p class="text-foreground text-sm font-medium">已经有像它的条目，先认一认：</p>
      <ul class="space-y-1">
        <li v-for="c in candidates" :key="c.token" class="flex flex-wrap items-center gap-2 text-sm">
          <span class="text-default-500">{{ origin(c) }} #{{ c.id }}</span>
          <span class="text-foreground">{{ c.label }}</span>
          <KunButton
            v-if="c.token.startsWith('legacy:')"
            size="sm"
            variant="light"
            type="button"
            :loading="busy"
            @click="submit({ adopt: c.id })"
          >
            就是它，沿用编号
          </KunButton>
        </li>
      </ul>
      <p class="text-default-500 text-sm">canon 里已有的就不要再建，直接去编辑那一条。</p>
      <KunButton
        size="sm"
        variant="bordered"
        type="button"
        :loading="busy"
        @click="submit({ adopt: '' })"
      >
        都不是，照样新建
      </KunButton>
    </div>

    <div class="flex items-center justify-end gap-3">
      <p v-if="missing.length" class="text-default-500 text-sm">
        还要填：{{ missing.map((m) => CANON_PROPERTY_LABELS[m.field] ?? m.field).join('、') }}
      </p>
      <KunButton color="primary" type="submit" :disabled="missing.length > 0" :loading="busy">
        保存
      </KunButton>
    </div>
  </form>
</template>
