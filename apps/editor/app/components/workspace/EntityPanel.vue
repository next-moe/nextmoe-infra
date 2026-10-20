<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'

const props = withDefaults(
  defineProps<{
    family: EditFamily
    entityId: string
    label?: string
    subtitle?: string
    thumbnail?: { url: string; thumbhash?: string | null } | null
  }>(),
  { label: '', subtitle: '', thumbnail: null }
)

const session = useWorkspaceSession()
const entity = session.open(
  props.family,
  props.entityId,
  props.label || undefined
)
const workspace = entity.workspace
const editor = workspace.editor
const meta = ENTITY_META[props.family]

const { sections, changesBySection } = useWorkspaceSections(workspace)

const tab = useState<string>(`workspace-panel-tab-${props.family}`, () => '')
const tabs = computed<KunTabItem[]>(() =>
  sections.value.map((section) => ({
    value: section.key,
    icon: section.icon,
    textValue: changesBySection.value[section.key]
      ? `${section.label} · ${changesBySection.value[section.key]}`
      : section.label
  }))
)
const activeKey = computed({
  get: () =>
    sections.value.some((section) => section.key === tab.value)
      ? tab.value
      : (sections.value[0]?.key ?? ''),
  set: (next: string) => {
    tab.value = next
  }
})
const activeSection = computed(() =>
  sections.value.find((section) => section.key === activeKey.value)
)

const changeCount = computed(() => changedParentKeys(workspace).length)
const canEditAnything = computed(() =>
  (editor.capabilities.value?.fields ?? []).some((cap) => cap.can_propose)
)
const name = computed(() => session.nameOf(entity))
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <header
      class="border-default-200 flex shrink-0 items-center gap-3 border-b px-5 py-3"
    >
      <div
        class="bg-default-100 text-default-400 flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-lg"
      >
        <KunImage
          v-if="props.thumbnail?.url"
          :src="props.thumbnail.url"
          :thumbhash="props.thumbnail.thumbhash ?? undefined"
          alt=""
          aspect-ratio="1 / 1"
          object-fit="cover"
          class-name="size-10"
        />
        <KunIcon v-else :name="meta.icon" class="size-5" />
      </div>
      <div class="min-w-0 flex-1">
        <p class="flex items-center gap-2">
          <span class="truncate font-semibold">{{ name }}</span>
          <KunChip v-if="changeCount" size="xs" variant="flat" color="primary">
            {{ changeCount }} 项修改
          </KunChip>
        </p>
        <p class="text-default-500 truncate text-xs">
          {{ props.subtitle || `${meta.label} #${props.entityId}` }}
        </p>
      </div>
      <slot name="actions" />
      <KunTooltip
        :text="`在新标签页单独编辑这个${meta.label}`"
        position="bottom"
      >
        <KunButton
          :href="`${meta.path}/${props.entityId}/edit`"
          target="_blank"
          variant="light"
          size="sm"
          is-icon-only
          :aria-label="`单独编辑这个${meta.label}`"
        >
          <KunIcon name="lucide:external-link" class="size-4" />
        </KunButton>
      </KunTooltip>
    </header>

    <div class="min-h-0 flex-1 overflow-y-auto">
      <div v-if="$slots.context" class="border-default-200 border-b px-5 py-4">
        <slot name="context" />
      </div>

      <WorkspaceScope :workspace="workspace">
        <div v-if="editor.loading.value" class="px-5 py-4">
          <WorkspaceSkeletonForm :rows="4" />
        </div>
        <KunInfo
          v-else-if="editor.loadError.value"
          color="danger"
          variant="flat"
          icon="lucide:circle-x"
          :title="`无法打开这个${meta.label}`"
          :description="editor.loadError.value"
          class-name="m-5"
        />
        <template v-else>
          <div class="space-y-3 px-5 pt-4">
            <WorkspaceDraftNotice :workspace="workspace" />
            <KunInfo
              v-if="!canEditAnything"
              color="warning"
              variant="flat"
              icon="lucide:lock"
              :title="
                editor.capabilities.value?.quota_exhausted
                  ? '今天的提案额度已用完'
                  : `你目前不能修改这个${meta.label}`
              "
              description="可以查看全部内容。"
            />
          </div>
          <div
            class="border-default-200 bg-content1 sticky top-0 z-10 border-b px-5"
          >
            <KunTab
              v-model="activeKey"
              :items="tabs"
              variant="underlined"
              :name="`workspace-panel-${props.family}`"
            />
          </div>
          <div class="px-5 py-4">
            <WorkspaceFormSection
              :fields="activeSection?.fields ?? []"
              :section-label="activeSection?.label"
            />
          </div>
        </template>
      </WorkspaceScope>
    </div>
  </div>
</template>
