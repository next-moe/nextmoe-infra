<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogSchemaField } from '#shared/types/catalog'
import type { DiffContext } from '~/utils/review/context'
import type { FieldDecision, FieldVerdict } from '~/utils/review/decision'
import { cloneValue } from '#shared/utils/editModel'

const props = defineProps<{
  field: CatalogSchemaField
  current: unknown
  proposed: unknown
  decision: FieldDecision
  context: DiffContext
  conflictSeq?: number
  canReview: boolean
  focused: boolean
  disabled: boolean
}>()

const emit = defineEmits<{
  'update:decision': [decision: FieldDecision]
  focus: []
}>()

const VERDICTS: {
  value: FieldVerdict
  label: string
  key: string
  color: 'success' | 'warning' | 'danger'
}[] = [
  { value: 'accept', label: '采纳', key: 'a', color: 'success' },
  { value: 'amend', label: '修正', key: 'e', color: 'warning' },
  { value: 'reject', label: '拒绝', key: 'r', color: 'danger' }
]

const setVerdict = (verdict: FieldVerdict) => {
  if (verdict === 'amend') {
    emit('update:decision', {
      verdict,
      value:
        props.decision.verdict === 'amend'
          ? props.decision.value
          : cloneValue(props.proposed)
    })
  } else {
    emit('update:decision', { verdict })
  }
}

const amended = computed({
  get: () => props.decision.value,
  set: (value) => emit('update:decision', { verdict: 'amend', value })
})

const shown = computed(() =>
  props.decision.verdict === 'amend' ? props.decision.value : props.proposed
)

const TONES: Partial<Record<FieldVerdict, string>> = {
  accept: 'border-default-200',
  amend: 'border-warning-300',
  reject: 'border-danger-300 opacity-70'
}

const tone = computed(
  () => TONES[props.decision.verdict] ?? 'border-default-200'
)
</script>

<template>
  <section
    :class="
      cn(
        'bg-content1 rounded-xl border p-4 transition-shadow',
        tone,
        props.focused && 'ring-primary-300 ring-2'
      )
    "
    tabindex="-1"
    @focusin="emit('focus')"
  >
    <header class="mb-3 flex flex-wrap items-center gap-2">
      <h3 class="text-foreground font-medium">
        {{ fieldLabel(props.field.key) }}
      </h3>
      <code class="text-default-400 text-xs">{{ props.field.key }}</code>
      <KunChip
        v-if="props.conflictSeq"
        size="xs"
        color="warning"
        variant="flat"
      >
        提案之后第 {{ props.conflictSeq }} 版也改过这个字段
      </KunChip>
      <KunChip v-if="!props.canReview" size="xs" variant="flat">
        你无权审核此字段
      </KunChip>
      <div class="ml-auto flex gap-1">
        <KunButton
          v-for="verdict in VERDICTS"
          :key="verdict.value"
          size="sm"
          :variant="props.decision.verdict === verdict.value ? 'flat' : 'light'"
          :color="
            props.decision.verdict === verdict.value ? verdict.color : 'default'
          "
          :disabled="props.disabled || !props.canReview"
          @click="setVerdict(verdict.value)"
        >
          {{ verdict.label }}
          <kbd v-if="props.focused" class="text-default-400 ml-1 text-[10px]">{{
            verdict.key
          }}</kbd>
        </KunButton>
      </div>
    </header>

    <p
      v-if="props.decision.verdict === 'reject'"
      class="text-danger-600 mb-2 text-xs"
    >
      合并时会丢弃这个字段，条目保持当前值。
    </p>

    <DiffField
      :field="props.field"
      :before="props.current"
      :after="shown"
      :context="props.context"
      before-label="当前"
      :after-label="props.decision.verdict === 'amend' ? '修正后' : '提议'"
    />

    <div
      v-if="props.decision.verdict === 'amend'"
      class="border-default-200 mt-3 border-t pt-3"
    >
      <p class="text-default-500 mb-2 text-xs">
        修正提议的值后再合并，提案会记下双方署名。
      </p>
      <ReviewDetailAmendEditor
        v-model="amended"
        :field="props.field"
        :context="props.context"
      />
    </div>
  </section>
</template>
