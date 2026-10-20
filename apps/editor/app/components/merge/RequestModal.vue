<script setup lang="ts">
import type { KunSelectOption } from '@kungal/ui-vue'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'

type Direction = 'into-other' | 'into-this'

const props = defineProps<{
  family: EditFamily
  entityId: string
  entityName: string
}>()

const open = defineModel<boolean>({ required: true })

const api = useEditorApi()
const refs = useRefNames()

const meta = computed(() => ENTITY_META[props.family])
const other = ref<string | null>(null)
const direction = ref<Direction>('into-other')
const reason = ref('')
const submitting = ref(false)

const directionOptions = computed<KunSelectOption<Direction>[]>(() => [
  {
    value: 'into-other',
    label: `本${meta.value.label}是重复的，并入选中的${meta.value.label}`
  },
  {
    value: 'into-this',
    label: `选中的${meta.value.label}是重复的，并入本${meta.value.label}`
  }
])

const otherName = computed(() =>
  other.value ? refs.nameOf(props.family, other.value) || `#${other.value}` : ''
)
const sourceLabel = computed(() =>
  direction.value === 'into-other' ? props.entityName : otherName.value
)
const targetLabel = computed(() =>
  direction.value === 'into-other' ? otherName.value : props.entityName
)
const canSubmit = computed(
  () =>
    !!other.value &&
    other.value !== props.entityId &&
    reason.value.trim().length >= 4
)

watch(open, (value) => {
  if (value) {
    other.value = null
    direction.value = 'into-other'
    reason.value = ''
  }
})

const submit = async () => {
  if (!canSubmit.value || submitting.value) {
    return
  }
  submitting.value = true
  const [source, target] =
    direction.value === 'into-other'
      ? [props.entityId, other.value!]
      : [other.value!, props.entityId]
  try {
    await api('/v2/me/merge-requests', {
      method: 'POST',
      headers: { 'Idempotency-Key': newIdempotencyKey() },
      body: {
        object: props.family,
        source_id: Number(source),
        target_id: Number(target),
        reason: reason.value.trim()
      }
    })
    useKunMessage('合并请求已提交，审核者确认后会在冷却期结束时执行', 'success')
    open.value = false
    await navigateTo('/me/merge-requests')
  } catch (error) {
    useKunMessage(problemMessage(error, '提交合并请求失败'), 'error')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="`合并重复的${meta.label}`"
    description="合并会把重复条目的资料、关联与外链并入保留的那一个，被并入的条目改为重定向。审核通过后还有一段冷却期，期间可以撤回。"
    size="lg"
  >
    <div class="space-y-4">
      <KunSelect
        v-model="direction"
        :options="directionOptions"
        aria-label="合并方向"
      />
      <EditRefPicker
        v-model="other"
        :family="props.family"
        :exclude="[props.entityId]"
        :aria-label="`另一个${meta.label}`"
        :placeholder="`搜索另一个${meta.label}`"
      />
      <p
        v-if="other"
        class="bg-default-50 text-default-600 rounded-lg p-3 text-sm"
      >
        「{{ sourceLabel }}」将被并入「{{ targetLabel }}」，保留后者。
      </p>
      <KunTextarea
        v-model="reason"
        placeholder="理由（必填）：为什么认为它们是同一个条目，有哪些依据"
        aria-label="合并理由"
        max-height="8rem"
      />
      <div class="flex justify-end gap-2">
        <KunButton variant="light" @click="open = false">取消</KunButton>
        <KunButton
          color="primary"
          :loading="submitting"
          :disabled="!canSubmit"
          @click="submit"
        >
          提交合并请求
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
