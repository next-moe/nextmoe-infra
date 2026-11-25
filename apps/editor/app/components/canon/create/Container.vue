<script setup lang="ts">
import type { CanonSchemas } from '#shared/types/canon'
import { CANON_CREATE_ROOTS, CANON_CREATE_TEMPLATES } from '~/constants/canonCreate'
import { canonMessage } from '~/utils/canon/record'

const api = useEditorApi()
const route = useRoute()
const schemas = ref<CanonSchemas | null>(null)
const kind = computed(() => {
  const k = String(route.query.kind ?? 'work')
  return (CANON_CREATE_ROOTS as readonly string[]).includes(k) ? k : 'work'
})
const template = computed(() => CANON_CREATE_TEMPLATES[kind.value]!)

useHead({ title: () => `${template.value.label} · 未萌编辑台` })

const created = (ids: Record<string, string>) => {
  const open = template.value.open
  if (open && ids[open.ref]) {
    navigateTo(`/canon/${open.entity}/${ids[open.ref]}`)
  }
}

onMounted(async () => {
  try {
    schemas.value = await api<CanonSchemas>('/v3/catalog/schemas')
  } catch (error) {
    useKunMessage(canonMessage(error), 'error')
  }
})
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5">
    <KunHeader
      :name="template.label"
      description="直接写进自有目录。只按出版社、レーベル或登记机构的资料填，拿不准的留空。"
    />
    <KunCard v-if="schemas" bordered>
      <CanonCreateForm
        :template="template"
        :schemas="schemas"
        :context="{}"
        @created="created"
      />
    </KunCard>
    <CommonSkeletonRows v-else :rows="5" />
  </div>
</template>
