<script setup lang="ts">
import type { LlmModel } from '~~/shared/types/llm'

const props = defineProps<{
  models: LlmModel[]
  primaryId: number
  label: string
  hint: string
}>()
const ids = defineModel<number[]>({ required: true })

const addId = ref<number | null>(null)

const byId = (id: number) => props.models.find((m) => m.id === id)
const primaryKind = computed(() => byId(props.primaryId)?.kind)

const options = computed(() =>
  props.models
    .filter(
      (m) =>
        m.kind === primaryKind.value &&
        m.id !== props.primaryId &&
        !ids.value.includes(m.id)
    )
    .map((m) => ({ value: m.id, label: m.key }))
)

watch(addId, (id) => {
  if (id == null) return
  ids.value = [...ids.value, id]
  nextTick(() => (addId.value = null))
})

const move = (i: number, delta: number) => {
  const j = i + delta
  if (j < 0 || j >= ids.value.length) return
  const next = [...ids.value]
  ;[next[i], next[j]] = [next[j]!, next[i]!]
  ids.value = next
}

const remove = (i: number) => {
  ids.value = ids.value.filter((_, k) => k !== i)
}
</script>

<template>
  <div class="space-y-2">
    <p class="text-default-500 text-sm">
      <span class="text-foreground font-medium">{{ label }}</span>
      <span class="text-default-400 ml-2 text-xs">{{ hint }}</span>
    </p>
    <div
      v-for="(id, i) in ids"
      :key="id"
      class="border-default-200 flex items-center gap-2 rounded-lg border px-3 py-1.5"
    >
      <span class="text-default-400 w-5 text-xs tabular-nums">
        {{ i + 1 }}
      </span>
      <span class="text-foreground min-w-0 flex-1 font-mono text-sm break-all">
        {{ byId(id)?.key ?? `#${id}` }}
      </span>
      <KunButton
        size="sm"
        variant="light"
        is-icon-only
        aria-label="上移"
        :disabled="i === 0"
        @click="move(i, -1)"
      >
        <KunIcon name="lucide:arrow-up" class="size-4" />
      </KunButton>
      <KunButton
        size="sm"
        variant="light"
        is-icon-only
        aria-label="下移"
        :disabled="i === ids.length - 1"
        @click="move(i, 1)"
      >
        <KunIcon name="lucide:arrow-down" class="size-4" />
      </KunButton>
      <KunButton
        size="sm"
        variant="light"
        color="danger"
        is-icon-only
        aria-label="移除"
        @click="remove(i)"
      >
        <KunIcon name="lucide:x" class="size-4" />
      </KunButton>
    </div>
    <KunSelect
      v-model="addId"
      :options="options"
      :disabled="!options.length"
      :placeholder="
        options.length ? `添加到「${label}」…` : '没有可添加的同类型模型'
      "
      :aria-label="`添加到「${label}」`"
    />
  </div>
</template>
