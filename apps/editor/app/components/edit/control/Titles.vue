<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { KunDropdownItem } from '@kungal/ui-vue'
import type { CatalogSchemaMember } from '#shared/types/edit'
import { vocabLabel } from '~/constants/vocabLabels'
import { titleLanguages } from '~/utils/languageCoverage'
import { INLINE_FIELD_CLASS } from '~/utils/inlineField'
import { mergeRows, type MergedRow } from '~/utils/mergedRows'

const props = defineProps<{ fieldKey: string }>()

const {
  context,
  value,
  baseValue,
  disabled,
  field,
  issues,
  companion,
  suppressed
} = useEditorField(() => props.fieldKey)
const editor = context.editor
const { byName } = useVocabularies()

type Row = Record<string, unknown>

const draftRows = computed<Row[]>(() =>
  Array.isArray(value.value) ? (value.value as Row[]) : []
)
const baseRows = computed<Row[]>(() =>
  Array.isArray(baseValue.value) ? (baseValue.value as Row[]) : []
)
const identityOf = (row: unknown) => rowIdentity(props.fieldKey, row)

const merged = computed(() =>
  mergeRows({
    draft: draftRows.value,
    base: baseRows.value,
    upstream: [],
    suppressed: [],
    baseSuppressed: [],
    identityOf
  })
)

const memberOf = (key: string) =>
  field.value?.element?.members?.find((member) => member.key === key) as
    CatalogSchemaMember | undefined
const kindMember = computed(() => memberOf('kind'))
const langMember = computed(() => memberOf('lang'))
const kindSlot = computed(() =>
  kindMember.value ? memberVocabSlot(kindMember.value) : null
)
const kindWire = (token: string, fallback: number) => {
  const slot = kindSlot.value
  const vocab = slot ? byName.value[slot.vocabulary] : undefined
  const found =
    slot && vocab
      ? vocabWireValues(slot, vocab).find((entry) => entry.token === token)
      : undefined
  return found ? Number(found.wire) : fallback
}
const officialKind = computed(() => kindWire('official', 0))
const aliasKind = computed(() => kindWire('alias', 1))

const olang = computed(() => {
  const raw = editor.draft.value[`${context.config.entityType}.olang`]
  return typeof raw === 'string' ? raw : null
})
const displayName = computed(() => {
  const raw = editor.draft.value[`${context.config.entityType}.display_name`]
  return typeof raw === 'string' ? raw : ''
})

const coverage = computed(() =>
  titleLanguages({
    olang: olang.value,
    titles: draftRows.value,
    officialKind: officialKind.value
  })
)

const langLabel = (lang: string) =>
  lang ? vocabLabel('olang', lang) : '未标注语言'

const groups = computed(() =>
  coverage.value
    .filter((entry) => !entry.missing)
    .map((entry) => ({
      ...entry,
      rows: merged.value.filter(
        (row) => String((row.value as Row | null)?.lang ?? '') === entry.lang
      )
    }))
)

const removedIdentities = computed(
  () =>
    new Set(
      merged.value
        .filter((row) => row.status === 'removed')
        .map((row) => row.identity)
    )
)
const olderSuppressions = computed(() =>
  suppressed.value
    .filter((key) => !removedIdentities.value.has(key))
    .map((key) => ({ key, label: suppressedKeyParts(key).label }))
)
const unsuppress = (key: string) => {
  suppressed.value = suppressed.value.filter((entry) => entry !== key)
}

const update = (next: Row[]) => {
  value.value = next
}
const patchAt = (index: number, patch: Row) => {
  update(
    draftRows.value.map((row, i) => (i === index ? { ...row, ...patch } : row))
  )
}
const removeAt = (index: number) => {
  update(draftRows.value.filter((_, i) => i !== index))
}
const restore = (row: MergedRow) => {
  const insertAt = Math.min(row.index, draftRows.value.length)
  update([
    ...draftRows.value.slice(0, insertAt),
    cloneValue(row.base as Row),
    ...draftRows.value.slice(insertAt)
  ])
}
const undo = (row: MergedRow) => {
  if (row.status === 'changed') {
    patchAt(row.index, cloneValue(row.base as Row))
  }
}

const cap = computed(() => field.value?.max_elements ?? 0)
const focusIndex = ref<number | null>(null)
const addTitle = (lang: string) => {
  if (disabled.value || (cap.value && draftRows.value.length >= cap.value)) {
    return
  }
  const hasOfficial = draftRows.value.some(
    (row) => row.lang === lang && Number(row.kind) === officialKind.value
  )
  update([
    ...draftRows.value,
    {
      lang,
      title: '',
      kind: hasOfficial || !lang ? aliasKind.value : officialKind.value
    }
  ])
  focusIndex.value = draftRows.value.length
}

watch(focusIndex, async (index) => {
  if (index === null) {
    return
  }
  await nextTick()
  document
    .querySelector<HTMLInputElement>(`[data-title-index="${index}"] input`)
    ?.focus()
  focusIndex.value = null
})

const scrollToGroup = (lang: string) => {
  document
    .getElementById(`titles-${props.fieldKey}-${lang || 'none'}`)
    ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

const COMMON_MOVE = ['ja', 'zh-Hans', 'zh-Hant', 'en', 'ko']
const langOptions = computed(() => {
  const slot = langMember.value ? memberVocabSlot(langMember.value) : null
  const vocab = slot ? byName.value[slot.vocabulary] : undefined
  return (vocab?.values ?? []).map((entry) => entry.value)
})
const moveItems = (row: MergedRow): KunDropdownItem[] => {
  const current = String((row.value as Row | null)?.lang ?? '')
  const common = [...new Set([olang.value ?? 'ja', ...COMMON_MOVE])]
  const rest = langOptions.value.filter((lang) => !common.includes(lang))
  return [
    ...common
      .filter((lang) => lang !== current)
      .map((lang) => ({ key: lang, label: langLabel(lang) })),
    ...(rest.length
      ? [
          {
            key: '__more',
            label: '更多语言',
            children: rest
              .filter((lang) => lang !== current)
              .map((lang) => ({ key: lang, label: langLabel(lang) }))
          }
        ]
      : []),
    ...(current
      ? [{ key: '__none', label: '未标注语言', icon: 'lucide:circle-slash' }]
      : [])
  ] as KunDropdownItem[]
}
const moveTo = (row: MergedRow, item: KunDropdownItem) => {
  patchAt(row.index, { lang: item.key === '__none' ? '' : item.key })
}

const memberInvalid = (index: number, key: string) =>
  issues.value.some((issue) => issue.index === index && issue.member === key)
const MEMBER_LABEL: Record<string, string> = {
  title: '标题',
  lang: '语言',
  kind: '类型',
  latin: '罗马字'
}
const rowIssues = (index: number) =>
  issues.value
    .filter((issue) => issue.index === index)
    .map((issue) => ({
      message: issue.member
        ? `${MEMBER_LABEL[issue.member] ?? issue.member}：${issue.message}`
        : issue.message
    }))

const isDisplayTitle = (row: MergedRow) => {
  const v = row.value as Row | null
  return (
    Boolean(displayName.value) &&
    v?.title === displayName.value &&
    v?.lang === olang.value &&
    Number(v?.kind) === officialKind.value
  )
}
</script>

<template>
  <div class="@container space-y-5">
    <div class="flex flex-wrap gap-2">
      <button
        v-for="entry in coverage"
        :key="entry.lang || 'none'"
        type="button"
        :disabled="entry.missing && disabled"
        :class="
          cn(
            'inline-flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-sm transition-colors',
            entry.missing
              ? 'border-default-300 text-default-500 hover:border-primary-400 hover:text-primary-600 border-dashed'
              : 'border-default-200 bg-content1 hover:bg-default-50',
            entry.isOriginal &&
              !entry.missing &&
              'border-primary-300 bg-primary-50'
          )
        "
        @click="
          entry.missing ? addTitle(entry.lang) : scrollToGroup(entry.lang)
        "
      >
        <KunIcon v-if="entry.missing" name="lucide:plus" class="size-3.5" />
        {{ langLabel(entry.lang) }}
        <span v-if="entry.isOriginal" class="text-primary-600 text-[11px]">
          原语言
        </span>
        <span v-if="!entry.missing" class="text-default-400 tabular-nums">
          {{ entry.count }}
        </span>
        <KunTooltip
          v-if="!entry.missing && !entry.official && entry.lang"
          text="这种语言还没有正式名"
          position="top"
        >
          <KunIcon
            name="lucide:circle-alert"
            class="text-warning-500 size-3.5"
          />
        </KunTooltip>
      </button>
    </div>

    <section
      v-for="group in groups"
      :id="`titles-${props.fieldKey}-${group.lang || 'none'}`"
      :key="group.lang || 'none'"
      class="scroll-mt-24"
    >
      <header class="mb-1 flex items-center gap-2 px-2">
        <h4 class="text-sm font-medium">{{ langLabel(group.lang) }}</h4>
        <span class="text-default-400 text-xs tabular-nums">
          {{ group.count }}
        </span>
        <span class="flex-1" />
        <KunButton
          v-if="!disabled"
          size="xs"
          variant="light"
          @click="addTitle(group.lang)"
        >
          <KunIcon name="lucide:plus" class="size-3.5" />
          添加
        </KunButton>
      </header>
      <ul class="space-y-0.5">
        <li
          v-for="row in group.rows"
          :key="row.key"
          :data-title-index="row.index"
        >
          <EditListRow
            :row="row"
            :can-remove="!disabled"
            hide-source
            remove-label="删除这个标题（导入的标题会被屏蔽）"
            trail-class-name="w-auto justify-end"
            @remove="removeAt(row.index)"
            @restore="restore(row)"
            @undo="undo(row)"
          >
            <div
              v-if="row.status !== 'removed' && !disabled"
              class="grid min-w-0 gap-2 @2xl:grid-cols-[minmax(0,1fr)_8rem_11rem] @2xl:items-center"
            >
              <div class="flex min-w-0 items-center gap-2">
                <KunInput
                  :model-value="String((row.value as Row).title ?? '')"
                  aria-label="标题"
                  placeholder="标题原文"
                  :is-invalid="memberInvalid(row.index, 'title')"
                  :class-name="
                    memberInvalid(row.index, 'title') ? '' : INLINE_FIELD_CLASS
                  "
                  size="sm"
                  class="min-w-0 flex-1"
                  @update:model-value="
                    patchAt(row.index, { title: String($event) })
                  "
                />
                <KunTooltip
                  v-if="isDisplayTitle(row)"
                  text="原语言的正式名，作为列表与页面的显示名"
                  position="top"
                >
                  <KunChip size="xs" variant="flat" color="primary"
                    >显示名</KunChip
                  >
                </KunTooltip>
              </div>
              <EditVocabSelect
                v-if="kindSlot"
                :model-value="(row.value as Row).kind"
                :vocab="kindSlot"
                aria-label="标题类型"
                inline
                @update:model-value="patchAt(row.index, { kind: $event })"
              />
              <KunInput
                :model-value="String((row.value as Row).latin ?? '')"
                aria-label="罗马字"
                placeholder="罗马字"
                :class-name="INLINE_FIELD_CLASS"
                size="sm"
                @update:model-value="
                  patchAt(row.index, { latin: String($event) || undefined })
                "
              />
            </div>
            <div
              v-else
              class="flex min-w-0 items-center gap-2 text-sm leading-8"
            >
              <span class="truncate">{{ (row.value as Row).title }}</span>
              <span class="text-default-400 shrink-0 text-xs">
                {{
                  vocabLabel(
                    'title_kind',
                    ['official', 'alias', 'abbreviation'][
                      Number((row.value as Row).kind)
                    ] ?? ''
                  )
                }}
              </span>
              <span
                v-if="(row.value as Row).latin"
                class="text-default-400 truncate text-xs"
              >
                {{ (row.value as Row).latin }}
              </span>
            </div>
            <p
              v-for="(issue, i) in row.status !== 'removed'
                ? rowIssues(row.index)
                : []"
              :key="i"
              class="text-danger-600 mt-1 text-xs"
            >
              {{ issue.message }}
            </p>
            <template v-if="!disabled && row.status !== 'removed'" #actions>
              <KunDropdown
                :items="moveItems(row)"
                position="bottom-end"
                @select="moveTo(row, $event)"
              >
                <template #trigger>
                  <KunButton
                    size="xs"
                    variant="light"
                    color="default"
                    is-icon-only
                    aria-label="移到其他语言"
                  >
                    <KunIcon name="lucide:languages" class="size-3.5" />
                  </KunButton>
                </template>
              </KunDropdown>
            </template>
          </EditListRow>
        </li>
      </ul>
    </section>

    <WorkspaceEmpty
      v-if="!groups.length"
      icon="lucide:heading"
      title="还没有标题"
      description="点上方的语言即可添加这种语言的标题。"
    />

    <section
      v-if="companion && olderSuppressions.length"
      class="border-default-200 bg-default-50 rounded-lg border px-3 py-2"
    >
      <p class="text-default-600 mb-1 flex items-center gap-1.5 text-sm">
        <KunIcon name="lucide:eye-off" class="size-4" />
        已屏蔽的导入标题
        <span class="text-default-400 tabular-nums">
          {{ olderSuppressions.length }}
        </span>
      </p>
      <ul class="divide-default-200 divide-y">
        <li
          v-for="entry in olderSuppressions"
          :key="entry.key"
          class="flex items-center gap-2 py-1.5 text-sm"
        >
          <span class="text-default-500 min-w-0 flex-1 break-all">
            {{ entry.label || entry.key }}
          </span>
          <KunButton
            v-if="!disabled"
            size="xs"
            variant="light"
            color="primary"
            @click="unsuppress(entry.key)"
          >
            取消屏蔽
          </KunButton>
        </li>
      </ul>
    </section>
  </div>
</template>
