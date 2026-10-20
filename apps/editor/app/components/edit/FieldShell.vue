<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { WRITER_LABELS } from '~/constants/editEntities'

const props = defineProps<{ fieldKey: string }>()

const { context, field, config, capability, issues, companionKey } =
  useEditorField(() => props.fieldKey)
const editor = context.editor

const provenance = computed(
  () => context.provenance?.value[props.fieldKey] ?? []
)
const writerLabel = (source: string) => WRITER_LABELS[source] ?? source
const writer = computed(() => latestWriter(provenance.value))
const writerHistory = computed(() =>
  provenance.value
    .flatMap((column) =>
      (column.history ?? []).map(
        (entry) =>
          `${provenance.value.length > 1 ? `${column.column}：` : ''}${writerLabel(entry.source)}${entry.at ? ` ${formatDateTime(entry.at)}` : ''}`
      )
    )
    .slice(0, 8)
    .join('\n')
)

const label = computed(() => config.value?.label ?? fieldLabel(props.fieldKey))
const changed = computed(
  () =>
    editor.changedKeys.value.includes(props.fieldKey) ||
    editor.changedKeys.value.includes(companionKey.value)
)
const serverErrors = computed(() => [
  ...(editor.serverErrors.value[props.fieldKey] ?? []),
  ...(editor.serverErrors.value[companionKey.value] ?? [])
])

const state = computed(() => {
  const cap = capability.value
  if (!cap) {
    return { label: '仅查看', color: 'default' as const, icon: 'lucide:eye' }
  }
  if (cap.locked) {
    return { label: '已锁定', color: 'default' as const, icon: 'lucide:lock' }
  }
  if (!cap.can_propose) {
    return { label: '无权修改', color: 'default' as const, icon: 'lucide:ban' }
  }
  if (cap.would_automerge) {
    return { label: '直接生效', color: 'success' as const, icon: 'lucide:zap' }
  }
  return {
    label: '需审核',
    color: 'warning' as const,
    icon: 'lucide:hourglass'
  }
})

const issueText = (issue: { index?: number; message: string }) =>
  issue.index === undefined
    ? issue.message
    : `第 ${issue.index + 1} 项：${issue.message}`
</script>

<template>
  <section
    :id="`field-${props.fieldKey}`"
    :class="
      cn(
        'border-default-200 bg-content1 scroll-mt-20 rounded-xl border p-4 transition-colors',
        changed && 'border-primary-300'
      )
    "
  >
    <header class="mb-3 flex flex-wrap items-start justify-between gap-2">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2">
          <h3 class="font-semibold">{{ label }}</h3>
          <KunChip v-if="changed" size="xs" variant="flat" color="primary">
            已修改
          </KunChip>
          <KunTooltip
            v-if="!context.readonly"
            :text="field?.key ?? props.fieldKey"
            position="top"
          >
            <KunChip size="xs" variant="light" :color="state.color">
              <span class="inline-flex items-center gap-1">
                <KunIcon :name="state.icon" class="size-3" />
                {{ state.label }}
              </span>
            </KunChip>
          </KunTooltip>
          <KunTooltip v-if="writer" :text="writerHistory" position="top">
            <KunChip
              size="xs"
              variant="light"
              :color="writer.human ? 'secondary' : 'default'"
            >
              <span class="inline-flex items-center gap-1">
                <KunIcon
                  :name="writer.human ? 'lucide:user-pen' : 'lucide:database'"
                  class="size-3"
                />
                {{
                  writer.human
                    ? '人工维护'
                    : `来自 ${writerLabel(writer.source)}`
                }}
              </span>
            </KunChip>
          </KunTooltip>
        </div>
        <p v-if="config?.help" class="text-default-500 mt-1 text-xs leading-5">
          {{ config.help }}
        </p>
      </div>
      <KunButton
        v-if="changed"
        size="xs"
        variant="light"
        color="default"
        @click="editor.revertField(props.fieldKey)"
      >
        <KunIcon name="lucide:undo-2" class="size-3.5" />
        撤销修改
      </KunButton>
    </header>

    <slot />

    <ul
      v-if="issues.length || serverErrors.length"
      class="text-danger-600 mt-3 space-y-1 text-xs"
    >
      <li
        v-for="(issue, i) in issues"
        :key="`c${i}`"
        class="flex items-start gap-1"
      >
        <KunIcon name="lucide:circle-alert" class="mt-0.5 size-3.5 shrink-0" />
        {{ issueText(issue) }}
      </li>
      <li
        v-for="(message, i) in serverErrors"
        :key="`s${i}`"
        class="flex items-start gap-1"
      >
        <KunIcon name="lucide:server-crash" class="mt-0.5 size-3.5 shrink-0" />
        {{ message }}
      </li>
    </ul>
  </section>
</template>
