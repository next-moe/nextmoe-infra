<script setup lang="ts">
import type { EditRefFamily } from '#shared/types/edit'
import type { RefOption } from '~/composables/useRefNames'

const props = withDefaults(
  defineProps<{
    family: EditRefFamily
    modelValue?: string | null
    placeholder?: string
    disabled?: boolean
    exclude?: string[]
    ariaLabel?: string
  }>(),
  {
    modelValue: null,
    placeholder: '',
    disabled: false,
    exclude: () => [],
    ariaLabel: ''
  }
)

const emit = defineEmits<{
  'update:modelValue': [value: string | null]
  select: [option: RefOption]
}>()

const refs = useRefNames()
const query = ref('')
const options = ref<RefOption[]>([])
const searching = ref(false)
const roleOptions = ref<RefOption[]>([])

const isRole = computed(() => props.family === 'role')
const selectedName = computed(() => refs.nameOf(props.family, props.modelValue))
const familyLabel = computed(() => REF_FAMILY_LABEL[props.family])

let token = 0
const onSearch = async (text: string) => {
  const mine = ++token
  searching.value = true
  const found = await refs.search(props.family, text)
  if (mine !== token) {
    return
  }
  options.value = found.filter(
    (option) => !props.exclude.includes(option.value)
  )
  searching.value = false
}

const autocompleteOptions = computed(() =>
  options.value.map((option) => ({
    value: option.value,
    label: option.hint ? `${option.label}（${option.hint}）` : option.label
  }))
)

const onSelect = (picked: { value: string }) => {
  const option = options.value.find((o) => o.value === picked.value)
  if (!option) {
    return
  }
  refs.remember(props.family, option.value, option.label)
  emit('update:modelValue', option.value)
  emit('select', option)
  query.value = ''
  options.value = []
}

const roleModel = computed({
  get: () => props.modelValue ?? null,
  set: (value: string | null) => {
    emit('update:modelValue', value)
    const option = roleOptions.value.find((o) => o.value === value)
    if (option) {
      emit('select', option)
    }
  }
})

onMounted(async () => {
  if (isRole.value) {
    roleOptions.value = await refs.loadRoles()
  } else if (props.modelValue) {
    await refs.resolve(props.family, [props.modelValue])
  }
})
</script>

<template>
  <div class="min-w-0">
    <KunSelect
      v-if="isRole"
      v-model="roleModel"
      :options="roleOptions"
      :placeholder="props.placeholder || `选择${familyLabel}`"
      :aria-label="props.ariaLabel || familyLabel"
      :disabled="props.disabled"
      searchable
      size="sm"
    />
    <div
      v-else-if="props.modelValue"
      class="border-default-200 bg-default-50 flex h-8 min-w-0 items-center gap-1.5 rounded-lg border px-2 text-sm"
    >
      <span class="min-w-0 flex-1 truncate" :title="selectedName">
        {{ selectedName || '加载中…' }}
      </span>
      <span class="text-default-400 shrink-0 font-mono text-xs">
        #{{ props.modelValue }}
      </span>
      <button
        v-if="!props.disabled"
        type="button"
        class="text-default-400 hover:text-danger-600 shrink-0"
        :aria-label="`清除${familyLabel}`"
        @click="emit('update:modelValue', null)"
      >
        <KunIcon name="lucide:x" class="size-3.5" />
      </button>
    </div>
    <KunAutocomplete
      v-else
      v-model="query"
      :options="autocompleteOptions"
      :placeholder="props.placeholder || `搜索${familyLabel}名称或输入 ID`"
      :aria-label="props.ariaLabel || `搜索${familyLabel}`"
      :disabled="props.disabled"
      :loading="searching"
      :debounce="250"
      manual-filter
      size="sm"
      no-result-text="没有找到匹配的条目"
      @search="onSearch"
      @select="onSelect"
    />
  </div>
</template>
