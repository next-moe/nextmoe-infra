<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogSchemaField } from '#shared/types/catalog'
import type { ThreeWayMerge } from '#shared/utils/threeWay'
import type { DiffContext } from '~/utils/review/context'
import type { FieldDecision, FieldVerdict } from '~/utils/review/decision'
import { cloneValue } from '#shared/utils/editModel'

const props = defineProps<{
  field: CatalogSchemaField
  base: unknown
  baseKnown: boolean
  current: unknown
  proposed: unknown
  merge: ThreeWayMerge | null
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

const autoAvailable = computed(() => Boolean(props.merge?.clean))

const CHOICES = computed(() => [
  {
    value: 'auto' as const,
    label: '自动合并',
    key: 'a',
    icon: 'lucide:git-merge',
    disabled: !autoAvailable.value,
    hint: autoAvailable.value
      ? '把提案的改动套用到现在的值上'
      : props.baseKnown
        ? '两边改到了同一处，不能自动合并'
        : '没有提案时的值，无法自动合并'
  },
  {
    value: 'override' as const,
    label: '采用提案的值',
    key: 'o',
    icon: 'lucide:arrow-right-left',
    disabled: false,
    hint: '用提案的值覆盖之后的修改'
  },
  {
    value: 'keep' as const,
    label: '保留现在的值',
    key: 'c',
    icon: 'lucide:shield-check',
    disabled: false,
    hint: '丢弃提案对这个字段的修改'
  },
  {
    value: 'amend' as const,
    label: '手动合并',
    key: 'e',
    icon: 'lucide:pencil',
    disabled: false,
    hint: '在合并结果的基础上自己调整'
  }
])

const choose = (verdict: FieldVerdict) => {
  if (verdict === 'auto') {
    if (props.merge?.clean) {
      emit('update:decision', { verdict, value: cloneValue(props.merge.value) })
    }
  } else if (verdict === 'amend') {
    emit('update:decision', {
      verdict,
      value:
        props.decision.verdict === 'amend'
          ? props.decision.value
          : cloneValue(props.merge?.value ?? props.current ?? props.proposed)
    })
  } else {
    emit('update:decision', { verdict })
  }
}

const amended = computed({
  get: () => props.decision.value,
  set: (value) => emit('update:decision', { verdict: 'amend', value })
})

const result = computed(() => {
  switch (props.decision.verdict) {
    case 'auto':
    case 'amend':
      return props.decision.value
    case 'override':
      return props.proposed
    default:
      return undefined
  }
})

const resolved = computed(
  () => !['pending', 'accept'].includes(props.decision.verdict)
)
const chosenLabel = computed(
  () =>
    CHOICES.value.find((choice) => choice.value === props.decision.verdict)
      ?.label ?? ''
)

const overlapRows = computed(() => {
  const overlaps = props.merge?.overlaps ?? []
  if (props.field.field_type !== 'list' || !overlaps.length) {
    return null
  }
  return {
    current: overlaps
      .filter((o) => o.current !== undefined)
      .map((o) => o.current),
    proposed: overlaps
      .filter((o) => o.proposed !== undefined)
      .map((o) => o.proposed)
  }
})
</script>

<template>
  <section
    :class="
      cn(
        'bg-content1 rounded-xl border p-4 transition-shadow',
        resolved ? 'border-success-300' : 'border-warning-300',
        props.focused && 'ring-primary-300 ring-2'
      )
    "
    tabindex="-1"
    @focusin="emit('focus')"
  >
    <header class="mb-2 flex flex-wrap items-center gap-2">
      <KunIcon
        :name="resolved ? 'lucide:circle-check' : 'lucide:triangle-alert'"
        :class="
          cn('size-4', resolved ? 'text-success-500' : 'text-warning-500')
        "
      />
      <h3 class="text-foreground font-medium">
        {{ fieldLabel(props.field.key) }}
      </h3>
      <code class="text-default-400 text-xs">{{ props.field.key }}</code>
      <KunChip size="xs" color="warning" variant="flat">
        冲突{{
          props.conflictSeq ? ` · 提案之后第 ${props.conflictSeq} 版也改过` : ''
        }}
      </KunChip>
      <KunChip v-if="resolved" size="xs" color="success" variant="flat">
        已处理：{{ chosenLabel }}
      </KunChip>
      <KunChip v-if="!props.canReview" size="xs" variant="flat">
        你无权审核此字段
      </KunChip>
    </header>
    <p class="text-default-500 mb-3 text-xs leading-5">
      这条提案提交之后，这个字段又被改过。直接合并会覆盖那次修改，请先决定怎么处理。
    </p>

    <div v-if="props.baseKnown" class="grid gap-3 xl:grid-cols-2">
      <div class="border-default-200 rounded-lg border p-3">
        <p class="text-default-600 mb-2 flex items-center gap-1.5 text-xs">
          <KunIcon name="lucide:history" class="size-3.5" />
          提案之后，条目被改成了
        </p>
        <DiffField
          :field="props.field"
          :before="props.base"
          :after="props.current"
          :context="props.context"
          before-label="提案时"
          after-label="现在"
        />
      </div>
      <div class="border-default-200 rounded-lg border p-3">
        <p class="text-default-600 mb-2 flex items-center gap-1.5 text-xs">
          <KunIcon name="lucide:git-pull-request" class="size-3.5" />
          这条提案想改成
        </p>
        <DiffField
          :field="props.field"
          :before="props.base"
          :after="props.proposed"
          :context="props.context"
          before-label="提案时"
          after-label="提议"
        />
      </div>
    </div>
    <template v-else>
      <KunInfo
        color="warning"
        variant="flat"
        icon="lucide:circle-help"
        title="没有记录提案时的值"
        description="这个条目在开始记录编辑前的状态之前就被改过，只能对照现在的值和提议的值来决定。"
        class-name="mb-3"
      />
      <DiffField
        :field="props.field"
        :before="props.current"
        :after="props.proposed"
        :context="props.context"
        before-label="现在"
        after-label="提议"
      />
    </template>

    <div
      v-if="overlapRows"
      class="border-warning-200 bg-warning-50 mt-3 rounded-lg border p-3"
    >
      <p class="text-warning-800 mb-2 text-xs">
        这 {{ props.merge?.overlaps.length }} 处两边都改过，需要你来决定：
      </p>
      <DiffField
        :field="props.field"
        :before="overlapRows.current"
        :after="overlapRows.proposed"
        :context="props.context"
        before-label="现在"
        after-label="提议"
      />
    </div>

    <div class="mt-3 flex flex-wrap gap-2">
      <KunTooltip
        v-for="choice in CHOICES"
        :key="choice.value"
        :text="choice.hint"
        position="top"
      >
        <KunButton
          size="sm"
          :variant="
            props.decision.verdict === choice.value ? 'flat' : 'bordered'
          "
          :color="
            props.decision.verdict === choice.value ? 'primary' : 'default'
          "
          :disabled="props.disabled || !props.canReview || choice.disabled"
          @click="choose(choice.value)"
        >
          <KunIcon :name="choice.icon" class="size-4" />
          {{ choice.label }}
          <kbd v-if="props.focused" class="text-default-400 ml-1 text-[10px]">{{
            choice.key
          }}</kbd>
        </KunButton>
      </KunTooltip>
    </div>

    <p
      v-if="props.decision.verdict === 'keep'"
      class="text-default-500 mt-3 text-xs"
    >
      合并时丢弃提案对这个字段的修改，保持现在的值。
    </p>
    <div
      v-else-if="result !== undefined"
      class="border-default-200 mt-3 border-t pt-3"
    >
      <p class="text-default-600 mb-2 text-xs">合并后会变成</p>
      <DiffField
        :field="props.field"
        :before="props.current"
        :after="result"
        :context="props.context"
        before-label="现在"
        after-label="合并后"
      />
    </div>

    <div
      v-if="props.decision.verdict === 'amend'"
      class="border-default-200 mt-3 border-t pt-3"
    >
      <p class="text-default-500 mb-2 text-xs">
        在下面调整合并结果，提案会记下双方署名。
      </p>
      <ReviewDetailAmendEditor
        v-model="amended"
        :field="props.field"
        :context="props.context"
      />
    </div>
  </section>
</template>
