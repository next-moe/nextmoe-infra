<script setup lang="ts">
const props = defineProps<{
  initialReason: string
  loading: boolean
}>()

const emit = defineEmits<{ confirm: [reason: string] }>()

const open = defineModel<boolean>({ required: true })
const reason = ref('')

watch(open, (value) => {
  if (value) {
    reason.value = props.initialReason
  }
})

const submit = () => {
  const trimmed = reason.value.trim()
  if (trimmed) {
    emit('confirm', trimmed)
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    title="拒绝整条提案"
    description="提案人会看到拒绝理由。只想去掉部分字段时，请在字段上选择「拒绝」后合并。"
    role="alertdialog"
    size="md"
  >
    <div class="space-y-3">
      <KunTextarea
        v-model="reason"
        placeholder="写下拒绝理由"
        aria-label="拒绝理由"
        max-height="224px"
        required
      />
      <div class="flex justify-end gap-2">
        <KunButton variant="light" @click="open = false">取消</KunButton>
        <KunButton
          color="danger"
          :loading="props.loading"
          :disabled="!reason.trim()"
          @click="submit"
        >
          确认拒绝
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
