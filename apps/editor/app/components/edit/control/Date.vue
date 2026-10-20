<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const { value, disabled, field } = useEditorField(() => props.fieldKey)

type DateParts = { y: number; m?: number; d?: number }

const parts = computed<Partial<DateParts>>(() =>
  value.value && typeof value.value === 'object'
    ? (value.value as Partial<DateParts>)
    : {}
)

const setPart = (key: keyof DateParts, next: number | null) => {
  const merged: Partial<DateParts> = {
    ...parts.value,
    [key]: next ?? undefined
  }
  if (merged.m === undefined) {
    delete merged.d
  }
  if (merged.y === undefined) {
    value.value = null
    return
  }
  value.value = Object.fromEntries(
    Object.entries(merged).filter(([, v]) => v !== undefined)
  )
}

const partModel = (key: keyof DateParts) =>
  computed({
    get: () => parts.value[key] ?? null,
    set: (next: number | null) => setPart(key, next)
  })

const year = partModel('y')
const month = partModel('m')
const day = partModel('d')
</script>

<template>
  <div class="flex flex-wrap items-end gap-2">
    <div class="space-y-1">
      <span class="text-default-500 text-xs">年</span>
      <KunNumberInput
        v-model="year"
        aria-label="年"
        :disabled="disabled"
        :min="1950"
        :max="2200"
        class="w-28"
      />
    </div>
    <div class="space-y-1">
      <span class="text-default-500 text-xs">月</span>
      <KunNumberInput
        v-model="month"
        aria-label="月"
        :disabled="disabled || year === null"
        :min="1"
        :max="12"
        class="w-20"
      />
    </div>
    <div class="space-y-1">
      <span class="text-default-500 text-xs">日</span>
      <KunNumberInput
        v-model="day"
        aria-label="日"
        :disabled="disabled || month === null"
        :min="1"
        :max="31"
        class="w-20"
      />
    </div>
    <KunButton
      v-if="!disabled && field?.nullable !== false && value"
      size="sm"
      variant="light"
      color="default"
      @click="value = null"
    >
      清空
    </KunButton>
  </div>
</template>
