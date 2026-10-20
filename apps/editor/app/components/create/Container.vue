<script setup lang="ts">
import type {
  CatalogEditCapabilities,
  CatalogObjectSchema,
  CatalogProposal
} from '#shared/types/catalog'
import {
  CREATABLE_FAMILIES,
  CREATION_EXTRA_FIELDS,
  CREATION_PARAM_LABELS
} from '~/constants/creation'
import {
  EDIT_CONFIGS,
  ENTITY_META,
  isEditFamily,
  type EditFamily
} from '~/constants/editEntities'
import {
  buildCreationBody,
  duplicateSuspects,
  missingRequired,
  type DuplicateSuspect
} from '~/utils/creation'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'

const route = useRoute()
const api = useEditorApi()

const familyParam = computed(() => String(route.params.family))
const family = computed<EditFamily | null>(() =>
  isEditFamily(familyParam.value) ? familyParam.value : null
)
const isWork = computed(() => family.value === 'work')
const creatable = computed(
  () => !!family.value && CREATABLE_FAMILIES.includes(family.value)
)
const meta = computed(() => (family.value ? ENTITY_META[family.value] : null))
const config = computed(() =>
  family.value ? EDIT_CONFIGS[family.value] : null
)

const schema = ref<CatalogObjectSchema | null>(null)
const capabilities = ref<CatalogEditCapabilities | null>(null)
const loading = ref(false)
const values = ref<Record<string, unknown>>({})
const params = ref<Record<string, unknown>>({})
const note = ref('')
const submitting = ref(false)
const suspects = ref<DuplicateSuspect[]>([])
const duplicateOpen = ref(false)

const creation = computed(() => capabilities.value?.creation ?? null)
const requiredFields = computed(() => creation.value?.required_fields ?? [])
const formFields = computed(() => {
  const keys = [
    ...requiredFields.value,
    ...(family.value ? (CREATION_EXTRA_FIELDS[family.value] ?? []) : [])
  ].filter((key, index, all) => all.indexOf(key) === index)
  return keys
    .map((key) => schema.value?.fields.find((field) => field.key === key))
    .filter((field) => field !== undefined)
})
const paramSpecs = computed(() => creation.value?.param_specs ?? [])
const missing = computed(() => [
  ...missingRequired(requiredFields.value, values.value),
  ...paramSpecs.value
    .filter((spec) => spec.required && !params.value[spec.key])
    .map((spec) => spec.key)
])

const load = async () => {
  if (!family.value || !creatable.value) {
    return
  }
  loading.value = true
  const [schemaRes, caps] = await Promise.all([
    api<CatalogObjectSchema>(`/v2/catalog/schemas/${family.value}`).catch(
      () => null
    ),
    api<CatalogEditCapabilities>(
      `/v2/me/edit-capabilities/${family.value}`
    ).catch(() => null)
  ])
  schema.value = schemaRes
  capabilities.value = caps
  values.value = {}
  params.value = Object.fromEntries(
    (caps?.creation?.param_specs ?? [])
      .filter((spec) => typeof route.query[spec.key] === 'string')
      .map((spec) => [spec.key, Number(route.query[spec.key])])
  )
  loading.value = false
}

const paramValue = (key: string) =>
  params.value[key] === undefined || params.value[key] === null
    ? null
    : String(params.value[key])
const setParam = (key: string, next: string | null) => {
  params.value = { ...params.value, [key]: next ? Number(next) : null }
}

let idempotencyKey: string | null = null

const submit = async (confirmDuplicates = false) => {
  if (!family.value || submitting.value || missing.value.length) {
    return
  }
  submitting.value = true
  idempotencyKey ??= newIdempotencyKey()
  try {
    const proposal = await api<CatalogProposal>('/v2/me/creations', {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: buildCreationBody({
        object: family.value,
        values: values.value,
        params: params.value,
        note: note.value,
        confirmDuplicates
      })
    })
    idempotencyKey = null
    duplicateOpen.value = false
    if (proposal.state === 'merged' && proposal.entity_id !== '0') {
      useKunMessage(`${meta.value?.label}已创建，继续完善资料吧`, 'success')
      await navigateTo(`${meta.value?.path}/${proposal.entity_id}/edit`)
      return
    }
    useKunMessage('创建提案已提交，审核通过后条目就会出现', 'success')
    await navigateTo(`/me/proposals/${proposal.id}`)
  } catch (error) {
    idempotencyKey = null
    const found = duplicateSuspects(error)
    if (found) {
      suspects.value = found
      duplicateOpen.value = true
      return
    }
    useKunMessage(problemMessage(error, '创建失败'), 'error')
  } finally {
    submitting.value = false
  }
}

watch(familyParam, load)
await load()

useHead(() => ({
  title: `新建${meta.value?.label ?? '条目'} · 未萌编辑台`
}))
</script>

<template>
  <div class="mx-auto max-w-3xl space-y-5">
    <CreateWork v-if="isWork" />

    <KunInfo
      v-else-if="!family || !creatable"
      color="danger"
      variant="flat"
      title="不能新建这种条目"
      :description="`「${familyParam}」不能在编辑台新建。`"
    />

    <template v-else>
      <KunHeader
        :name="`新建${meta?.label}`"
        description="先填写最基本的信息创建条目，创建后可以在编辑页继续完善全部资料。"
      />

      <KunLoading v-if="loading" spinner />

      <KunInfo
        v-else-if="creation && !creation.can_create"
        color="warning"
        variant="flat"
        title="暂时不能新建"
        description="你的账号目前没有新建这种条目的权限，或者今天的提案额度已经用完。"
      />

      <KunCard v-else bordered padding="md" content-class="space-y-4">
        <KunInfo
          v-if="creation"
          :color="creation.would_automerge ? 'success' : 'default'"
          variant="flat"
          :title="
            creation.would_automerge ? '提交后直接创建' : '提交后进入审核'
          "
          :description="
            creation.would_automerge
              ? '你的权限允许直接创建这种条目。'
              : '新建条目需要审核者确认，通过后条目才会出现在资料库中。'
          "
        />

        <div v-for="spec in paramSpecs" :key="spec.key" class="space-y-1">
          <span class="text-default-700 text-sm font-medium">
            {{ CREATION_PARAM_LABELS[spec.key]?.label ?? spec.key }}
            <span v-if="spec.required" class="text-danger-500">*</span>
          </span>
          <EditRefPicker
            v-if="CREATION_PARAM_LABELS[spec.key]"
            :model-value="paramValue(spec.key)"
            :family="CREATION_PARAM_LABELS[spec.key]!.ref"
            @update:model-value="setParam(spec.key, $event)"
            :aria-label="CREATION_PARAM_LABELS[spec.key]!.label"
          />
          <p
            v-if="CREATION_PARAM_LABELS[spec.key]"
            class="text-default-400 text-xs"
          >
            {{ CREATION_PARAM_LABELS[spec.key]!.help }}
          </p>
        </div>

        <CreateFieldInput
          v-for="field in formFields"
          :key="field.key"
          v-model="values[field.key]"
          :field="field"
          :config="config?.fields[field.key]"
          :required="requiredFields.includes(field.key)"
        />

        <KunTextarea
          v-model="note"
          placeholder="说明（可选）：资料来源、为什么需要这个条目"
          aria-label="说明"
          max-height="8rem"
        />

        <div class="flex items-center justify-end gap-3">
          <span v-if="missing.length" class="text-default-400 text-xs">
            还有 {{ missing.length }} 项必填
          </span>
          <KunButton
            color="primary"
            :loading="submitting"
            :disabled="missing.length > 0"
            @click="submit()"
          >
            <KunIcon name="lucide:plus" class="size-4" />
            创建
          </KunButton>
        </div>
      </KunCard>

      <CreateDuplicateModal
        v-model="duplicateOpen"
        :suspects="suspects"
        :path="meta?.path ?? ''"
        :submitting="submitting"
        @confirm="submit(true)"
      />
    </template>
  </div>
</template>
