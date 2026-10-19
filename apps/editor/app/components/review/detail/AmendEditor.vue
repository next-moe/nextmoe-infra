<script setup lang="ts">
import type { KunSelectOption } from '@kungal/ui-vue'
import type { CatalogSchemaField } from '#shared/types/catalog'
import type { DiffContext } from '~/utils/review/context'
import { vocabularyLabel } from '~/utils/review/describe'
import { cloneValue } from '~/utils/review/equal'
import { asList } from '~/utils/review/listDiff'

const props = defineProps<{
  field: CatalogSchemaField
  context: DiffContext
}>()

const model = defineModel<unknown>({ required: true })

const isList = computed(() => props.field.field_type === 'list')
const jsonMode = ref(false)
const jsonText = ref('')
const jsonError = ref('')

const vocabulary = computed(() =>
  props.field.vocabulary
    ? props.context.vocabulary(props.field.vocabulary)
    : undefined
)

const enumOptions = computed<KunSelectOption<string | number>[]>(() => {
  const values = vocabulary.value?.values ?? []
  const options = values.map((value, index) => {
    const wire =
      props.field.encoding === 'int' ? props.field.base + index : value.value
    return {
      value: wire,
      label: vocabularyLabel(
        wire,
        vocabulary.value,
        props.field.encoding,
        props.field.base
      )
    }
  })
  return props.field.nullable
    ? [{ value: '', label: '（清空）' }, ...options]
    : options
})

const enumValue = computed({
  get: () => (model.value ?? '') as string | number,
  set: (value) => {
    model.value = value === '' ? null : value
  }
})

const textValue = computed({
  get: () =>
    model.value === null || model.value === undefined
      ? ''
      : String(model.value),
  set: (value: string) => {
    if (props.field.field_type === 'int' || props.field.field_type === 'ref') {
      const trimmed = value.trim()
      model.value = trimmed === '' ? null : Number(trimmed)
    } else {
      model.value = value === '' && props.field.nullable ? null : value
    }
  }
})

const multiline = computed(
  () =>
    props.field.diff_hint === 'lines' || String(model.value ?? '').length > 80
)

const items = computed(() => asList(model.value))

const removeAt = (index: number) => {
  const next = [...items.value]
  next.splice(index, 1)
  model.value = next
}

const move = (index: number, delta: number) => {
  const target = index + delta
  if (target < 0 || target >= items.value.length) {
    return
  }
  const next = [...items.value]
  const [item] = next.splice(index, 1)
  next.splice(target, 0, item)
  model.value = next
}

const openJson = () => {
  jsonText.value = JSON.stringify(
    model.value ?? (isList.value ? [] : null),
    null,
    2
  )
  jsonError.value = ''
  jsonMode.value = true
}

const applyJson = () => {
  try {
    const parsed = JSON.parse(jsonText.value) as unknown
    if (isList.value && !Array.isArray(parsed)) {
      jsonError.value = '这个字段需要一个 JSON 数组'
      return
    }
    model.value = cloneValue(parsed)
    jsonError.value = ''
    jsonMode.value = false
  } catch (error) {
    jsonError.value = `JSON 无法解析：${(error as Error).message}`
  }
}
</script>

<template>
  <div class="space-y-2">
    <template v-if="jsonMode">
      <KunTextarea
        v-model="jsonText"
        :error="jsonError"
        max-height="24rem"
        aria-label="以 JSON 编辑字段值"
        class-name="font-mono text-xs"
      />
      <div class="flex gap-2">
        <KunButton size="sm" @click="applyJson">应用</KunButton>
        <KunButton size="sm" variant="light" @click="jsonMode = false"
          >取消</KunButton
        >
      </div>
    </template>

    <template v-else-if="isList">
      <ul class="space-y-1">
        <li
          v-for="(item, index) in items"
          :key="index"
          class="border-default-200 flex items-start gap-2 rounded-md border px-2 py-1.5"
        >
          <DiffElement
            :field="props.field"
            :item="item"
            :context="props.context"
            class="min-w-0 flex-1 text-sm"
          />
          <div class="flex shrink-0 gap-0.5">
            <KunButton
              size="xs"
              variant="light"
              is-icon-only
              aria-label="上移"
              :disabled="index === 0"
              @click="move(index, -1)"
            >
              <KunIcon name="lucide:arrow-up" class="size-3.5" />
            </KunButton>
            <KunButton
              size="xs"
              variant="light"
              is-icon-only
              aria-label="下移"
              :disabled="index === items.length - 1"
              @click="move(index, 1)"
            >
              <KunIcon name="lucide:arrow-down" class="size-3.5" />
            </KunButton>
            <KunButton
              size="xs"
              variant="light"
              color="danger"
              is-icon-only
              aria-label="移除这一项"
              @click="removeAt(index)"
            >
              <KunIcon name="lucide:x" class="size-3.5" />
            </KunButton>
          </div>
        </li>
      </ul>
      <KunNull
        v-if="!items.length"
        description="修正后这个列表为空"
        :is-show-sticker="false"
      />
      <KunButton size="sm" variant="bordered" @click="openJson">
        <KunIcon name="lucide:braces" class="size-3.5" />
        以 JSON 编辑（可新增条目）
      </KunButton>
    </template>

    <KunSelect
      v-else-if="props.field.vocabulary"
      v-model="enumValue"
      :options="enumOptions"
      :aria-label="fieldLabel(props.field.key)"
    />

    <KunTextarea
      v-else-if="props.field.field_type === 'text' && multiline"
      v-model="textValue"
      :aria-label="fieldLabel(props.field.key)"
      max-height="24rem"
    />

    <div v-else class="flex items-center gap-2">
      <KunInput
        v-model="textValue"
        :type="
          props.field.field_type === 'int' || props.field.field_type === 'ref'
            ? 'number'
            : 'text'
        "
        :placeholder="props.field.field_type === 'date' ? 'YYYY-MM-DD' : ''"
        :aria-label="fieldLabel(props.field.key)"
        class-name="flex-1"
      />
      <KunButton size="sm" variant="light" @click="openJson">JSON</KunButton>
    </div>
  </div>
</template>
