<script setup lang="ts">
import type { EditFamily } from '~/constants/editEntities'

const props = defineProps<{
  family: EditFamily
  entityId: string
  patch: Record<string, unknown>
  state: string
}>()

const workspace = useEntityWorkspace(props.family, () => props.entityId, {
  persist: false
})
const { editor } = workspace

const patchKeys = computed(() => Object.keys(props.patch))

workspace.load().then(() => editor.overlay(props.patch))
</script>

<template>
  <div v-if="editor.loading.value" class="space-y-2" aria-busy="true">
    <KunSkeleton
      v-for="index in Math.min(Math.max(patchKeys.length, 1), 4)"
      :key="index"
      height="4.5rem"
      rounded="lg"
    />
  </div>
  <div v-else-if="editor.changedKeys.value.length" class="space-y-2">
    <p v-if="props.state !== 'open'" class="text-default-500 text-xs">
      与条目当前内容对比：
    </p>
    <EditFieldDiff
      v-for="key in editor.changedKeys.value"
      :key="key"
      :field-key="key"
    />
  </div>
  <p v-else class="text-default-500 text-sm">
    {{
      props.state === 'merged'
        ? '这些修改已经生效，条目当前内容已包含它们。'
        : '提议的内容与条目当前内容一致。'
    }}
    涉及字段：{{ patchKeys.map((key) => fieldLabel(key)).join('、') }}
  </p>
</template>
