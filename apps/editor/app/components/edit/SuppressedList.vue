<script setup lang="ts">
const props = defineProps<{ fieldKey: string; title?: string }>()

const { context, companion, suppressed, disabled, toggleSuppressed } =
  useEditorField(() => props.fieldKey)
const refs = context.refs

const visibleIdentities = computed(
  () =>
    new Set(
      (context.upstreamRows.value[props.fieldKey] ?? [])
        .map((row) => row.identity)
        .filter((identity): identity is string => Boolean(identity))
    )
)

const hidden = computed(() =>
  suppressed.value
    .filter((key) => !visibleIdentities.value.has(key))
    .map((key) => ({ key, parts: suppressedKeyParts(key) }))
)

watch(
  hidden,
  (list) => {
    for (const { parts } of list) {
      for (const ref of parts.refs) {
        refs.resolve(ref.family, [ref.id])
      }
    }
  },
  { immediate: true }
)

const describe = (entry: (typeof hidden.value)[number]) => {
  if (entry.parts.label) {
    return entry.parts.label
  }
  const names = entry.parts.refs.map(
    (ref) => refs.nameOf(ref.family, ref.id) || `#${ref.id}`
  )
  return names.length ? names.join(' · ') : entry.key
}
</script>

<template>
  <div
    v-if="companion && hidden.length"
    class="border-default-200 bg-default-50 mt-3 rounded-lg border px-3 py-2"
  >
    <p class="text-default-600 mb-1 flex items-center gap-1.5 text-sm">
      <KunIcon name="lucide:eye-off" class="size-4" />
      {{ props.title ?? '已隐藏的资料源条目' }}
      <span class="text-default-400 tabular-nums">{{ hidden.length }}</span>
    </p>
    <ul class="divide-default-200 divide-y">
      <li
        v-for="entry in hidden"
        :key="entry.key"
        class="flex items-center gap-2 py-1.5 text-sm"
      >
        <span class="text-default-500 min-w-0 flex-1 break-all">
          {{ describe(entry) }}
        </span>
        <KunButton
          v-if="!disabled"
          size="xs"
          variant="light"
          color="primary"
          @click="toggleSuppressed(entry.key)"
        >
          取消隐藏
        </KunButton>
      </li>
    </ul>
  </div>
</template>
