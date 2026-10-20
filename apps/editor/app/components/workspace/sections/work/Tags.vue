<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const nav = useWorkspaceNav()
const { editor } = useEditorContext()

const tagsKey = 'catalog.work.tag_ids'
const spoilersKey = 'catalog.work.tag_spoilers'
const hasTags = computed(() => editor.fieldByKey.value.has(tagsKey))
</script>

<template>
  <WorkspacePage
    :section="props.section"
    description="作品上的全部标签，人工添加的与资料源导入的合在一起。剧透等级对同一个标签的所有来源同时生效。"
    wide
  >
    <template #actions>
      <EditListSectionRevert
        :fields="props.section.fields"
        :count="nav.changesBySection.value[props.section.key] ?? 0"
      />
    </template>
    <div
      v-if="hasTags"
      class="border-default-200 bg-content1 rounded-xl border px-4 py-1 md:px-5"
    >
      <EditFieldShell :field-key="tagsKey" hide-header>
        <EditTagsTable :tags-key="tagsKey" :spoilers-key="spoilersKey" />
      </EditFieldShell>
    </div>
    <WorkspaceFormSection v-else :fields="props.section.fields" />
  </WorkspacePage>
</template>
