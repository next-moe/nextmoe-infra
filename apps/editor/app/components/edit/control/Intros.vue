<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { EditUpstreamRow } from '#shared/types/edit'
import { vocabLabel } from '~/constants/vocabLabels'
import { introLanguages } from '~/utils/languageCoverage'

const props = defineProps<{ fieldKey: string }>()

const {
  context,
  value,
  baseValue,
  disabled,
  field,
  issues,
  companion,
  suppressed,
  toggleSuppressed
} = useEditorField(() => props.fieldKey)
const editor = context.editor
const { byName } = useVocabularies()

type Row = { lang: string; intro: string }

const rows = computed<Row[]>(() =>
  Array.isArray(value.value) ? (value.value as Row[]) : []
)
const baseRows = computed<Row[]>(() =>
  Array.isArray(baseValue.value) ? (baseValue.value as Row[]) : []
)
const baseSuppressed = computed<string[]>(() => {
  const raw = editor.base.value[`${props.fieldKey}.suppressed`]
  return Array.isArray(raw) ? raw.map(String) : []
})
const upstream = computed<EditUpstreamRow[]>(
  () => context.upstreamRows.value[props.fieldKey] ?? []
)
const isHidden = (row: EditUpstreamRow) =>
  Boolean(row.identity && suppressed.value.includes(row.identity))

const olang = computed(() => {
  const raw = editor.draft.value[`${context.config.entityType}.olang`]
  return typeof raw === 'string' ? raw : null
})

const langSlot = computed(() => {
  const member = field.value?.element?.members?.find((m) => m.key === 'lang')
  return member ? memberVocabSlot(member) : null
})
const langLabel = (lang: string) =>
  vocabLabel(langSlot.value?.vocabulary ?? 'intro_lang', lang)

const extraLangs = ref<string[]>([])
const languages = computed(() =>
  introLanguages({
    olang: olang.value,
    human: rows.value,
    upstream: upstream.value.map((row) => ({
      lang: row.lang ?? '',
      hidden: isHidden(row),
      source: upstreamSourceLabel(row),
      machine: row.machine
    })),
    extra: [...extraLangs.value, ...baseRows.value.map((row) => row.lang)]
  })
)

const selected = ref('')
watch(
  languages,
  (list) => {
    if (list.some((entry) => entry.lang === selected.value)) {
      return
    }
    selected.value =
      list.find((entry) => entry.status === 'human')?.lang ??
      list.find((entry) => entry.status === 'upstream')?.lang ??
      list[0]?.lang ??
      ''
  },
  { immediate: true }
)

const current = computed(() =>
  languages.value.find((entry) => entry.lang === selected.value)
)
const humanIndex = computed(() =>
  rows.value.findIndex((row) => row.lang === selected.value)
)
const humanRow = computed(() =>
  humanIndex.value >= 0 ? rows.value[humanIndex.value] : undefined
)
const baseHuman = computed(() =>
  baseRows.value.find((row) => row.lang === selected.value)
)
const versions = computed(() =>
  upstream.value.filter((row) => (row.lang ?? '') === selected.value)
)
const shownVersion = computed(() =>
  humanRow.value ? undefined : versions.value.find((row) => !isHidden(row))
)

const langHasIssue = (lang: string) => {
  const index = rows.value.findIndex((row) => row.lang === lang)
  return index >= 0 && issues.value.some((issue) => issue.index === index)
}

const langChanged = (lang: string) => {
  const draft = rows.value.find((row) => row.lang === lang)?.intro
  const base = baseRows.value.find((row) => row.lang === lang)?.intro
  if (draft !== base) {
    return true
  }
  return upstream.value.some(
    (row) =>
      (row.lang ?? '') === lang &&
      row.identity &&
      suppressed.value.includes(row.identity) !==
        baseSuppressed.value.includes(row.identity)
  )
}

const update = (next: Row[]) => {
  value.value = next
}
const setIntro = (text: string) => {
  if (humanIndex.value >= 0) {
    update(
      rows.value.map((row, i) =>
        i === humanIndex.value ? { ...row, intro: text } : row
      )
    )
  } else {
    update([...rows.value, { lang: selected.value, intro: text }])
  }
}
const startWriting = async (text = '') => {
  setIntro(text)
  await nextTick()
  document
    .querySelector<HTMLTextAreaElement>(
      `[data-intro-editor="${props.fieldKey}"] textarea`
    )
    ?.focus()
}
const removeHuman = () => {
  update(rows.value.filter((_, i) => i !== humanIndex.value))
}
const restoreHuman = () => {
  if (baseHuman.value) {
    update([...rows.value, cloneValue(baseHuman.value)])
  }
}

const useAsDraft = async (row: EditUpstreamRow) => {
  const text = row.text ?? ''
  const existing = humanRow.value?.intro ?? ''
  if (existing.trim() && existing !== text) {
    const ok = await useKunAlert({
      title: '用这个版本替换人工简介',
      message: '当前的人工简介会被替换为这个资料源版本，之后仍可继续修改。',
      type: 'warning',
      confirmText: '替换',
      cancelText: '取消'
    })
    if (!ok) {
      return
    }
  }
  startWriting(text)
}

const expanded = ref<Record<string, boolean>>({})

const addOptions = computed(() => {
  const vocab = langSlot.value ? byName.value[langSlot.value.vocabulary] : null
  const present = new Set(languages.value.map((entry) => entry.lang))
  return (vocab?.values ?? [])
    .filter((entry) => !present.has(entry.value))
    .map((entry) => ({
      value: entry.value,
      label: vocabLabel(
        langSlot.value?.vocabulary ?? 'intro_lang',
        entry.value,
        entry.display_name
      )
    }))
})
const addPick = ref<string | null>(null)
watch(addPick, (lang) => {
  if (!lang) {
    return
  }
  extraLangs.value = [...extraLangs.value, lang]
  selected.value = lang
  addPick.value = null
})

const charCount = (text: string) => [...text].length
const humanIssues = computed(() =>
  issues.value.filter((issue) => issue.index === humanIndex.value)
)
</script>

<template>
  <div class="@container space-y-4">
    <div
      role="tablist"
      :aria-label="`${field?.key ?? ''} 语言`"
      class="flex flex-wrap items-center gap-1.5"
    >
      <button
        v-for="entry in languages"
        :key="entry.lang"
        type="button"
        role="tab"
        :aria-selected="selected === entry.lang"
        :class="
          cn(
            'relative inline-flex h-9 items-center gap-2 rounded-lg border px-3 text-sm transition-colors',
            selected === entry.lang
              ? 'border-primary-400 bg-primary-50 text-primary-700'
              : entry.status === 'missing'
                ? 'border-default-300 text-default-500 hover:border-default-400 hover:text-foreground border-dashed'
                : 'border-default-200 bg-content1 hover:bg-default-50'
          )
        "
        @click="selected = entry.lang"
      >
        <span class="font-medium">{{ langLabel(entry.lang) }}</span>
        <span
          :class="
            cn(
              'rounded px-1.5 text-[11px] leading-5',
              entry.status === 'human' && 'bg-secondary-100 text-secondary-700',
              entry.status === 'upstream' && 'bg-default-100 text-default-600',
              entry.status === 'missing' && 'text-default-400'
            )
          "
        >
          {{
            entry.status === 'human'
              ? '人工'
              : entry.status === 'upstream'
                ? `${entry.displaySource ?? '资料源'}${entry.machine && entry.displaySource !== '机翻' ? ' · 机翻' : ''}`
                : '缺失'
          }}
        </span>
        <span
          v-if="langHasIssue(entry.lang)"
          class="bg-danger-500 ring-content1 absolute -top-1 -right-1 size-2.5 rounded-full ring-2"
          aria-label="需要修正"
        />
        <span
          v-else-if="langChanged(entry.lang)"
          class="bg-primary-500 ring-content1 absolute -top-1 -right-1 size-2.5 rounded-full ring-2"
          aria-label="有修改"
        />
      </button>
      <KunSelect
        v-if="!disabled && addOptions.length"
        v-model="addPick"
        class="w-40"
        :options="addOptions"
        searchable
        size="sm"
        aria-label="添加其他语言"
        placeholder="+ 其他语言"
      />
    </div>

    <div
      v-if="current"
      class="grid gap-5 @4xl:grid-cols-[minmax(0,1fr)_minmax(16rem,22rem)]"
    >
      <div class="min-w-0 space-y-3" :data-intro-editor="props.fieldKey">
        <p class="text-default-500 flex flex-wrap items-center gap-1.5 text-sm">
          <KunIcon name="lucide:monitor" class="size-4" />
          页面上显示：
          <span v-if="humanRow" class="text-foreground">
            下面的人工简介（优先于资料源）
          </span>
          <span v-else-if="shownVersion" class="text-foreground">
            {{ upstreamSourceLabel(shownVersion) }}
            的版本{{ shownVersion.machine ? '（机器翻译）' : '' }}
          </span>
          <span v-else class="text-warning-700"> 这个语言暂无简介 </span>
        </p>

        <template v-if="humanRow">
          <KunTextarea
            :model-value="humanRow.intro"
            :aria-label="`${langLabel(selected)}简介`"
            :disabled="disabled"
            :rows="8"
            auto-grow
            :placeholder="`撰写${langLabel(selected)}简介`"
            @update:model-value="setIntro(String($event))"
          />
          <div class="flex flex-wrap items-center gap-3 text-xs">
            <span class="text-default-400 tabular-nums">
              {{ charCount(humanRow.intro) }} 字
            </span>
            <span v-if="!baseHuman" class="text-success-700">
              新增的人工简介
            </span>
            <span
              v-else-if="baseHuman.intro !== humanRow.intro"
              class="text-primary-600"
            >
              已修改
            </span>
            <span class="flex-1" />
            <KunButton
              v-if="
                baseHuman && baseHuman.intro !== humanRow.intro && !disabled
              "
              size="xs"
              variant="light"
              color="default"
              @click="setIntro(baseHuman.intro)"
            >
              <KunIcon name="lucide:undo-2" class="size-3.5" />
              撤销修改
            </KunButton>
            <KunButton
              v-if="!disabled"
              size="xs"
              variant="light"
              color="danger"
              @click="removeHuman"
            >
              <KunIcon name="lucide:trash-2" class="size-3.5" />
              删除人工简介
            </KunButton>
          </div>
          <p
            v-for="(issue, i) in humanIssues"
            :key="i"
            class="text-danger-600 text-xs"
          >
            {{ issue.message }}
          </p>
        </template>

        <div
          v-else
          class="border-default-300 rounded-xl border border-dashed px-5 py-6"
        >
          <div v-if="baseHuman" class="flex flex-wrap items-center gap-3">
            <KunIcon name="lucide:trash-2" class="text-danger-500 size-5" />
            <p class="text-sm">
              {{
                langLabel(selected)
              }}的人工简介将被删除，页面会改为显示资料源版本。
            </p>
            <KunButton size="sm" variant="flat" @click="restoreHuman">
              <KunIcon name="lucide:undo-2" class="size-4" />
              撤销删除
            </KunButton>
          </div>
          <template v-else>
            <p class="text-foreground text-sm font-medium">
              还没有人工撰写的{{ langLabel(selected) }}简介
            </p>
            <p class="text-default-500 mt-1 text-xs leading-5">
              人工简介会优先于资料源显示，资料源再次同步也不会覆盖它。
            </p>
            <div v-if="!disabled" class="mt-3 flex flex-wrap gap-2">
              <KunButton size="sm" color="primary" @click="startWriting()">
                <KunIcon name="lucide:pen-line" class="size-4" />
                撰写简介
              </KunButton>
              <KunButton
                v-for="version in versions.filter((row) => row.text)"
                :key="version.key"
                size="sm"
                variant="flat"
                @click="useAsDraft(version)"
              >
                <KunIcon name="lucide:copy" class="size-4" />
                以{{ upstreamSourceLabel(version) }}版本为底稿
              </KunButton>
            </div>
          </template>
        </div>
      </div>

      <aside class="min-w-0 space-y-2">
        <p
          class="text-default-500 flex items-center gap-1.5 text-xs font-medium"
        >
          <KunIcon name="lucide:database" class="size-3.5" />
          资料源版本
          <span class="text-default-400 tabular-nums">{{
            versions.length
          }}</span>
        </p>
        <p
          v-if="!versions.length"
          class="border-default-200 text-default-400 rounded-lg border px-3 py-4 text-center text-xs"
        >
          资料源没有这个语言的简介。
        </p>
        <article
          v-for="version in versions"
          :key="version.key"
          :class="
            cn(
              'border-default-200 bg-content1 rounded-lg border p-3',
              shownVersion?.key === version.key && 'border-primary-300',
              isHidden(version) && 'border-dashed'
            )
          "
        >
          <header class="mb-1.5 flex flex-wrap items-center gap-1.5">
            <WorkspaceSourceBadge
              :source="upstreamSourceLabel(version)"
              :machine="
                version.machine && upstreamSourceLabel(version) !== '机翻'
              "
            />
            <span
              v-if="shownVersion?.key === version.key"
              class="text-primary-600 text-xs"
            >
              正在显示
            </span>
            <span
              v-else-if="isHidden(version)"
              class="text-default-400 inline-flex items-center gap-1 text-xs"
            >
              <KunIcon name="lucide:eye-off" class="size-3" />
              {{
                version.identity && baseSuppressed.includes(version.identity)
                  ? '已隐藏'
                  : '将隐藏'
              }}
            </span>
            <span class="text-default-400 ml-auto text-xs tabular-nums">
              {{ charCount(version.text ?? '') }} 字
            </span>
          </header>
          <p
            :class="
              cn(
                'text-default-600 text-xs leading-5 whitespace-pre-line',
                !expanded[version.key] && 'line-clamp-6',
                isHidden(version) && 'opacity-60'
              )
            "
          >
            {{ version.text }}
          </p>
          <div class="mt-2 flex flex-wrap items-center gap-1">
            <KunButton
              v-if="(version.text ?? '').length > 180"
              size="xs"
              variant="light"
              color="default"
              @click="expanded[version.key] = !expanded[version.key]"
            >
              {{ expanded[version.key] ? '收起' : '展开全文' }}
            </KunButton>
            <span class="flex-1" />
            <KunButton
              v-if="!disabled && version.text"
              size="xs"
              variant="light"
              color="primary"
              @click="useAsDraft(version)"
            >
              以此为底稿
            </KunButton>
            <KunButton
              v-if="!disabled && companion && version.identity"
              size="xs"
              variant="light"
              :color="isHidden(version) ? 'primary' : 'default'"
              @click="toggleSuppressed(version.identity)"
            >
              <KunIcon
                :name="isHidden(version) ? 'lucide:eye' : 'lucide:eye-off'"
                class="size-3.5"
              />
              {{ isHidden(version) ? '恢复' : '隐藏' }}
            </KunButton>
          </div>
        </article>
      </aside>
    </div>
  </div>
</template>
