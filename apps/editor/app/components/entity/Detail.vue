<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'

const props = defineProps<{ family: EditFamily }>()

const route = useRoute()
const router = useRouter()

const meta = ENTITY_META[props.family]
const entityId = computed(() => String(route.params.id))
const { data: summary, pending: summaryPending } = useEntitySummary(
  props.family,
  entityId
)

const workspace = useEntityWorkspace(props.family, entityId, {
  persist: false,
  readonly: true
})
const { editor, fieldsByGroup, groups } = workspace

const tabs = computed<KunTabItem[]>(() =>
  groups.value.map((group) => ({
    value: group.value,
    icon: group.icon,
    textValue: group.label
  }))
)
const activeGroup = ref(
  typeof route.query.group === 'string' ? route.query.group : ''
)
watch(
  tabs,
  (list) => {
    if (list.length && !list.some((tab) => tab.value === activeGroup.value)) {
      activeGroup.value = list[0]!.value
    }
  },
  { immediate: true }
)
watch(activeGroup, (group) => {
  router.replace({
    query: {
      ...route.query,
      group: group === tabs.value[0]?.value ? undefined : group
    }
  })
})

const publicFacts = computed(() =>
  Object.entries(summary.value ?? {}).filter(
    ([key, value]) =>
      !['id', 'object', '$schema', 'display_name'].includes(key) &&
      (typeof value === 'string' ||
        typeof value === 'number' ||
        typeof value === 'boolean')
  )
)

workspace.load()

const preferences = useEditorPreferences()

useHead(() => ({
  title: `${preferences.nameOf(summary.value) || summary.value?.title || `${meta.label} #${entityId.value}`} · 未萌编辑台`
}))
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-5">
    <EntityHeader
      :family="props.family"
      :entity-id="entityId"
      :summary="summary ?? null"
      :loading="summaryPending"
      mode="view"
    />

    <CommonDetailSkeleton
      v-if="editor.loading.value"
      :header="false"
      :tabs="4"
      :blocks="3"
    />

    <template v-else-if="editor.loadError.value">
      <KunInfo
        v-if="editor.loadStatus.value === 404 && !editor.schema.value"
        color="default"
        variant="flat"
        icon="lucide:construction"
        :title="`${meta.label}暂未开放编辑`"
        description="下面是这个条目的公开资料。编辑开放后，这里会按字段展示可编辑的内容。"
      />
      <KunInfo
        v-else
        color="danger"
        variant="flat"
        icon="lucide:circle-x"
        title="无法载入条目资料"
        :description="editor.loadError.value"
      />
      <KunCard v-if="publicFacts.length" bordered padding="md">
        <dl class="grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
          <div v-for="[key, value] in publicFacts" :key="key" class="min-w-0">
            <dt class="text-default-400 text-xs">{{ key }}</dt>
            <dd class="break-all">{{ value }}</dd>
          </div>
        </dl>
      </KunCard>
    </template>

    <template v-else>
      <KunTab
        v-model="activeGroup"
        :items="tabs"
        variant="underlined"
        :name="`${props.family}-detail-groups`"
      />
      <div class="space-y-4">
        <EditFieldView
          v-for="key in fieldsByGroup[activeGroup] ?? []"
          :key="key"
          :field-key="key"
        />
      </div>
    </template>
  </div>
</template>
