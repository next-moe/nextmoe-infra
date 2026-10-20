<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { WRITER_LABELS } from '~/constants/editEntities'

const props = withDefaults(
  defineProps<{
    fieldKey: string
    layout?: 'row' | 'block'
    label?: string
    hideHeader?: boolean
  }>(),
  { layout: 'block', label: '', hideHeader: false }
)

const { context, config, capability, issues, companionKey } = useEditorField(
  () => props.fieldKey
)
const editor = context.editor

const provenance = computed(
  () => context.provenance?.value[props.fieldKey] ?? []
)
const writerLabel = (source: string) => WRITER_LABELS[source] ?? source
const writer = computed(() => latestWriter(provenance.value))
const writerText = computed(() => {
  if (!writer.value) {
    return ''
  }
  const head = writer.value.human
    ? '人工维护，导入任务不会覆盖'
    : `当前值来自 ${writerLabel(writer.value.source)}`
  const history = provenance.value
    .flatMap((column) =>
      (column.history ?? []).map(
        (entry) =>
          `${provenance.value.length > 1 ? `${column.column}：` : ''}${writerLabel(entry.source)}${entry.at ? ` ${formatDateTime(entry.at)}` : ''}`
      )
    )
    .slice(0, 6)
  return [head, ...history].join('\n')
})

const label = computed(
  () => props.label || config.value?.label || fieldLabel(props.fieldKey)
)
const changed = computed(
  () =>
    editor.changedKeys.value.includes(props.fieldKey) ||
    editor.changedKeys.value.includes(companionKey.value)
)
const serverErrors = computed(() => [
  ...(editor.serverErrors.value[props.fieldKey] ?? []),
  ...(editor.serverErrors.value[companionKey.value] ?? [])
])

const lock = computed(() => {
  if (context.readonly) {
    return null
  }
  const cap = capability.value
  if (!cap) {
    return { icon: 'lucide:eye', text: '这一项只能查看' }
  }
  if (cap.locked) {
    return { icon: 'lucide:lock', text: '这一项已被锁定' }
  }
  if (!cap.can_propose) {
    return { icon: 'lucide:lock', text: '你没有修改这一项的权限' }
  }
  return null
})

const issueText = (issue: { index?: number; message: string }) =>
  issue.index === undefined
    ? issue.message
    : `第 ${issue.index + 1} 项：${issue.message}`
</script>

<template>
  <div
    :id="`field-${props.fieldKey}`"
    :class="
      cn(
        'relative scroll-mt-24',
        props.layout === 'row'
          ? 'grid gap-x-6 gap-y-1.5 py-3 sm:grid-cols-[9rem_minmax(0,1fr)]'
          : 'py-4',
        changed &&
          'before:bg-primary-500 before:absolute before:inset-y-3 before:-left-3 before:w-0.5 before:rounded-full'
      )
    "
  >
    <div
      v-if="!props.hideHeader"
      :class="
        cn(
          'flex min-w-0 gap-2',
          props.layout === 'row'
            ? 'items-center sm:min-h-9'
            : 'mb-3 items-start justify-between'
        )
      "
    >
      <div class="min-w-0">
        <div class="flex min-w-0 items-center gap-1.5">
          <component
            :is="props.layout === 'row' ? 'span' : 'h3'"
            :class="
              cn(
                'truncate',
                props.layout === 'row'
                  ? 'text-default-600 text-sm'
                  : 'text-foreground text-sm font-semibold'
              )
            "
          >
            {{ label }}
          </component>
          <KunTooltip
            v-if="props.layout === 'row' && config?.help"
            :text="config.help"
            position="top"
          >
            <KunIcon
              name="lucide:circle-help"
              class="text-default-400 size-3.5 shrink-0"
            />
          </KunTooltip>
          <KunTooltip v-if="lock" :text="lock.text" position="top">
            <KunIcon :name="lock.icon" class="text-default-400 size-3.5" />
          </KunTooltip>
          <KunTooltip v-if="writer" :text="writerText" position="top">
            <KunIcon
              :name="writer.human ? 'lucide:user-pen' : 'lucide:database'"
              :class="
                cn(
                  'size-3.5 shrink-0',
                  writer.human ? 'text-secondary-500' : 'text-default-400'
                )
              "
            />
          </KunTooltip>
          <KunTooltip v-if="changed" text="撤销这一项的修改" position="top">
            <button
              type="button"
              class="text-primary-600 hover:bg-primary-50 inline-flex shrink-0 items-center gap-0.5 rounded px-1 text-xs"
              @click="editor.revertField(props.fieldKey)"
            >
              <KunIcon name="lucide:undo-2" class="size-3.5" />
              <span v-if="props.layout === 'block'">撤销</span>
            </button>
          </KunTooltip>
        </div>
        <p
          v-if="props.layout === 'block' && config?.help"
          class="text-default-500 mt-1 text-xs leading-5"
        >
          {{ config.help }}
        </p>
      </div>
      <div
        v-if="props.layout === 'block' && $slots.actions"
        class="flex shrink-0 items-center gap-1"
      >
        <slot name="actions" />
      </div>
    </div>

    <div class="min-w-0">
      <slot />
      <ul
        v-if="issues.length || serverErrors.length"
        class="text-danger-600 mt-2 space-y-1 text-xs"
      >
        <li
          v-for="(issue, i) in issues"
          :key="`c${i}`"
          class="flex items-start gap-1"
        >
          <KunIcon
            name="lucide:circle-alert"
            class="mt-0.5 size-3.5 shrink-0"
          />
          {{ issueText(issue) }}
        </li>
        <li
          v-for="(message, i) in serverErrors"
          :key="`s${i}`"
          class="flex items-start gap-1"
        >
          <KunIcon
            name="lucide:server-crash"
            class="mt-0.5 size-3.5 shrink-0"
          />
          {{ message }}
        </li>
      </ul>
    </div>
  </div>
</template>
