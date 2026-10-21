<script setup lang="ts">
const open = defineModel<boolean>({ required: true })

const workbench = useMergeWorkbench()
const reason = ref('')

const confirm = async () => {
  if (!reason.value.trim()) {
    return
  }
  if (await workbench.decide('reject', reason.value)) {
    open.value = false
    reason.value = ''
    useKunMessage('已驳回', 'success')
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    title="驳回合并请求"
    description="驳回理由会展示给提交者。"
    size="md"
  >
    <div class="space-y-4">
      <KunTextarea
        v-model="reason"
        placeholder="驳回理由（必填）：例如两者是不同版本、VNDB 分开收录……"
        aria-label="驳回理由"
        auto-grow
        max-height="200px"
      />
      <div class="flex justify-end gap-2">
        <KunButton variant="light" @click="open = false">取消</KunButton>
        <KunButton
          color="danger"
          :disabled="!reason.trim()"
          :loading="workbench.busy.value === 'reject'"
          @click="confirm"
        >
          驳回
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
