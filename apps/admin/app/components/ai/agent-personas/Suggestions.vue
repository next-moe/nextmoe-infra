<script setup lang="ts">
import type {
  AgentSuggestionDraft,
  AgentSuggestionGroup
} from '~~/shared/types/agent'

const props = defineProps<{ builtin: AgentSuggestionGroup[] }>()
const groups = defineModel<AgentSuggestionDraft[]>({ required: true })

const toDraft = (g: AgentSuggestionGroup): AgentSuggestionDraft => ({
  label: g.label,
  icon: g.icon,
  web_search: g.web_search,
  promptsText: (g.prompts ?? []).join('\n')
})

const add = () => {
  groups.value = [
    ...groups.value,
    { label: '', icon: 'sparkles', web_search: false, promptsText: '' }
  ]
}
const remove = (i: number) => {
  groups.value = groups.value.filter((_, j) => j !== i)
}
const copyBuiltin = () => {
  groups.value = props.builtin.map(toDraft)
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div>
        <p class="text-foreground text-sm font-medium">建议问题</p>
        <p class="text-default-400 text-xs">
          空对话时输入框下的模式按钮。不配置时使用内置的
          {{ builtin.length }} 组。
        </p>
      </div>
      <div class="flex gap-2">
        <KunButton size="sm" variant="flat" @click="copyBuiltin">
          复制内置分组来改
        </KunButton>
        <KunButton size="sm" variant="flat" color="primary" @click="add">
          <KunIcon name="lucide:plus" class="mr-1 size-4" />
          添加分组
        </KunButton>
      </div>
    </div>

    <div
      v-for="(g, i) in groups"
      :key="i"
      class="border-default-200 space-y-3 rounded-lg border p-3"
    >
      <div class="grid gap-3 sm:grid-cols-[1fr_1fr_auto_auto] sm:items-end">
        <KunInput v-model="g.label" label="按钮文字" placeholder="找作品" />
        <KunInput
          v-model="g.icon"
          label="图标"
          placeholder="search"
          description="lucide 图标名"
        />
        <KunSwitch v-model="g.web_search" label="联网发送" />
        <KunButton
          size="sm"
          variant="light"
          color="danger"
          :aria-label="`删除分组 ${g.label}`"
          @click="remove(i)"
        >
          <KunIcon name="lucide:trash-2" class="size-4" />
        </KunButton>
      </div>
      <KunTextarea
        v-model="g.promptsText"
        label="问题（每行一条，1–12 条）"
        :rows="4"
      />
    </div>
  </div>
</template>
