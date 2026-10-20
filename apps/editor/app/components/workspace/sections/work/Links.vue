<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const nav = useWorkspaceNav()
const key = computed(() => props.section.fields[0] ?? 'catalog.work.links')
</script>

<template>
  <WorkspacePage
    :section="props.section"
    description="身份链接把这部作品对应到 VNDB、Bangumi 等资料站上的条目；相关链接是官网、商店页与社交账号。"
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
      <EditFieldShell :field-key="key" hide-header>
        <EditLinksTable :field-key="key" />
      </EditFieldShell>
    </div>
  </WorkspacePage>
</template>
