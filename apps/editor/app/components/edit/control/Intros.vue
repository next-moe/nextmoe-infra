<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { EditUpstreamRow } from '#shared/types/edit'
import { vocabLabel } from '~/constants/vocabLabels'

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

type Row = { lang: string; intro: string }

const rows = computed<Row[]>(() =>
  Array.isArray(value.value) ? (value.value as Row[]) : []
)
const baseByLang = computed(
  () =>
    new Map(
      (Array.isArray(baseValue.value) ? (baseValue.value as Row[]) : []).map(
        (row) => [row.lang, row.intro]
      )
    )
)
const langMember = computed(() =>
  field.value?.element?.members?.find((member) => member.key === 'lang')
)
const langSlot = computed(() =>
  langMember.value ? memberVocabSlot(langMember.value) : null
)

const upstream = computed<EditUpstreamRow[]>(
  () => context.upstreamRows.value[props.fieldKey] ?? []
)

const update = (next: Row[]) => {
  value.value = next
}

const setAt = (index: number, patch: Partial<Row>) => {
  update(rows.value.map((row, i) => (i === index ? { ...row, ...patch } : row)))
}

const usedLangs = computed(() => new Set(rows.value.map((row) => row.lang)))
const newLang = ref<unknown>(null)

const addLang = () => {
  const lang = typeof newLang.value === 'string' ? newLang.value : ''
  if (!lang || usedLangs.value.has(lang)) {
    return
  }
  update([...rows.value, { lang, intro: '' }])
  newLang.value = null
}

const useAsDraft = (row: EditUpstreamRow) => {
  const lang = row.lang ?? ''
  const index = rows.value.findIndex((entry) => entry.lang === lang)
  if (index >= 0) {
    setAt(index, { intro: row.text ?? '' })
  } else {
    update([...rows.value, { lang, intro: row.text ?? '' }])
  }
}

const rowChanged = (row: Row) => baseByLang.value.get(row.lang) !== row.intro
const rowIssues = (index: number) =>
  issues.value.filter((issue) => issue.index === index)
</script>

<template>
  <div class="space-y-3">
    <article
      v-for="(row, index) in rows"
      :key="row.lang || index"
      :class="
        cn(
          'border-default-200 rounded-lg border p-3',
          rowChanged(row) && 'border-primary-300'
        )
      "
    >
      <header class="mb-2 flex items-center justify-between gap-2">
        <div class="flex items-center gap-2">
          <KunChip size="sm" variant="flat" color="primary">
            {{ vocabLabel(langSlot?.vocabulary ?? 'intro_lang', row.lang) }}
          </KunChip>
          <span class="text-default-400 text-xs tabular-nums">
            {{ [...(row.intro ?? '')].length }} 字
          </span>
        </div>
        <KunButton
          v-if="!disabled"
          size="xs"
          variant="light"
          color="danger"
          @click="update(rows.filter((_, i) => i !== index))"
        >
          <KunIcon name="lucide:trash-2" class="size-3.5" />
          删除这段
        </KunButton>
      </header>
      <KunTextarea
        :model-value="row.intro"
        :aria-label="`${vocabLabel('intro_lang', row.lang)}简介`"
        :disabled="disabled"
        max-height="32rem"
        @update:model-value="setAt(index, { intro: String($event) })"
      />
      <p
        v-for="(issue, i) in rowIssues(index)"
        :key="i"
        class="text-danger-600 mt-1 text-xs"
      >
        {{ issue.message }}
      </p>
    </article>

    <p v-if="!rows.length" class="text-default-400 text-sm">
      还没有人工撰写的简介。
    </p>

    <form
      v-if="!disabled && langSlot"
      class="flex flex-wrap items-center gap-2"
      @submit.prevent="addLang"
    >
      <EditVocabSelect
        v-model="newLang"
        :vocab="langSlot"
        aria-label="新简介的语言"
        class="w-48"
      />
      <KunButton
        type="submit"
        variant="flat"
        size="sm"
        :disabled="!newLang || usedLangs.has(String(newLang))"
      >
        <KunIcon name="lucide:plus" class="size-4" />
        添加这种语言的简介
      </KunButton>
    </form>

    <div v-if="upstream.length" class="border-default-200 rounded-lg border">
      <p class="text-default-600 flex items-center gap-1.5 px-3 py-2 text-sm">
        <KunIcon name="lucide:database" class="size-4" />
        资料源简介
        <span class="text-default-400 tabular-nums">{{ upstream.length }}</span>
      </p>
      <ul class="divide-default-100 border-default-200 divide-y border-t">
        <li v-for="row in upstream" :key="row.key" class="space-y-1 px-3 py-2">
          <div class="flex items-center justify-between gap-2">
            <span class="text-sm font-medium">
              {{ row.label }}
              <KunChip
                v-if="row.source"
                size="xs"
                variant="flat"
                class-name="ml-1"
              >
                {{ row.source }}
              </KunChip>
            </span>
            <div class="flex shrink-0 items-center gap-1">
              <KunButton
                v-if="!disabled"
                size="xs"
                variant="light"
                color="primary"
                @click="useAsDraft(row)"
              >
                以此为底稿
              </KunButton>
              <KunButton
                v-if="!disabled && companion && row.identity"
                size="xs"
                variant="light"
                :color="
                  suppressed.includes(row.identity) ? 'primary' : 'danger'
                "
                @click="toggleSuppressed(row.identity)"
              >
                {{ suppressed.includes(row.identity) ? '恢复' : '隐藏' }}
              </KunButton>
            </div>
          </div>
          <p
            class="text-default-500 line-clamp-3 text-xs leading-5 whitespace-pre-line"
          >
            {{ row.text }}
          </p>
        </li>
      </ul>
    </div>
    <EditSuppressedList :field-key="props.fieldKey" />
  </div>
</template>
