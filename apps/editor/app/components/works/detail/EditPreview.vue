<script setup lang="ts">
import type {
  CatalogEditCapabilities,
  CatalogObjectSchema
} from '#shared/types/catalog'

const props = defineProps<{ workId: string }>()

const api = useEditorApi()

const { data, pending } = useLazyAsyncData(
  `work-edit-preview-${props.workId}`,
  async () => {
    const [schema, capabilities] = await Promise.all([
      api<CatalogObjectSchema>('/v2/catalog/schemas/work').catch(() => null),
      api<CatalogEditCapabilities>('/v2/me/edit-capabilities/work', {
        query: { entity_id: props.workId }
      }).catch(() => null)
    ])
    return { schema, capabilities }
  }
)

const rows = computed(() => {
  const caps = new Map(
    (data.value?.capabilities?.fields ?? []).map((f) => [f.key, f])
  )
  return (data.value?.schema?.fields ?? [])
    .filter((field) => !field.deprecated)
    .map((field) => ({ field, cap: caps.get(field.key) }))
})

const mark = (value: boolean | undefined) =>
  value ? 'lucide:check' : 'lucide:minus'
</script>

<template>
  <div class="space-y-4">
    <KunInfo
      color="primary"
      variant="flat"
      icon="lucide:info"
      title="编辑这部作品"
      description="下面是这部作品开放编辑的字段，以及你在每个字段上的权限。可以直接生效、需要审核或仅可查看，都以资料库的实时判定为准。"
    >
      <KunButton
        :href="`/works/${props.workId}/edit`"
        target="_blank"
        color="primary"
        size="sm"
        class="mt-2"
      >
        <KunIcon name="lucide:pencil" class="size-4" />
        打开编辑器
      </KunButton>
    </KunInfo>

    <WorksDetailSection title="可编辑字段" :count="rows.length">
      <CommonSkeletonRows v-if="pending && !data" :rows="6" trailing />
      <div v-else-if="rows.length" class="overflow-x-auto">
        <table class="w-full min-w-[520px] text-sm">
          <thead class="text-default-400 text-left text-xs">
            <tr>
              <th class="pb-2 font-normal">字段</th>
              <th class="pb-2 font-normal">类型</th>
              <th class="pb-2 text-center font-normal">可提案</th>
              <th class="pb-2 text-center font-normal">免审生效</th>
              <th class="pb-2 text-center font-normal">可审核</th>
            </tr>
          </thead>
          <tbody class="divide-default-200 divide-y">
            <tr v-for="{ field, cap } in rows" :key="field.key">
              <td class="py-2 pr-3">
                <p>{{ fieldLabel(field.key) }}</p>
                <p class="text-default-400 font-mono text-xs">
                  {{ field.key }}
                </p>
              </td>
              <td class="text-default-500 py-2 pr-3">{{ field.field_type }}</td>
              <td class="py-2 text-center">
                <KunIcon
                  :name="mark(cap?.can_propose)"
                  :class="
                    cap?.can_propose ? 'text-success-600' : 'text-default-300'
                  "
                  class="inline size-4"
                />
              </td>
              <td class="py-2 text-center">
                <KunIcon
                  :name="mark(cap?.would_automerge)"
                  :class="
                    cap?.would_automerge
                      ? 'text-success-600'
                      : 'text-default-300'
                  "
                  class="inline size-4"
                />
              </td>
              <td class="py-2 text-center">
                <KunIcon
                  :name="mark(cap?.can_review)"
                  :class="
                    cap?.can_review ? 'text-success-600' : 'text-default-300'
                  "
                  class="inline size-4"
                />
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <KunNull v-else description="无法读取字段清单" :is-show-sticker="false" />
    </WorksDetailSection>
  </div>
</template>
