<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const nav = useWorkspaceNav()
const key = computed(() => props.section.fields[0] ?? 'catalog.work.intros')
</script>

<template>
  <WorkspacePage
    :section="props.section"
    description="每种语言一段。人工撰写的简介优先显示；没有人工简介时显示资料源的版本，也可以隐藏不合适的资料源版本。"
    wide
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
        <EditControlIntros :field-key="key" />
      </EditFieldShell>
    </div>
  </WorkspacePage>
</template>
