<script setup lang="ts">
import { ENTITY_META } from '~/constants/editEntities'
import type { WorkspaceEntity } from '~/composables/useWorkspaceSession'

const open = defineModel<boolean>({ required: true })

const session = useWorkspaceSession()
const nav = useWorkspaceNav()

const entities = computed(() =>
  [...session.dirty.value].sort((a, b) =>
    a.key === session.root.key ? -1 : b.key === session.root.key ? 1 : 0
  )
)

const keysOf = (entity: WorkspaceEntity) =>
  entity.workspace.editor.changedKeys.value

const locate = (entity: WorkspaceEntity, key: string) => {
  open.value = false
  if (entity.key === session.root.key) {
    nav.jumpToField(parentFieldKey(key))
  }
}

const discard = async (entity: WorkspaceEntity) => {
  const ok = await useKunAlert({
    title: '放弃这个条目的修改',
    message: `「${session.nameOf(entity)}」上的全部修改都会被清空。`,
    type: 'warning',
    confirmText: '放弃',
    cancelText: '保留'
  })
  if (ok) {
    entity.workspace.editor.resetAll()
  }
}

watch(
  () => session.isDirty.value,
  (dirty) => {
    if (!dirty) {
      open.value = false
    }
  }
)
</script>

<template>
  <KunModal
    v-model="open"
    title="查看更改"
    :description="`${entities.length} 个条目，共 ${session.changeCount.value} 项修改。下面是修改前后的对比。`"
    size="xl"
  >
    <div class="-mx-1 max-h-[70vh] space-y-6 overflow-y-auto px-1">
      <section v-for="entity in entities" :key="entity.key" class="space-y-3">
        <header
          class="border-default-200 flex flex-wrap items-center gap-2 border-b pb-2"
        >
          <KunIcon
            :name="ENTITY_META[entity.family].icon"
            class="text-default-500 size-4"
          />
          <span class="text-default-500 text-sm">
            {{ ENTITY_META[entity.family].label }}
          </span>
          <span class="min-w-0 truncate font-semibold">
            {{ session.nameOf(entity) }}
          </span>
          <KunChip
            size="xs"
            variant="flat"
            :color="
              entity.workspace.editor.willAutomerge.value
                ? 'success'
                : 'warning'
            "
          >
            {{
              entity.workspace.editor.willAutomerge.value
                ? '提交后直接生效'
                : '提交后进入审核'
            }}
          </KunChip>
          <span class="flex-1" />
          <KunButton
            size="xs"
            variant="light"
            color="danger"
            @click="discard(entity)"
          >
            放弃此条目的修改
          </KunButton>
        </header>

        <WorkspaceScope :workspace="entity.workspace">
          <div class="space-y-3">
            <EditFieldDiff
              v-for="key in keysOf(entity)"
              :key="key"
              :field-key="key"
            >
              <template #actions>
                <span class="flex-1" />
                <KunButton
                  v-if="entity.key === session.root.key"
                  size="xs"
                  variant="light"
                  @click="locate(entity, key)"
                >
                  <KunIcon name="lucide:crosshair" class="size-3.5" />
                  定位
                </KunButton>
                <KunButton
                  size="xs"
                  variant="light"
                  color="default"
                  @click="
                    entity.workspace.editor.revertField(parentFieldKey(key))
                  "
                >
                  <KunIcon name="lucide:undo-2" class="size-3.5" />
                  撤销
                </KunButton>
              </template>
            </EditFieldDiff>
          </div>
        </WorkspaceScope>
      </section>
    </div>
  </KunModal>
</template>
