<script setup lang="ts">
import type { CatalogObjectSchema } from '#shared/types/catalog'
import { EDIT_CONFIGS } from '~/constants/editEntities'
import {
  duplicateSuspects,
  parseRefs,
  parseReleaseDate,
  type DuplicateSuspect
} from '~/utils/creation'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'

const OLANG = 'catalog.work.olang'
const TITLES = 'catalog.work.titles'

const api = useEditorApi()

const { data: schema, pending: schemaPending } = useLazyAsyncData(
  'create-work-schema',
  () => api<CatalogObjectSchema>('/v2/catalog/schemas/work').catch(() => null)
)

const olangField = computed(() =>
  schema.value?.fields.find((field) => field.key === OLANG)
)
const titleKindBase = computed(() => {
  const member = schema.value?.fields
    .find((field) => field.key === TITLES)
    ?.element?.members?.find((m) => m.key === 'kind')
  return member ? (memberVocabSlot(member)?.base ?? 0) : 0
})

const displayName = ref('')
const originalTitle = ref('')
const olang = ref<unknown>(null)
const refsText = ref('')
const releasedText = ref('')
const submitting = ref(false)
const suspects = ref<DuplicateSuspect[]>([])
const duplicateOpen = ref(false)

const refs = computed(() => parseRefs(refsText.value))
const released = computed(() =>
  releasedText.value.trim() ? parseReleaseDate(releasedText.value) : null
)
const releasedInvalid = computed(
  () => releasedText.value.trim() !== '' && released.value === null
)
const canSubmit = computed(
  () => displayName.value.trim() !== '' && !releasedInvalid.value
)

let idempotencyKey: string | null = null

const submit = async (confirmDuplicates = false) => {
  if (!canSubmit.value || submitting.value) {
    return
  }
  submitting.value = true
  idempotencyKey ??= newIdempotencyKey()
  const fieldValues: Record<string, unknown> = {}
  if (olang.value) {
    fieldValues[OLANG] = olang.value
  }
  if (originalTitle.value.trim()) {
    fieldValues[TITLES] = [
      {
        lang: olang.value ?? null,
        title: originalTitle.value.trim(),
        kind: titleKindBase.value,
        latin: null
      }
    ]
  }
  try {
    const claim = await api<{ id: string; state: string }>('/v2/me/claims', {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: {
        display_name: displayName.value.trim(),
        ...(Object.keys(fieldValues).length
          ? { field_values: fieldValues }
          : {}),
        ...(refs.value.length ? { refs: refs.value } : {}),
        ...(released.value ? { released: released.value } : {}),
        ...(confirmDuplicates ? { confirm_duplicates: true } : {})
      }
    })
    idempotencyKey = null
    duplicateOpen.value = false
    useKunMessage(
      claim.state === 'live'
        ? '作品已创建，继续完善资料吧'
        : '作品已提交，审核通过后会公开显示',
      'success'
    )
    await navigateTo(`/works/${claim.id}/edit`)
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
</script>

<template>
  <div class="space-y-5">
    <KunHeader
      name="新建作品"
      description="填写最基本的信息创建作品，创建后在编辑页继续补充标题、简介、厂商、职员等资料。"
    />

    <KunCard bordered padding="md" content-class="space-y-4">
      <div class="space-y-1">
        <span class="text-default-700 text-sm font-medium">
          显示名 <span class="text-danger-500">*</span>
        </span>
        <KunInput v-model="displayName" aria-label="显示名" />
        <p class="text-default-400 text-xs">
          列表与页面标题使用的名称，通常是作品的正式名。
        </p>
      </div>

      <CreateFieldInput
        v-if="olangField"
        v-model="olang"
        :field="olangField"
        :config="EDIT_CONFIGS.work.fields[OLANG]"
      />
      <div v-else-if="schemaPending" class="space-y-1.5" aria-busy="true">
        <KunSkeleton variant="text" class-name="text-sm" width="4rem" />
        <KunSkeleton height="2.5rem" rounded="lg" />
      </div>

      <div class="space-y-1">
        <span class="text-default-700 text-sm font-medium">原文标题</span>
        <KunInput v-model="originalTitle" aria-label="原文标题" />
        <p class="text-default-400 text-xs">
          作品原语言的正式名；和显示名相同时也可以填上，用于查重与搜索。
        </p>
      </div>

      <div class="space-y-1">
        <span class="text-default-700 text-sm font-medium">资料站链接</span>
        <KunInput
          v-model="refsText"
          aria-label="资料站链接"
          placeholder="例如 vndb:v17  bangumi:12345"
        />
        <p class="text-default-400 text-xs">
          可选，空格分隔。资料站 ID
          已经对应到某部作品时，会直接认领那部作品而不是新建。
        </p>
      </div>

      <div class="space-y-1">
        <span class="text-default-700 text-sm font-medium">发售日期</span>
        <KunInput
          v-model="releasedText"
          aria-label="发售日期"
          placeholder="YYYY、YYYY-MM 或 YYYY-MM-DD，未定可留空"
          :is-invalid="releasedInvalid"
        />
      </div>

      <div class="flex justify-end">
        <KunButton
          color="primary"
          :loading="submitting"
          :disabled="!canSubmit"
          @click="submit()"
        >
          <KunIcon name="lucide:plus" class="size-4" />
          创建作品
        </KunButton>
      </div>
    </KunCard>

    <CreateDuplicateModal
      v-model="duplicateOpen"
      :suspects="suspects"
      path="/works"
      :submitting="submitting"
      @confirm="submit(true)"
    />
  </div>
</template>
