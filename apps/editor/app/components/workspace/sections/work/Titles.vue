<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const nav = useWorkspaceNav()
const key = computed(() => props.section.fields[0] ?? 'catalog.work.titles')
</script>

<template>
  <WorkspacePage
    :section="props.section"
    description="按语言整理作品的正式名、别名与简称。删除导入的标题会同时屏蔽它，资料源再次同步也不会加回。"
  >
    <template #actions>
      <EditListSectionRevert
        :fields="props.section.fields"
        :count="nav.changesBySection.value[props.section.key] ?? 0"
      />
    </template>
    <div
      class="border-default-200 bg-content1 rounded-xl border px-4 py-1 md:px-5"
    >
      <EditFieldShell :field-key="key" hide-header inline-row-issues>
        <EditControlTitles :field-key="key" />
      </EditFieldShell>
    </div>
  </WorkspacePage>
</template>
