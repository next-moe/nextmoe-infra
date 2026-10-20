<script setup lang="ts">
const props = defineProps<{ again: boolean }>()

const { submitting, submit } = useEditorApplication()

const reason = ref('')
const serverError = ref('')

const length = computed(() => [...reason.value.trim()].length)
const lengthError = computed(() => {
  if (length.value === 0) {
    return ''
  }
  if (length.value < APPLICATION_REASON_MIN) {
    return `至少 ${APPLICATION_REASON_MIN} 个字`
  }
  if (length.value > APPLICATION_REASON_MAX) {
    return `最多 ${APPLICATION_REASON_MAX} 个字`
  }
  return ''
})
const canSubmit = computed(
  () =>
    !submitting.value &&
    length.value >= APPLICATION_REASON_MIN &&
    length.value <= APPLICATION_REASON_MAX
)

const onSubmit = async () => {
  serverError.value = ''
  const problem = await submit(reason.value.trim())
  if (problem) {
    serverError.value = problem.message
    return
  }
  reason.value = ''
  useKunMessage('申请已提交，等待管理员审核', 'success')
}
</script>

<template>
  <KunCard bordered padding="md">
    <form class="space-y-3" @submit.prevent="onSubmit">
      <h2 class="font-semibold">
        {{ props.again ? '重新提交申请' : '填写申请' }}
      </h2>
      <KunTextarea
        v-model="reason"
        label="申请理由"
        placeholder="介绍一下你常编辑哪些条目、熟悉哪些资料来源、过去的贡献（可附上提案编号或其他站点的链接）"
        max-height="16rem"
        :description="`${length} / ${APPLICATION_REASON_MAX}`"
        :error="lengthError || serverError"
      />
      <div class="flex justify-end">
        <KunButton
          type="submit"
          color="primary"
          :disabled="!canSubmit"
          :loading="submitting"
        >
          提交申请
        </KunButton>
      </div>
    </form>
  </KunCard>
</template>
