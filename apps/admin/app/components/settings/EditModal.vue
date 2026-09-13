<script setup lang="ts">
import type { SettingsKeyView, SettingValue } from '~~/shared/types/settings'
import { PROPAGATION_NOTE, formatSettingValue } from '~/constants/settings'

const props = defineProps<{
  row: SettingsKeyView | null
  submitting: boolean
  scopeKind: 'platform' | 'site'
  siteName: string
}>()
const emit = defineEmits<{
  save: [value: SettingValue, note: string]
}>()

const open = defineModel<boolean>('open', { required: true })

const title = computed(() =>
  props.scopeKind === 'site'
    ? `编辑(站点:${props.siteName})`
    : (props.row?.key ?? '编辑配置')
)

const boolValue = ref(false)
const numberValue = ref<number | null>(null)
const stringValue = ref('')
const enumValue = ref('')
const listValue = ref<string[]>([])
const note = ref('')

const enumOptions = computed(() =>
  (props.row?.enum ?? []).map((value) => ({ value, label: value }))
)

const numberStep = computed(() => (props.row?.kind === 'float' ? 0.01 : 1))

const rangeText = computed(() => {
  const row = props.row
  if (!row) return ''
  if (row.min != null && row.max != null) return `${row.min}–${row.max}`
  if (row.min != null) return `≥ ${row.min}`
  if (row.max != null) return `≤ ${row.max}`
  return ''
})

const resetForm = (row: SettingsKeyView) => {
  const initial = row.override?.value ?? row.effective
  note.value = row.override?.note ?? ''
  boolValue.value = initial === true
  numberValue.value = typeof initial === 'number' ? initial : null
  stringValue.value = typeof initial === 'string' ? initial : ''
  enumValue.value = typeof initial === 'string' ? initial : ''
  listValue.value = Array.isArray(initial) ? [...initial] : []
}

watch([open, () => props.row], ([isOpen, row]) => {
  if (!isOpen || !row) return
  resetForm(row)
})

const reject = (message: string) => {
  useKunMessage(message, 'warn')
}

const emitSave = () => {
  const row = props.row
  if (!row) return
  const nextNote = note.value

  if (row.kind === 'bool') {
    emit('save', boolValue.value, nextNote)
    return
  }

  if (row.kind === 'int') {
    const n = numberValue.value
    if (n === null || !Number.isFinite(n)) {
      reject('请填写整数')
      return
    }
    if (!Number.isInteger(n)) {
      reject('必须是整数')
      return
    }
    if (row.min != null && n < row.min) {
      reject(`值不能小于 ${row.min}`)
      return
    }
    if (row.max != null && n > row.max) {
      reject(`值不能大于 ${row.max}`)
      return
    }
    emit('save', n, nextNote)
    return
  }

  if (row.kind === 'float') {
    const n = numberValue.value
    if (n === null || !Number.isFinite(n)) {
      reject('请填写数字')
      return
    }
    if (row.min != null && n < row.min) {
      reject(`值不能小于 ${row.min}`)
      return
    }
    if (row.max != null && n > row.max) {
      reject(`值不能大于 ${row.max}`)
      return
    }
    emit('save', n, nextNote)
    return
  }

  if (row.kind === 'enum') {
    if (enumValue.value === '') {
      reject('请选择一项')
      return
    }
    emit('save', enumValue.value, nextNote)
    return
  }

  if (row.kind === 'string_list') {
    emit('save', [...listValue.value], nextNote)
    return
  }

  emit('save', stringValue.value, nextNote)
}
</script>

<template>
  <KunModal v-model="open" size="lg" :aria-label="title">
    <div v-if="row" class="space-y-4">
      <div>
        <h2
          class="text-foreground text-xl font-bold"
          :class="{ 'font-mono break-all': scopeKind !== 'site' }"
        >
          {{ title }}
        </h2>
        <p
          v-if="scopeKind === 'site'"
          class="text-foreground mt-1 font-mono break-all"
        >
          {{ row.key }}
        </p>
        <p class="text-default-500 mt-1">{{ row.desc_zh }}</p>
      </div>

      <div class="space-y-1 text-sm">
        <p class="text-default-500">
          默认值
          <span class="text-foreground font-mono">
            {{ formatSettingValue(row.kind, row.default) }}
          </span>
        </p>
        <p class="text-default-500">
          当前生效值
          <span class="text-foreground font-mono">
            {{ formatSettingValue(row.kind, row.effective) }}
          </span>
        </p>
      </div>

      <KunSwitch v-if="row.kind === 'bool'" v-model="boolValue" label="开启" />

      <KunSelect
        v-else-if="row.kind === 'enum'"
        v-model="enumValue"
        :options="enumOptions"
      />

      <div v-else-if="row.kind === 'int' || row.kind === 'float'">
        <KunNumberInput
          v-model="numberValue"
          :min="row.min"
          :max="row.max"
          :step="numberStep"
        />
        <p v-if="rangeText" class="text-default-400 mt-1 text-sm">
          允许范围 {{ rangeText }}
        </p>
      </div>

      <KunInput v-else-if="row.kind === 'string'" v-model="stringValue" />

      <div v-else-if="row.kind === 'string_list'">
        <KunTagInput v-model="listValue" placeholder="输入后回车添加" />
        <p
          v-if="row.pattern"
          class="text-default-400 mt-1 font-mono text-sm break-all"
        >
          {{ row.pattern }}
        </p>
      </div>

      <KunTextarea
        v-model="note"
        label="备注(可选,最多 512 字)"
        :rows="3"
        :maxlength="512"
      />

      <p class="text-default-500 text-sm">{{ PROPAGATION_NOTE }}</p>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :loading="submitting" @click="emitSave">
          保存
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
